package main

import (
	"context"
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	defaultListenAddress  = "127.0.0.1:8787"
	maxLogBytes           = 128 * 1024
	clashAPIURL           = "http://127.0.0.1:9090"
	selectorTag           = "proxy"
	googleLatencyTestURL  = "https://www.gstatic.com/generate_204"
	geminiLatencyTestURL  = "https://gemini.google.com/"
	chatGPTLatencyTestURL = "https://chatgpt.com/"
	// sing-box defines urltest tolerance as uint16. Its largest valid value is
	// far above ordinary latency, so it prevents latency-only switching.
	failoverTolerance = 65535
	// In failover-only mode, probe the pinned automatic node regularly. Manual
	// "test all" requests never use this path and therefore never switch nodes.
	failoverCheckInterval = 30 * time.Second
	delayTestConcurrency  = 32
)

//go:embed defaults/config.json
var defaultConfig []byte

// packagedBuild is set by package.ps1 so a portable executable keeps data
// alongside itself. Development runs continue to use the working directory.
var packagedBuild = "false"

var listenAddress = defaultListenAddress

type app struct {
	mu            sync.Mutex
	dataDir       string
	config        string
	binary        string
	subscriptions string
	whitelist     string
	autoSwitch    string
	nodeCleanup   string
	cmd           *exec.Cmd
	cancel        context.CancelFunc
	started       time.Time
	lastExit      string
	logs          string
	delays        map[string]serviceDelays
	failoverMu    sync.Mutex
	cleanupMu     sync.Mutex
	applyMu       sync.Mutex
}

type statusResponse struct {
	Running       bool   `json:"running"`
	StartedAt     string `json:"startedAt,omitempty"`
	LastExit      string `json:"lastExit,omitempty"`
	Binary        string `json:"binary"`
	ConfigPath    string `json:"configPath"`
	ProxyEndpoint string `json:"proxyEndpoint"`
}

type systemProxyResponse struct {
	Supported bool   `json:"supported"`
	Enabled   bool   `json:"enabled"`
	Server    string `json:"server,omitempty"`
}

type autoSwitchSettings struct {
	FailoverOnly bool `json:"failoverOnly"`
	// ActiveGroup and the per-group selection maps make a user choice survive
	// stopping the local core or restarting the web agent.
	ActiveGroup string `json:"activeGroup,omitempty"`
	// Pinned holds the node currently used by each automatic group in
	// failover-only mode. Traffic goes through the selector directly, not the
	// urltest outbound, so a latency measurement cannot replace it.
	Pinned map[string]string `json:"pinned,omitempty"`
	// Manual records groups explicitly selected by the user. A pinned group not
	// marked manual is still in automatic mode and may be changed on failure.
	Manual map[string]bool `json:"manual,omitempty"`
}

type failedNodeCleanupSettings struct {
	RemoveFailed bool `json:"removeFailed"`
}

type configRequest struct {
	Content string `json:"content"`
}

type subscriptionRequest struct {
	URL string `json:"url"`
}

type subscriptionGroup struct {
	ID        string      `json:"id"`
	URL       string      `json:"url"`
	Name      string      `json:"name"`
	UpdatedAt time.Time   `json:"updatedAt"`
	Nodes     []vlessNode `json:"nodes"`
}

type subscriptionSummary struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	URL       string `json:"url"`
	NodeCount int    `json:"nodeCount"`
	UpdatedAt string `json:"updatedAt"`
}

type subscriptionsResponse struct {
	Groups []subscriptionSummary `json:"groups"`
}

type legacySubscription struct {
	URL       string     `json:"url"`
	Name      string     `json:"name"`
	UpdatedAt time.Time  `json:"updatedAt"`
	Nodes     []nodeMeta `json:"nodes"`
}

type vlessNode struct {
	Tag      string
	Name     string
	Outbound map[string]any
}

type nodeMeta struct {
	Tag  string `json:"tag"`
	Name string `json:"name"`
}

type nodeStatusResponse struct {
	Running     bool          `json:"running"`
	ActiveGroup string        `json:"activeGroup"`
	Groups      []groupStatus `json:"groups"`
}

type groupStatus struct {
	ID     string       `json:"id"`
	Name   string       `json:"name"`
	Mode   string       `json:"mode"`
	Active string       `json:"active"`
	Nodes  []nodeStatus `json:"nodes"`
}

type nodeStatus struct {
	Tag     string `json:"tag"`
	Name    string `json:"name"`
	Country string `json:"country"`
	// DelayMS is the combined score retained for older frontends. It is the
	// slowest successful service check, so a slow AI service is not hidden by
	// a fast Google probe.
	DelayMS        int    `json:"delayMs"`
	GoogleDelayMS  int    `json:"googleDelayMs"`
	GeminiDelayMS  int    `json:"geminiDelayMs"`
	ChatGPTDelayMS int    `json:"chatgptDelayMs"`
	Error          string `json:"error,omitempty"`
}

// serviceDelays stores the real destination checks for a node. A positive
// value is milliseconds, zero is not tested yet, and -1 is a failed check.
type serviceDelays struct {
	Google  int
	Gemini  int
	ChatGPT int
}

func (d serviceDelays) merge(next serviceDelays, service delayTestService) serviceDelays {
	if service == delayTestAll {
		return next
	}
	switch service {
	case delayTestGoogle:
		d.Google = next.Google
	case delayTestGemini:
		d.Gemini = next.Gemini
	case delayTestChatGPT:
		d.ChatGPT = next.ChatGPT
	}
	return d
}

type delayTestService string

type latencyServiceProbe struct {
	name string
	url  string
}

const (
	delayTestAll     delayTestService = "all"
	delayTestGoogle  delayTestService = "google"
	delayTestGemini  delayTestService = "gemini"
	delayTestChatGPT delayTestService = "chatgpt"
)

func (d serviceDelays) combined() int {
	if d.Google == 0 && d.Gemini == 0 && d.ChatGPT == 0 {
		return 0
	}
	if d.Google <= 0 || d.Gemini <= 0 || d.ChatGPT <= 0 {
		return -1
	}
	return max(d.Google, d.Gemini, d.ChatGPT)
}

func (d serviceDelays) errorMessage() string {
	failed := make([]string, 0, 3)
	if d.Google < 0 {
		failed = append(failed, "Google")
	}
	if d.Gemini < 0 {
		failed = append(failed, "Gemini")
	}
	if d.ChatGPT < 0 {
		failed = append(failed, "ChatGPT")
	}
	if len(failed) == 0 {
		return ""
	}
	return strings.Join(failed, "、") + " 无法连接"
}

func parseDelayTestService(r *http.Request) (delayTestService, error) {
	service := delayTestService(strings.ToLower(strings.TrimSpace(r.URL.Query().Get("service"))))
	if service == "" {
		return delayTestAll, nil
	}
	switch service {
	case delayTestAll, delayTestGoogle, delayTestGemini, delayTestChatGPT:
		return service, nil
	default:
		return "", errors.New("test service must be all, google, gemini, or chatgpt")
	}
}

type routeRule struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Kind     string `json:"kind"`
	Value    string `json:"value"`
	Outbound string `json:"outbound"`
	Source   string `json:"source"`
	Editable bool   `json:"editable"`
}

type routeRulesResponse struct {
	Rules []routeRule `json:"rules"`
}

type nodeAvailability struct {
	Country string
}

type countryAvailabilityRule struct {
	Country string
	Markers []string
}

// This only recognizes the region written in a subscription's display name.
// Service availability is determined by actual latency tests instead.
var countryAvailabilityRules = []countryAvailabilityRule{
	{Country: "香港", Markers: []string{"🇭🇰", "香港", "hong kong"}},
	{Country: "美国", Markers: []string{"🇺🇸", "美国", "united states", "usa"}},
	{Country: "澳大利亚", Markers: []string{"🇦🇺", "澳大利亚", "australia"}},
	{Country: "韩国", Markers: []string{"🇰🇷", "韩国", "south korea", "korea"}},
	{Country: "德国", Markers: []string{"🇩🇪", "德国", "germany"}},
	{Country: "阿联酋", Markers: []string{"🇦🇪", "阿联酋", "迪拜", "united arab emirates", "uae", "dubai"}},
	{Country: "英国", Markers: []string{"🇬🇧", "英国", "united kingdom", "uk", "great britain"}},
	{Country: "印度", Markers: []string{"🇮🇳", "印度", "india"}},
	{Country: "新加坡", Markers: []string{"🇸🇬", "新加坡", "singapore"}},
	{Country: "日本", Markers: []string{"🇯🇵", "日本", "japan"}},
}

type selectionRequest struct {
	GroupID string `json:"groupId"`
	Mode    string `json:"mode"`
	Tag     string `json:"tag"`
}

type delayTestRequest struct {
	Service string `json:"service"`
}

type clashProxiesResponse struct {
	Proxies map[string]clashProxy `json:"proxies"`
}

type clashProxy struct {
	Now string `json:"now"`
}

func main() {
	configureListenAddress()
	dataDir := os.Getenv("SINGBOX_WEB_DATA_DIR")
	if dataDir == "" {
		dataDir = "data"
		if packagedBuild == "true" {
			if executable, err := os.Executable(); err == nil {
				dataDir = filepath.Join(filepath.Dir(executable), "data")
			}
		}
	}
	absoluteDataDir, err := filepath.Abs(dataDir)
	if err != nil {
		log.Fatalf("resolve data directory: %v", err)
	}

	application := &app{
		dataDir:       absoluteDataDir,
		config:        filepath.Join(absoluteDataDir, "config.json"),
		binary:        findBinary(absoluteDataDir),
		subscriptions: filepath.Join(absoluteDataDir, "subscriptions.json"),
		whitelist:     filepath.Join(absoluteDataDir, "whitelist.json"),
		autoSwitch:    filepath.Join(absoluteDataDir, "auto-switch.json"),
		nodeCleanup:   filepath.Join(absoluteDataDir, "failed-node-cleanup.json"),
		delays:        make(map[string]serviceDelays),
	}
	if err := application.ensureConfig(); err != nil {
		log.Fatalf("prepare configuration: %v", err)
	}
	if err := application.ensureSubscriptions(); err != nil {
		log.Fatalf("prepare subscriptions: %v", err)
	}
	if err := application.ensureWhitelist(); err != nil {
		log.Fatalf("prepare whitelist: %v", err)
	}
	if err := application.ensureAutoSwitchSettings(); err != nil {
		log.Fatalf("prepare auto-switch settings: %v", err)
	}
	if err := application.ensureFailedNodeCleanupSettings(); err != nil {
		log.Fatalf("prepare failed-node cleanup settings: %v", err)
	}
	if err := application.reconcileFailoverOnlyMode(); err != nil {
		log.Printf("prepare failover-only mode: %v", err)
	}
	go application.runFailoverMonitor()

	server := &http.Server{
		Addr:              listenAddress,
		Handler:           withCORS(application.handler()),
		ReadHeaderTimeout: 5 * time.Second,
	}

	log.Printf("singbox-web API is listening on http://%s", listenAddress)
	log.Printf("sing-box binary: %s", application.binary)
	if shouldOpenBrowser() {
		go openBrowser()
	}
	if err := server.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}

func configureListenAddress() {
	port := strings.TrimSpace(os.Getenv("SINGBOX_WEB_LISTEN_PORT"))
	if port == "" {
		return
	}
	value, err := strconv.Atoi(port)
	if err != nil || value < 1 || value > 65535 {
		log.Fatalf("invalid SINGBOX_WEB_LISTEN_PORT: %q", port)
	}
	listenAddress = "127.0.0.1:" + strconv.Itoa(value)
}

func (a *app) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/status", a.handleStatus)
	mux.HandleFunc("GET /api/config", a.handleGetConfig)
	mux.HandleFunc("PUT /api/config", a.handleSaveConfig)
	mux.HandleFunc("GET /api/logs", a.handleLogs)
	mux.HandleFunc("POST /api/restart", a.handleRestart)
	mux.HandleFunc("POST /api/exit", a.handleExit)
	mux.HandleFunc("GET /api/system-proxy", a.handleSystemProxyStatus)
	mux.HandleFunc("POST /api/system-proxy", a.handleSystemProxyUpdate)
	mux.HandleFunc("GET /api/auto-switch", a.handleAutoSwitchStatus)
	mux.HandleFunc("POST /api/auto-switch", a.handleAutoSwitchUpdate)
	mux.HandleFunc("GET /api/failed-node-cleanup", a.handleFailedNodeCleanupStatus)
	mux.HandleFunc("POST /api/failed-node-cleanup", a.handleFailedNodeCleanupUpdate)
	mux.HandleFunc("GET /api/subscriptions", a.handleListSubscriptions)
	mux.HandleFunc("POST /api/subscriptions", a.handleImportSubscription)
	mux.HandleFunc("POST /api/subscriptions/{id}/refresh", a.handleRefreshSubscription)
	mux.HandleFunc("DELETE /api/subscriptions/{id}", a.handleDeleteSubscription)
	mux.HandleFunc("GET /api/nodes", a.handleNodes)
	mux.HandleFunc("POST /api/groups/{groupID}/nodes/test", a.handleTestGroupNodes)
	mux.HandleFunc("POST /api/nodes/{tag}/test", a.handleTestNode)
	mux.HandleFunc("POST /api/selection", a.handleSelection)
	mux.HandleFunc("GET /api/whitelist", a.handleGetWhitelist)
	mux.HandleFunc("POST /api/whitelist", a.handleAddWhitelist)
	mux.HandleFunc("PUT /api/whitelist/{domain}", a.handleUpdateWhitelist)
	mux.HandleFunc("DELETE /api/whitelist/{domain}", a.handleDeleteWhitelist)
	mux.HandleFunc("GET /api/route-rules", a.handleRouteRules)
	mux.Handle("/", frontendHandler())
	return mux
}

func shouldOpenBrowser() bool {
	// A portable build is launched by double-clicking the executable, so it
	// opens its local control page. Electron disables this for its child process.
	if packagedBuild == "true" {
		return os.Getenv("SINGBOX_WEB_OPEN_BROWSER") != "false"
	}
	for _, argument := range os.Args[1:] {
		if argument == "--open" {
			return true
		}
	}
	return false
}

func openBrowser() {
	if runtime.GOOS != "windows" {
		return
	}
	time.Sleep(300 * time.Millisecond)
	command := exec.Command("rundll32.exe", "url.dll,FileProtocolHandler", fmt.Sprintf("http://%s/", listenAddress))
	configureBackgroundCommand(command)
	if err := command.Start(); err != nil {
		log.Printf("open browser: %v", err)
	}
}

func frontendHandler() http.Handler {
	webDir := strings.TrimSpace(os.Getenv("SINGBOX_WEB_DIR"))
	if webDir == "" {
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "frontend directory is missing; set SINGBOX_WEB_DIR", http.StatusServiceUnavailable)
		})
	}
	indexPath := filepath.Join(webDir, "index.html")
	if info, err := os.Stat(indexPath); err != nil || info.IsDir() {
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "frontend index is missing: "+indexPath, http.StatusServiceUnavailable)
		})
	}
	fileServer := http.FileServer(http.Dir(webDir))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			http.NotFound(w, r)
			return
		}
		if r.URL.Path == "/" || (!strings.HasPrefix(r.URL.Path, "/assets/") && filepath.Ext(r.URL.Path) == "") {
			clone := r.Clone(r.Context())
			clone.URL.Path = "/"
			fileServer.ServeHTTP(w, clone)
			return
		}
		fileServer.ServeHTTP(w, r)
	})
}

func findBinary(dataDir string) string {
	if value := os.Getenv("SINGBOX_BINARY"); value != "" {
		return value
	}
	name := "sing-box"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return filepath.Join(dataDir, "bin", name)
}

func (a *app) ensureConfig() error {
	if err := os.MkdirAll(a.dataDir, 0o755); err != nil {
		return err
	}
	if _, err := os.Stat(a.config); errors.Is(err, os.ErrNotExist) {
		return os.WriteFile(a.config, defaultConfig, 0o600)
	}
	return nil
}

// ensureSubscriptions migrates the legacy single subscription.json into the
// grouped subscriptions.json the first time the new binary runs.
func (a *app) ensureSubscriptions() error {
	if _, err := os.Stat(a.subscriptions); err == nil {
		return nil
	}
	legacyPath := filepath.Join(a.dataDir, "subscription.json")
	contents, err := os.ReadFile(legacyPath)
	if err != nil {
		return os.WriteFile(a.subscriptions, []byte("[]"), 0o600)
	}
	var legacy legacySubscription
	if err := json.Unmarshal(contents, &legacy); err != nil || legacy.URL == "" {
		return os.WriteFile(a.subscriptions, []byte("[]"), 0o600)
	}
	group := subscriptionGroup{
		ID:        "g1",
		URL:       legacy.URL,
		Name:      legacy.Name,
		UpdatedAt: legacy.UpdatedAt,
		Nodes:     a.extractNodesFromConfig(legacy.Nodes),
	}
	if err := a.writeSubscriptions([]subscriptionGroup{group}); err != nil {
		return err
	}
	if len(group.Nodes) > 0 {
		if config, err := buildSubscriptionConfig([]subscriptionGroup{group}, nil); err == nil {
			_ = os.WriteFile(a.config, config, 0o600)
		}
	}
	_ = os.Remove(legacyPath)
	a.appendLog(fmt.Sprintf("Migrated subscription %q into group g1 (%d nodes).", group.Name, len(group.Nodes)))
	return nil
}

// extractNodesFromConfig reads vless outbounds from the current config.json and
// pairs them with the legacy node metadata so a group can be rebuilt without a
// fresh download.
func (a *app) extractNodesFromConfig(metas []nodeMeta) []vlessNode {
	contents, err := os.ReadFile(a.config)
	if err != nil {
		return nil
	}
	var cfg struct {
		Outbounds []map[string]any `json:"outbounds"`
	}
	if err := json.Unmarshal(contents, &cfg); err != nil {
		return nil
	}
	byTag := make(map[string]map[string]any)
	for _, outbound := range cfg.Outbounds {
		if typeName, _ := outbound["type"].(string); typeName == "vless" {
			if tag, _ := outbound["tag"].(string); tag != "" {
				byTag[tag] = outbound
			}
		}
	}
	var nodes []vlessNode
	for _, meta := range metas {
		if outbound, ok := byTag[meta.Tag]; ok {
			nodes = append(nodes, vlessNode{Tag: meta.Tag, Name: meta.Name, Outbound: outbound})
		}
	}
	relabelNodes(nodes, "g1")
	return nodes
}

func autoTagFor(groupID string) string  { return "auto-" + groupID }
func groupTagFor(groupID string) string { return "grp-" + groupID }

func groupIDFromTag(tag string) string {
	return strings.TrimPrefix(tag, "grp-")
}

func nextGroupID(groups []subscriptionGroup) string {
	max := 0
	for _, group := range groups {
		var n int
		if _, err := fmt.Sscanf(group.ID, "g%d", &n); err == nil && n > max {
			max = n
		}
	}
	return fmt.Sprintf("g%d", max+1)
}

// relabelNodes assigns stable per-group tags (e.g. g1-01) to parsed nodes.
func relabelNodes(nodes []vlessNode, groupID string) {
	for i := range nodes {
		tag := fmt.Sprintf("%s-%02d", groupID, i+1)
		nodes[i].Tag = tag
		nodes[i].Outbound["tag"] = tag
	}
}

func findGroup(groups []subscriptionGroup, id string) int {
	for i, group := range groups {
		if group.ID == id {
			return i
		}
	}
	return -1
}

func hasNodeVless(nodes []vlessNode, tag string) bool {
	for _, node := range nodes {
		if node.Tag == tag {
			return true
		}
	}
	return false
}

func summaries(groups []subscriptionGroup) []subscriptionSummary {
	out := make([]subscriptionSummary, 0, len(groups))
	for _, group := range groups {
		out = append(out, subscriptionSummary{
			ID:        group.ID,
			Name:      group.Name,
			URL:       group.URL,
			NodeCount: len(group.Nodes),
			UpdatedAt: group.UpdatedAt.Format(time.RFC3339),
		})
	}
	return out
}
func (a *app) status() statusResponse {
	a.mu.Lock()
	defer a.mu.Unlock()

	response := statusResponse{
		Running:       a.cmd != nil && a.cmd.Process != nil,
		LastExit:      a.lastExit,
		Binary:        a.binary,
		ConfigPath:    a.config,
		ProxyEndpoint: "socks5://127.0.0.1:2080",
	}
	if !a.started.IsZero() && response.Running {
		response.StartedAt = a.started.Format(time.RFC3339)
	}
	return response
}

func (a *app) appendLog(message string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.logs += fmt.Sprintf("[%s] %s\n", time.Now().Format("15:04:05"), strings.TrimSpace(message))
	if len(a.logs) > maxLogBytes {
		a.logs = a.logs[len(a.logs)-maxLogBytes:]
	}
}

func (a *app) start() error {
	a.mu.Lock()
	if a.cmd != nil && a.cmd.Process != nil {
		a.mu.Unlock()
		return errors.New("sing-box is already running")
	}
	binary, config := a.binary, a.config
	a.mu.Unlock()
	if err := a.prepareSavedSelectionConfig(); err != nil {
		return err
	}

	if _, err := os.Stat(binary); err != nil {
		return fmt.Errorf("sing-box binary not found at %s; place it there or set SINGBOX_BINARY", binary)
	}
	checkCmd := exec.Command(binary, "check", "-c", config)
	checkCmd.Dir = filepath.Dir(config)
	configureBackgroundCommand(checkCmd)
	if output, err := checkCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("configuration is invalid: %s", strings.TrimSpace(string(output)))
	}

	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, binary, "run", "-c", config)
	cmd.Dir = filepath.Dir(config)
	configureBackgroundCommand(cmd)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		cancel()
		return err
	}
	if err := cmd.Start(); err != nil {
		cancel()
		return err
	}

	a.mu.Lock()
	a.cmd = cmd
	a.cancel = cancel
	a.started = time.Now()
	a.lastExit = ""
	a.mu.Unlock()
	a.appendLog("Started sing-box.")

	readLogs := func(output io.Reader) {
		buffer := make([]byte, 4096)
		for {
			n, readErr := output.Read(buffer)
			if n > 0 {
				a.appendLog(string(buffer[:n]))
			}
			if readErr != nil {
				return
			}
		}
	}
	go readLogs(stdout)
	go readLogs(stderr)

	go func() {
		err := cmd.Wait()
		a.mu.Lock()
		if a.cmd == cmd {
			a.cmd = nil
			a.cancel = nil
			a.lastExit = exitMessage(err)
		}
		a.mu.Unlock()
		a.appendLog("sing-box exited: " + exitMessage(err))
	}()
	go a.restoreSavedSelection()

	return nil
}

func (a *app) stop() error {
	a.mu.Lock()
	cancel := a.cancel
	cmd := a.cmd
	a.mu.Unlock()
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	if cancel != nil {
		cancel()
	}
	return nil
}

func (a *app) stopAndWait() error {
	if err := a.stop(); err != nil {
		return err
	}

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if !a.status().Running {
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	return errors.New("timed out while stopping sing-box")
}

func exitMessage(err error) string {
	if err == nil {
		return "stopped"
	}
	return err.Error()
}

func (a *app) handleStatus(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, a.status())
}

func (a *app) handleGetConfig(w http.ResponseWriter, _ *http.Request) {
	contents, err := os.ReadFile(a.config)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, configRequest{Content: string(contents)})
}

func (a *app) handleSaveConfig(w http.ResponseWriter, r *http.Request) {
	if a.status().Running {
		writeError(w, http.StatusConflict, errors.New("stop sing-box before changing its configuration"))
		return
	}
	defer r.Body.Close()
	var request configRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 2*1024*1024)).Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Errorf("read JSON: %w", err))
		return
	}
	if !json.Valid([]byte(request.Content)) {
		writeError(w, http.StatusBadRequest, errors.New("configuration must be valid JSON"))
		return
	}
	if err := os.WriteFile(a.config, []byte(request.Content), 0o600); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	a.appendLog("Saved configuration.")
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (a *app) handleLogs(w http.ResponseWriter, _ *http.Request) {
	a.mu.Lock()
	logs := a.logs
	a.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]string{"content": logs})
}

// handleRestart reloads the generated configuration without changing the
// Windows system-proxy setting.
func (a *app) handleRestart(w http.ResponseWriter, _ *http.Request) {
	if err := a.stopAndWait(); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if err := a.start(); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusAccepted, a.status())
}

// handleExit is used only when the desktop application is actually quitting.
// A regular stop keeps the user's system-proxy choice untouched.
func (a *app) handleExit(w http.ResponseWriter, _ *http.Request) {
	// Only clear the system proxy when it is still pointing at this local
	// instance. Do not overwrite a proxy configuration another app installed.
	if proxy, err := systemProxyStatus(); err == nil && proxy.Enabled && isLocalProxyServer(proxy.Server) {
		if err := setSystemProxy(false); err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		a.appendLog("System proxy disabled before stopping sing-box.")
	}
	if err := a.stop(); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusAccepted, a.status())
}

func (a *app) handleSystemProxyStatus(w http.ResponseWriter, _ *http.Request) {
	status, err := systemProxyStatus()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, status)
}

func (a *app) handleSystemProxyUpdate(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	var request struct {
		Enabled bool `json:"enabled"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16*1024)).Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if request.Enabled && !a.status().Running {
		writeError(w, http.StatusConflict, errors.New("start sing-box before enabling the system proxy"))
		return
	}
	if err := setSystemProxy(request.Enabled); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	status, err := systemProxyStatus()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	a.appendLog(fmt.Sprintf("System proxy %s.", map[bool]string{true: "enabled", false: "disabled"}[request.Enabled]))
	writeJSON(w, http.StatusOK, status)
}

func (a *app) handleAutoSwitchStatus(w http.ResponseWriter, _ *http.Request) {
	settings, err := a.readAutoSwitchSettings()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, settings)
}

func (a *app) handleAutoSwitchUpdate(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	var settings autoSwitchSettings
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16*1024)).Decode(&settings); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	normalizeAutoSwitchSettings(&settings)
	if settings.FailoverOnly {
		// Preserve the currently selected node before rebuilding the group
		// selector. The rebuilt selector will point to this node directly.
		a.captureActiveSelection(&settings)
	}
	if err := a.writeAutoSwitchSettings(settings); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if err := a.applyAutoSwitchSettings(); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	a.appendLog(fmt.Sprintf("Automatic switching mode set to %s.", map[bool]string{true: "failover-only", false: "latency-optimized"}[settings.FailoverOnly]))
	writeJSON(w, http.StatusOK, settings)
}

func (a *app) handleFailedNodeCleanupStatus(w http.ResponseWriter, _ *http.Request) {
	settings, err := a.readFailedNodeCleanupSettings()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, settings)
}

func (a *app) handleFailedNodeCleanupUpdate(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	var settings failedNodeCleanupSettings
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16*1024)).Decode(&settings); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if err := a.writeFailedNodeCleanupSettings(settings); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	a.appendLog(fmt.Sprintf("Remove failed nodes after delay tests: %t.", settings.RemoveFailed))
	writeJSON(w, http.StatusOK, settings)
}

func isLocalProxyServer(server string) bool {
	server = strings.TrimSpace(strings.ToLower(server))
	return server == "127.0.0.1:2081" || server == "localhost:2081"
}

func (a *app) handleListSubscriptions(w http.ResponseWriter, _ *http.Request) {
	groups, err := a.readSubscriptions()
	if errors.Is(err, os.ErrNotExist) {
		writeJSON(w, http.StatusOK, subscriptionsResponse{})
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, subscriptionsResponse{Groups: summaries(groups)})
}

func (a *app) handleImportSubscription(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	var request subscriptionRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16*1024)).Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Errorf("read JSON: %w", err))
		return
	}
	if _, err := a.importSubscription(request.URL); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	groups, _ := a.readSubscriptions()
	writeJSON(w, http.StatusOK, subscriptionsResponse{Groups: summaries(groups)})
}

func (a *app) handleRefreshSubscription(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, err := a.refreshSubscription(id); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	groups, _ := a.readSubscriptions()
	writeJSON(w, http.StatusOK, subscriptionsResponse{Groups: summaries(groups)})
}

func (a *app) handleDeleteSubscription(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := a.deleteSubscription(id); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	groups, _ := a.readSubscriptions()
	writeJSON(w, http.StatusOK, subscriptionsResponse{Groups: summaries(groups)})
}

func (a *app) handleNodes(w http.ResponseWriter, _ *http.Request) {
	response, err := a.nodesStatus()
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, response)
}

// handleTestGroupNodes measures every node in the requested subscription
// group. It intentionally never submits tests for nodes from other groups.
func (a *app) handleTestGroupNodes(w http.ResponseWriter, r *http.Request) {
	groupID := r.PathValue("groupID")
	groups, err := a.readSubscriptions()
	if err != nil || len(groups) == 0 {
		writeError(w, http.StatusBadRequest, errors.New("import a subscription before testing nodes"))
		return
	}
	if !a.status().Running {
		writeError(w, http.StatusConflict, errors.New("start sing-box before testing nodes"))
		return
	}
	service, err := parseDelayTestService(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}

	idx := findGroup(groups, groupID)
	if idx < 0 {
		writeError(w, http.StatusNotFound, errors.New("subscription group not found"))
		return
	}
	cleanup, err := a.readFailedNodeCleanupSettings()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	group := groups[idx]
	// Removing a node after a partial probe would be misleading. Cleanup is
	// intentionally reserved for an all-services test.
	removeFailed := cleanup.RemoveFailed && service == delayTestAll
	go a.testGroupNodes(group.ID, group.Nodes, service, removeFailed)
	writeJSON(w, http.StatusAccepted, map[string]int{"testing": len(group.Nodes)})
}

func (a *app) handleTestNode(w http.ResponseWriter, r *http.Request) {
	tag := r.PathValue("tag")
	groups, err := a.readSubscriptions()
	if err != nil {
		writeError(w, http.StatusBadRequest, errors.New("import a subscription before testing nodes"))
		return
	}
	found := false
	groupID := ""
	for _, group := range groups {
		if hasNodeVless(group.Nodes, tag) {
			found = true
			groupID = group.ID
			break
		}
	}
	if !found {
		writeError(w, http.StatusNotFound, errors.New("node not found"))
		return
	}
	if !a.status().Running {
		writeError(w, http.StatusConflict, errors.New("start sing-box before testing nodes"))
		return
	}
	service, err := parseDelayTestService(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	cleanup, err := a.readFailedNodeCleanupSettings()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	a.testNodeAsync(groupID, tag, service, cleanup.RemoveFailed && service == delayTestAll)
	writeJSON(w, http.StatusAccepted, map[string]string{"testing": tag})
}

func (a *app) handleSelection(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	if !a.status().Running {
		writeError(w, http.StatusConflict, errors.New("start sing-box before changing selection"))
		return
	}
	var request selectionRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16*1024)).Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	groups, err := a.readSubscriptions()
	if err != nil {
		writeError(w, http.StatusBadRequest, errors.New("import a subscription before changing selection"))
		return
	}
	idx := findGroup(groups, request.GroupID)
	if idx < 0 {
		writeError(w, http.StatusBadRequest, errors.New("select an existing subscription group"))
		return
	}
	group := groups[idx]
	target := autoTagFor(group.ID)
	settings, err := a.readAutoSwitchSettings()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if request.Mode == "manual" {
		if !hasNodeVless(group.Nodes, request.Tag) {
			writeError(w, http.StatusBadRequest, errors.New("select a node from this subscription group"))
			return
		}
		target = request.Tag
	} else if request.Mode != "auto" {
		writeError(w, http.StatusBadRequest, errors.New("mode must be auto or manual"))
		return
	} else if settings.FailoverOnly {
		// Keep automatic mode on one concrete node. Using the urltest outbound
		// here would let any latency request choose another member.
		target, err = a.failoverTarget(group, settings)
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
	}
	// Switch the active group, then pick auto or a specific node within it.
	if err := a.setProxy(selectorTag, groupTagFor(group.ID)); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if err := a.setProxy(groupTagFor(group.ID), target); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	normalizeAutoSwitchSettings(&settings)
	settings.ActiveGroup = group.ID
	if request.Mode == "manual" {
		settings.Pinned[group.ID] = target
		settings.Manual[group.ID] = true
	} else {
		settings.Manual[group.ID] = false
		if settings.FailoverOnly {
			settings.Pinned[group.ID] = target
		}
	}
	if err := a.writeAutoSwitchSettings(settings); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	a.appendLog(fmt.Sprintf("Selected group %s (%s).", group.ID, target))
	response, _ := a.nodesStatus()
	writeJSON(w, http.StatusOK, response)
}

func (a *app) nodesStatus() (nodeStatusResponse, error) {
	groups, err := a.readSubscriptions()
	if err != nil {
		return nodeStatusResponse{}, errors.New("import a subscription to view nodes")
	}
	response := nodeStatusResponse{Running: a.status().Running}

	activeGroup := ""
	mode := make(map[string]string)
	active := make(map[string]string)
	settings, _ := a.readAutoSwitchSettings()
	if response.Running {
		if selected, err := a.activeProxy(selectorTag); err == nil {
			activeGroup = groupIDFromTag(selected)
		}
		if activeGroup != "" {
			if picked, err := a.activeProxy(groupTagFor(activeGroup)); err == nil {
				if picked == autoTagFor(activeGroup) {
					mode[activeGroup] = "auto"
					if auto, err := a.activeProxy(autoTagFor(activeGroup)); err == nil {
						active[activeGroup] = auto
					}
				} else if picked != "" {
					if settings.FailoverOnly && !settings.Manual[activeGroup] && settings.Pinned[activeGroup] == picked {
						mode[activeGroup] = "auto"
					} else {
						mode[activeGroup] = "manual"
					}
					active[activeGroup] = picked
				}
			}
		}
	}
	response.ActiveGroup = activeGroup

	a.mu.Lock()
	defer a.mu.Unlock()
	for _, group := range groups {
		gs := groupStatus{ID: group.ID, Name: group.Name, Mode: "auto"}
		if m, ok := mode[group.ID]; ok {
			gs.Mode = m
		}
		gs.Active = active[group.ID]
		for _, node := range group.Nodes {
			availability := classifyNodeAvailability(node.Name)
			delays := a.delays[node.Tag]
			gs.Nodes = append(gs.Nodes, nodeStatus{
				Tag:            node.Tag,
				Name:           node.Name,
				Country:        availability.Country,
				DelayMS:        delays.combined(),
				GoogleDelayMS:  delays.Google,
				GeminiDelayMS:  delays.Gemini,
				ChatGPTDelayMS: delays.ChatGPT,
				Error:          delays.errorMessage(),
			})
		}
		response.Groups = append(response.Groups, gs)
	}
	return response, nil
}

func classifyNodeAvailability(name string) nodeAvailability {
	normalized := strings.ToLower(name)
	for _, rule := range countryAvailabilityRules {
		for _, marker := range rule.Markers {
			if strings.Contains(normalized, strings.ToLower(marker)) {
				return nodeAvailability{
					Country: rule.Country,
				}
			}
		}
	}
	return nodeAvailability{
		Country: "未识别",
	}
}

func (a *app) testNodeAsync(groupID, tag string, service delayTestService, removeFailed bool) {
	go func() {
		result, err := a.serviceDelayTest(tag, service)
		a.recordDelay(tag, result, service)
		if err != nil {
			a.appendLog(fmt.Sprintf("Service latency test %s failed: %v", tag, err))
			if removeFailed {
				a.removeFailedNodes(groupID, map[string]bool{tag: true})
			}
		}
	}()
}

// testGroupNodes limits concurrent probes, then applies all deletions in one
// configuration rebuild so the proxy only restarts once per full-group test.
func (a *app) testGroupNodes(groupID string, nodes []vlessNode, service delayTestService, removeFailed bool) {
	failed := make(map[string]bool)
	var failedMu sync.Mutex
	var workers sync.WaitGroup
	jobs := make(chan vlessNode)
	workerCount := delayTestConcurrency
	if len(nodes) < workerCount {
		workerCount = len(nodes)
	}
	for range workerCount {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for node := range jobs {
				delays, err := a.serviceDelayTest(node.Tag, service)
				a.recordDelay(node.Tag, delays, service)
				if err != nil {
					a.appendLog(fmt.Sprintf("Service latency test %s failed: %v", node.Tag, err))
					if removeFailed {
						failedMu.Lock()
						failed[node.Tag] = true
						failedMu.Unlock()
					}
				}
			}
		}()
	}
	for _, node := range nodes {
		jobs <- node
	}
	close(jobs)
	workers.Wait()
	if removeFailed {
		a.removeFailedNodes(groupID, failed)
	}
}

func (a *app) removeFailedNodes(groupID string, failed map[string]bool) {
	if len(failed) == 0 {
		return
	}
	a.cleanupMu.Lock()
	defer a.cleanupMu.Unlock()

	groups, err := a.readSubscriptions()
	if err != nil {
		a.appendLog(fmt.Sprintf("Could not remove failed nodes: %v", err))
		return
	}
	idx := findGroup(groups, groupID)
	if idx < 0 {
		return
	}
	kept := make([]vlessNode, 0, len(groups[idx].Nodes))
	for _, node := range groups[idx].Nodes {
		if !failed[node.Tag] {
			kept = append(kept, node)
		}
	}
	if len(kept) == len(groups[idx].Nodes) {
		return
	}
	if len(kept) == 0 {
		a.appendLog(fmt.Sprintf("Keeping %s: all %d nodes failed the delay test, so the group was not deleted.", groupID, len(failed)))
		return
	}
	removed := len(groups[idx].Nodes) - len(kept)
	groups[idx].Nodes = kept
	groups[idx].UpdatedAt = time.Now()
	// Keep failover-only automatic mode coherent if its pinned node was one of
	// the deleted members. Manual selections remain manual after this update.
	if settings, err := a.readAutoSwitchSettings(); err == nil {
		normalizeAutoSwitchSettings(&settings)
		if failed[settings.Pinned[groupID]] {
			settings.Pinned[groupID] = kept[0].Tag
			if err := a.writeAutoSwitchSettings(settings); err != nil {
				a.appendLog(fmt.Sprintf("Could not update the replacement node for %s: %v", groupID, err))
				return
			}
		}
	}
	if err := a.applyGroups(groups); err != nil {
		a.appendLog(fmt.Sprintf("Could not remove %d failed nodes from %s: %v", removed, groupID, err))
		return
	}
	a.mu.Lock()
	for tag := range failed {
		delete(a.delays, tag)
	}
	a.mu.Unlock()
	a.appendLog(fmt.Sprintf("Removed %d failed nodes from %s after delay tests.", removed, groupID))
}

func (a *app) recordDelay(tag string, delays serviceDelays, service delayTestService) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.delays[tag] = a.delays[tag].merge(delays, service)
}

func normalizeAutoSwitchSettings(settings *autoSwitchSettings) {
	if settings.Pinned == nil {
		settings.Pinned = make(map[string]string)
	}
	if settings.Manual == nil {
		settings.Manual = make(map[string]bool)
	}
}

func hasNodeTag(nodes []vlessNode, tag string) bool {
	for _, node := range nodes {
		if node.Tag == tag {
			return true
		}
	}
	return false
}

// captureActiveSelection converts the live selection into a pinned node before
// changing the configuration. This avoids a restart falling back to urltest.
func (a *app) captureActiveSelection(settings *autoSwitchSettings) {
	selectedGroup, err := a.activeProxy(selectorTag)
	if err != nil {
		return
	}
	groupID := groupIDFromTag(selectedGroup)
	if groupID == "" {
		return
	}
	groups, err := a.readSubscriptions()
	if err != nil {
		return
	}
	idx := findGroup(groups, groupID)
	if idx < 0 {
		return
	}
	picked, err := a.activeProxy(groupTagFor(groupID))
	if err != nil || picked == "" {
		return
	}
	manual := picked != autoTagFor(groupID)
	if !manual {
		picked, err = a.activeProxy(autoTagFor(groupID))
		if err != nil || picked == "" {
			return
		}
	}
	if !hasNodeTag(groups[idx].Nodes, picked) {
		return
	}
	normalizeAutoSwitchSettings(settings)
	settings.Pinned[groupID] = picked
	settings.Manual[groupID] = manual
}

// reconcileFailoverOnlyMode preserves an existing sing-box selection when the
// web agent is upgraded, then writes a selector that uses that node directly.
func (a *app) reconcileFailoverOnlyMode() error {
	settings, err := a.readAutoSwitchSettings()
	if err != nil || !settings.FailoverOnly {
		return err
	}
	normalizeAutoSwitchSettings(&settings)
	a.captureActiveSelection(&settings)
	if err := a.writeAutoSwitchSettings(settings); err != nil {
		return err
	}
	groups, err := a.readSubscriptions()
	if err != nil || len(groups) == 0 {
		return err
	}
	return a.applyGroups(groups)
}

// prepareSavedSelectionConfig updates config.json before starting sing-box so
// the selected group and each manual/automatic choice survive a later launch.
func (a *app) prepareSavedSelectionConfig() error {
	groups, err := a.readSubscriptions()
	if err != nil || len(groups) == 0 {
		return nil
	}
	settings, err := a.readAutoSwitchSettings()
	if err != nil {
		return err
	}
	normalizeAutoSwitchSettings(&settings)
	changed := false
	if findGroup(groups, settings.ActiveGroup) < 0 {
		settings.ActiveGroup = groups[0].ID
		changed = true
	}
	for _, group := range groups {
		if settings.Manual[group.ID] && hasNodeTag(group.Nodes, settings.Pinned[group.ID]) {
			continue
		}
		if !settings.FailoverOnly || len(group.Nodes) == 0 || hasNodeTag(group.Nodes, settings.Pinned[group.ID]) {
			continue
		}
		settings.Pinned[group.ID] = group.Nodes[0].Tag
		changed = true
	}
	if changed {
		if err := a.writeAutoSwitchSettings(settings); err != nil {
			return err
		}
	}
	whitelist, err := a.readWhitelist()
	if err != nil {
		return err
	}
	config, err := buildSubscriptionConfigWithSettings(groups, whitelist, settings)
	if err != nil {
		return err
	}
	if err := a.validateConfig(config); err != nil {
		return err
	}
	return os.WriteFile(a.config, config, 0o600)
}

func (a *app) restoreSavedSelection() {
	groups, err := a.readSubscriptions()
	if err != nil || len(groups) == 0 {
		return
	}
	settings, err := a.readAutoSwitchSettings()
	if err != nil {
		return
	}
	normalizeAutoSwitchSettings(&settings)
	groupID := settings.ActiveGroup
	if findGroup(groups, groupID) < 0 {
		groupID = groups[0].ID
	}
	idx := findGroup(groups, groupID)
	if idx < 0 {
		return
	}
	target := autoTagFor(groupID)
	if settings.Manual[groupID] && hasNodeTag(groups[idx].Nodes, settings.Pinned[groupID]) {
		target = settings.Pinned[groupID]
	} else if settings.FailoverOnly && hasNodeTag(groups[idx].Nodes, settings.Pinned[groupID]) {
		target = settings.Pinned[groupID]
	}
	if err := a.setProxy(selectorTag, groupTagFor(groupID)); err != nil {
		a.appendLog(fmt.Sprintf("Could not restore saved group %s: %v", groupID, err))
		return
	}
	if err := a.setProxy(groupTagFor(groupID), target); err != nil {
		a.appendLog(fmt.Sprintf("Could not restore saved node %s: %v", target, err))
		return
	}
	a.appendLog(fmt.Sprintf("Restored saved selection: %s (%s).", groupID, target))
}

func (a *app) failoverTarget(group subscriptionGroup, settings autoSwitchSettings) (string, error) {
	if tag := settings.Pinned[group.ID]; hasNodeTag(group.Nodes, tag) {
		return tag, nil
	}
	if picked, err := a.activeProxy(groupTagFor(group.ID)); err == nil {
		if picked == autoTagFor(group.ID) {
			picked, _ = a.activeProxy(autoTagFor(group.ID))
		}
		if hasNodeTag(group.Nodes, picked) {
			return picked, nil
		}
	}
	return a.firstWorkingNode(group.Nodes, "")
}

func (a *app) firstWorkingNode(nodes []vlessNode, skip string) (string, error) {
	type probeResult struct {
		tag string
		err error
	}
	results := make(chan probeResult, len(nodes))
	count := 0
	for _, node := range nodes {
		if node.Tag == skip {
			continue
		}
		count++
		go func(tag string) {
			delays, err := a.serviceDelayTest(tag, delayTestAll)
			a.recordDelay(tag, delays, delayTestAll)
			results <- probeResult{tag: tag, err: err}
		}(node.Tag)
	}
	if count == 0 {
		return "", errors.New("no alternate nodes available")
	}
	for range count {
		result := <-results
		if result.err == nil {
			return result.tag, nil
		}
	}
	return "", errors.New("no working nodes found")
}

// runFailoverMonitor only probes the active automatic node. A failed probe
// selects one working alternate; ordinary manual latency tests never enter it.
func (a *app) runFailoverMonitor() {
	// Run once on startup to migrate an existing urltest selection to a pinned
	// selector before the first user-triggered latency test.
	a.checkActiveFailover()
	ticker := time.NewTicker(failoverCheckInterval)
	defer ticker.Stop()
	for range ticker.C {
		a.checkActiveFailover()
	}
}

func (a *app) checkActiveFailover() {
	if !a.status().Running {
		return
	}
	settings, err := a.readAutoSwitchSettings()
	if err != nil || !settings.FailoverOnly {
		return
	}
	normalizeAutoSwitchSettings(&settings)
	groupTag, err := a.activeProxy(selectorTag)
	if err != nil {
		return
	}
	groupID := groupIDFromTag(groupTag)
	if groupID == "" || settings.Manual[groupID] {
		return
	}
	groups, err := a.readSubscriptions()
	if err != nil {
		return
	}
	idx := findGroup(groups, groupID)
	if idx < 0 {
		return
	}
	current := settings.Pinned[groupID]
	wasPinned := current != ""
	if !hasNodeTag(groups[idx].Nodes, current) {
		current, err = a.failoverTarget(groups[idx], settings)
		if err != nil {
			return
		}
	}
	if !wasPinned {
		// Older settings used urltest directly. Pin its current result now so
		// later latency tests cannot replace the active node.
		if err := a.setProxy(groupTagFor(groupID), current); err != nil {
			a.appendLog(fmt.Sprintf("Could not pin automatic node %s: %v", current, err))
			return
		}
		settings.Pinned[groupID] = current
		settings.Manual[groupID] = false
		if err := a.writeAutoSwitchSettings(settings); err != nil {
			a.appendLog(fmt.Sprintf("Pinned %s but could not save failover settings: %v", current, err))
			return
		}
		a.appendLog(fmt.Sprintf("Failover-only mode pinned automatic node %s.", current))
	}
	if delays, probeErr := a.serviceDelayTest(current, delayTestAll); probeErr == nil {
		a.recordDelay(current, delays, delayTestAll)
		return
	} else {
		a.recordDelay(current, delays, delayTestAll)
	}

	a.failoverMu.Lock()
	defer a.failoverMu.Unlock()
	// Re-read after waiting for another failure check or a user selection.
	settings, err = a.readAutoSwitchSettings()
	if err != nil || !settings.FailoverOnly || settings.Manual[groupID] || settings.Pinned[groupID] != current {
		return
	}
	next, err := a.firstWorkingNode(groups[idx].Nodes, current)
	if err != nil {
		a.appendLog(fmt.Sprintf("Failover check %s failed; no alternate node is available.", current))
		return
	}
	if err := a.setProxy(groupTagFor(groupID), next); err != nil {
		a.appendLog(fmt.Sprintf("Failover switch %s -> %s failed: %v", current, next, err))
		return
	}
	normalizeAutoSwitchSettings(&settings)
	settings.Pinned[groupID] = next
	if err := a.writeAutoSwitchSettings(settings); err != nil {
		a.appendLog(fmt.Sprintf("Failover switched to %s but could not save selection: %v", next, err))
		return
	}
	a.appendLog(fmt.Sprintf("Failover switched %s -> %s after the active node failed.", current, next))
}

func (a *app) serviceDelayTest(tag string, service delayTestService) (serviceDelays, error) {
	type serviceProbeResult struct {
		name  string
		delay int
		err   error
	}
	probes := []latencyServiceProbe{
		{name: "Google", url: googleLatencyTestURL},
		{name: "Gemini", url: geminiLatencyTestURL},
		{name: "ChatGPT", url: chatGPTLatencyTestURL},
	}
	var delays serviceDelays
	var failures []string
	if service != delayTestAll {
		probes = filterServiceProbes(probes, service)
	}
	results := make(chan serviceProbeResult, len(probes))
	for _, probe := range probes {
		go func(probe latencyServiceProbe) {
			delay, err := a.delayTestURL(tag, probe.url)
			results <- serviceProbeResult{name: probe.name, delay: delay, err: err}
		}(probe)
	}
	for range probes {
		result := <-results
		delay, err := result.delay, result.err
		if err != nil {
			delay = -1
			failures = append(failures, fmt.Sprintf("%s: %v", result.name, err))
		}
		switch result.name {
		case "Google":
			delays.Google = delay
		case "Gemini":
			delays.Gemini = delay
		case "ChatGPT":
			delays.ChatGPT = delay
		}
	}
	if len(failures) > 0 {
		return delays, errors.New(strings.Join(failures, "; "))
	}
	return delays, nil
}

func filterServiceProbes(probes []latencyServiceProbe, service delayTestService) []latencyServiceProbe {
	wanted := map[delayTestService]string{
		delayTestGoogle:  "Google",
		delayTestGemini:  "Gemini",
		delayTestChatGPT: "ChatGPT",
	}[service]
	for _, probe := range probes {
		if probe.name == wanted {
			return []latencyServiceProbe{probe}
		}
	}
	return nil
}

func (a *app) delayTestURL(tag, testURL string) (int, error) {
	endpoint := fmt.Sprintf("%s/proxies/%s/delay?timeout=10000&url=%s", clashAPIURL, url.PathEscape(tag), url.QueryEscape(testURL))
	response, err := (&http.Client{Timeout: 12 * time.Second}).Get(endpoint)
	if err != nil {
		return 0, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("delay test returned HTTP %d", response.StatusCode)
	}
	var payload struct {
		Delay int `json:"delay"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		return 0, err
	}
	return payload.Delay, nil
}

func (a *app) activeProxy(tag string) (string, error) {
	response, err := (&http.Client{Timeout: 3 * time.Second}).Get(fmt.Sprintf("%s/proxies", clashAPIURL))
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("read proxy state returned HTTP %d", response.StatusCode)
	}
	var payload clashProxiesResponse
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		return "", err
	}
	return payload.Proxies[tag].Now, nil
}

func (a *app) setProxy(group, selected string) error {
	payload, err := json.Marshal(map[string]string{"name": selected})
	if err != nil {
		return err
	}
	request, err := http.NewRequest(http.MethodPut, fmt.Sprintf("%s/proxies/%s", clashAPIURL, url.PathEscape(group)), strings.NewReader(string(payload)))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := (&http.Client{Timeout: 5 * time.Second}).Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("change selection returned HTTP %d", response.StatusCode)
	}
	return nil
}
func (a *app) importSubscription(rawURL string) (subscriptionGroup, error) {
	parsedURL, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || parsedURL.Scheme != "https" || parsedURL.Host == "" {
		return subscriptionGroup{}, errors.New("subscription URL must be a valid HTTPS URL")
	}
	name := strings.TrimSpace(parsedURL.Fragment)
	if name == "" {
		name = parsedURL.Host
	}
	parsedURL.Fragment = ""
	cleanURL := parsedURL.String()

	nodes, err := downloadNodes(cleanURL)
	if err != nil {
		return subscriptionGroup{}, err
	}
	groups, _ := a.readSubscriptions()
	group := subscriptionGroup{
		ID:        nextGroupID(groups),
		URL:       cleanURL,
		Name:      name,
		UpdatedAt: time.Now(),
		Nodes:     nodes,
	}
	relabelNodes(group.Nodes, group.ID)
	groups = append(groups, group)
	if err := a.applyGroups(groups); err != nil {
		return subscriptionGroup{}, err
	}
	a.appendLog(fmt.Sprintf("Imported subscription %q as %s with %d nodes.", group.Name, group.ID, len(nodes)))
	return group, nil
}

func (a *app) refreshSubscription(groupID string) (subscriptionGroup, error) {
	groups, err := a.readSubscriptions()
	if err != nil {
		return subscriptionGroup{}, err
	}
	idx := findGroup(groups, groupID)
	if idx < 0 {
		return subscriptionGroup{}, errors.New("subscription group not found")
	}
	nodes, err := downloadNodes(groups[idx].URL)
	if err != nil {
		return subscriptionGroup{}, err
	}
	relabelNodes(nodes, groupID)
	groups[idx].Nodes = nodes
	groups[idx].UpdatedAt = time.Now()
	if err := a.applyGroups(groups); err != nil {
		return subscriptionGroup{}, err
	}
	a.appendLog(fmt.Sprintf("Refreshed subscription %s (%d nodes).", groupID, len(nodes)))
	return groups[idx], nil
}

func (a *app) deleteSubscription(groupID string) error {
	groups, err := a.readSubscriptions()
	if err != nil {
		return err
	}
	idx := findGroup(groups, groupID)
	if idx < 0 {
		return errors.New("subscription group not found")
	}
	groups = append(groups[:idx], groups[idx+1:]...)
	if err := a.applyGroups(groups); err != nil {
		return err
	}
	a.appendLog(fmt.Sprintf("Deleted subscription %s.", groupID))
	return nil
}

// applyGroups rebuilds the sing-box configuration from all groups, validates it,
// writes it together with subscriptions.json, and restarts sing-box if it was running.
func (a *app) applyGroups(groups []subscriptionGroup) error {
	a.applyMu.Lock()
	defer a.applyMu.Unlock()
	whitelist, _ := a.readWhitelist()
	autoSwitch, _ := a.readAutoSwitchSettings()
	config, err := buildSubscriptionConfigWithSettings(groups, whitelist, autoSwitch)
	if err != nil {
		return err
	}
	if err := a.validateConfig(config); err != nil {
		return err
	}
	wasRunning := a.status().Running
	if wasRunning {
		if err := a.stopAndWait(); err != nil {
			return err
		}
	}
	if err := os.WriteFile(a.config, config, 0o600); err != nil {
		return err
	}
	if err := a.writeSubscriptions(groups); err != nil {
		return err
	}
	if wasRunning {
		if err := a.start(); err != nil {
			return fmt.Errorf("configuration updated but sing-box could not restart: %w", err)
		}
	}
	return nil
}

func (a *app) validateConfig(config []byte) error {
	if _, err := os.Stat(a.binary); errors.Is(err, os.ErrNotExist) {
		return nil
	}
	temporaryPath := filepath.Join(a.dataDir, "config.check.json")
	defer os.Remove(temporaryPath)
	if err := os.WriteFile(temporaryPath, config, 0o600); err != nil {
		return err
	}
	checkCmd := exec.Command(a.binary, "check", "-c", temporaryPath)
	checkCmd.Dir = a.dataDir
	configureBackgroundCommand(checkCmd)
	output, err := checkCmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("generated sing-box configuration is invalid: %s", strings.TrimSpace(string(output)))
	}
	return nil
}

func (a *app) readSubscriptions() ([]subscriptionGroup, error) {
	contents, err := os.ReadFile(a.subscriptions)
	if err != nil {
		return nil, err
	}
	var groups []subscriptionGroup
	if err := json.Unmarshal(contents, &groups); err != nil {
		return nil, err
	}
	return groups, nil
}

func (a *app) writeSubscriptions(groups []subscriptionGroup) error {
	contents, err := json.Marshal(groups)
	if err != nil {
		return err
	}
	return os.WriteFile(a.subscriptions, contents, 0o600)
}

// buildRouteRules assembles the sing-box route rules: private IPs and custom
// whitelist domains go direct first, then the geosite/geoip CN rule sets,
// with everything else falling through to the proxy selector.
func buildRouteRules(whitelist []string) []any {
	rules := []any{
		map[string]any{"ip_is_private": true, "outbound": "direct"},
	}
	if len(whitelist) > 0 {
		rules = append(rules, map[string]any{"domain_suffix": whitelist, "outbound": "direct"})
	}
	rules = append(rules,
		map[string]any{"rule_set": []string{"geosite-private"}, "outbound": "direct"},
		map[string]any{"rule_set": []string{"geosite-cn"}, "outbound": "direct"},
		map[string]any{"rule_set": []string{"geoip-cn"}, "outbound": "direct"},
	)
	return rules
}

func (a *app) ensureWhitelist() error {
	if _, err := os.Stat(a.whitelist); errors.Is(err, os.ErrNotExist) {
		return os.WriteFile(a.whitelist, []byte("[]"), 0o600)
	}
	return nil
}

func (a *app) readWhitelist() ([]string, error) {
	contents, err := os.ReadFile(a.whitelist)
	if err != nil {
		return nil, err
	}
	var domains []string
	if err := json.Unmarshal(contents, &domains); err != nil {
		return nil, err
	}
	return domains, nil
}

func (a *app) writeWhitelist(domains []string) error {
	contents, err := json.MarshalIndent(domains, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(a.whitelist, contents, 0o600)
}

// applyWhitelist rebuilds the sing-box config so that newly added/removed
// whitelist domains take effect. If no subscriptions exist there is nothing
// to rebuild (the default config is direct-only).
func (a *app) applyWhitelist() error {
	groups, err := a.readSubscriptions()
	if err != nil || len(groups) == 0 {
		return nil
	}
	return a.applyGroups(groups)
}

func (a *app) ensureAutoSwitchSettings() error {
	if _, err := os.Stat(a.autoSwitch); errors.Is(err, os.ErrNotExist) {
		return a.writeAutoSwitchSettings(autoSwitchSettings{})
	}
	return nil
}

func (a *app) readAutoSwitchSettings() (autoSwitchSettings, error) {
	contents, err := os.ReadFile(a.autoSwitch)
	if err != nil {
		return autoSwitchSettings{}, err
	}
	var settings autoSwitchSettings
	if err := json.Unmarshal(contents, &settings); err != nil {
		return autoSwitchSettings{}, err
	}
	return settings, nil
}

func (a *app) writeAutoSwitchSettings(settings autoSwitchSettings) error {
	contents, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(a.autoSwitch, contents, 0o600)
}

func (a *app) ensureFailedNodeCleanupSettings() error {
	if _, err := os.Stat(a.nodeCleanup); errors.Is(err, os.ErrNotExist) {
		return a.writeFailedNodeCleanupSettings(failedNodeCleanupSettings{})
	}
	return nil
}

func (a *app) readFailedNodeCleanupSettings() (failedNodeCleanupSettings, error) {
	contents, err := os.ReadFile(a.nodeCleanup)
	if err != nil {
		return failedNodeCleanupSettings{}, err
	}
	var settings failedNodeCleanupSettings
	if err := json.Unmarshal(contents, &settings); err != nil {
		return failedNodeCleanupSettings{}, err
	}
	return settings, nil
}

func (a *app) writeFailedNodeCleanupSettings(settings failedNodeCleanupSettings) error {
	contents, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(a.nodeCleanup, contents, 0o600)
}

func (a *app) applyAutoSwitchSettings() error {
	groups, err := a.readSubscriptions()
	if err != nil || len(groups) == 0 {
		return nil
	}
	return a.applyGroups(groups)
}

func normalizeDomain(input string) string {
	d := strings.TrimSpace(input)
	d = strings.ToLower(d)
	if i := strings.Index(d, "://"); i >= 0 {
		d = d[i+3:]
	}
	if i := strings.Index(d, "/"); i >= 0 {
		d = d[:i]
	}
	if i := strings.Index(d, ":"); i >= 0 {
		d = d[:i]
	}
	// sing-box domain_suffix accepts a leading dot. Treat *.vip as .vip so
	// users can add either familiar wildcard spelling without storing '*'.
	if strings.HasPrefix(d, "*.") {
		d = d[1:]
	}
	if strings.Contains(d, "*") {
		return ""
	}
	d = strings.TrimPrefix(d, "www.")
	return strings.TrimSpace(d)
}

func (a *app) handleGetWhitelist(w http.ResponseWriter, _ *http.Request) {
	domains, _ := a.readWhitelist()
	writeJSON(w, http.StatusOK, map[string]any{"domains": domains})
}

func (a *app) handleAddWhitelist(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Domain string `json:"domain"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	domain := normalizeDomain(req.Domain)
	if domain == "" {
		writeError(w, http.StatusBadRequest, errors.New("domain is required"))
		return
	}
	domains, _ := a.readWhitelist()
	for _, d := range domains {
		if d == domain {
			writeJSON(w, http.StatusOK, map[string]any{"domains": domains})
			return
		}
	}
	domains = append(domains, domain)
	if err := a.writeWhitelist(domains); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if err := a.applyWhitelist(); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"domains": domains})
}

func (a *app) handleDeleteWhitelist(w http.ResponseWriter, r *http.Request) {
	domain := normalizeDomain(r.PathValue("domain"))
	domains, _ := a.readWhitelist()
	filtered := make([]string, 0, len(domains))
	for _, d := range domains {
		if d != domain {
			filtered = append(filtered, d)
		}
	}
	if err := a.writeWhitelist(filtered); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if err := a.applyWhitelist(); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"domains": filtered})
}

func (a *app) handleUpdateWhitelist(w http.ResponseWriter, r *http.Request) {
	oldDomain := normalizeDomain(r.PathValue("domain"))
	var req struct {
		Domain string `json:"domain"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16*1024)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	newDomain := normalizeDomain(req.Domain)
	if oldDomain == "" || newDomain == "" {
		writeError(w, http.StatusBadRequest, errors.New("domain is required"))
		return
	}
	domains, err := a.readWhitelist()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	found := false
	updated := make([]string, 0, len(domains))
	seen := make(map[string]bool)
	for _, domain := range domains {
		if domain == oldDomain {
			domain = newDomain
			found = true
		}
		if !seen[domain] {
			updated = append(updated, domain)
			seen[domain] = true
		}
	}
	if !found {
		writeError(w, http.StatusNotFound, errors.New("whitelist domain not found"))
		return
	}
	if err := a.writeWhitelist(updated); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if err := a.applyWhitelist(); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"domains": updated})
}

func (a *app) handleRouteRules(w http.ResponseWriter, _ *http.Request) {
	domains, err := a.readWhitelist()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, routeRulesResponse{Rules: a.routeRules(domains)})
}

func (a *app) routeRules(whitelist []string) []routeRule {
	rules := []routeRule{
		{ID: "private-ip", Name: "私有网络", Kind: "IP", Value: "局域网 / 私有 IP", Outbound: "直连", Source: "内置", Editable: false},
		{ID: "geosite-private", Name: "私有域名", Kind: "规则集", Value: "geosite-private", Outbound: "直连", Source: "内置", Editable: false},
		{ID: "geosite-cn", Name: "中国大陆域名", Kind: "规则集", Value: "geosite-cn", Outbound: "直连", Source: "内置", Editable: false},
		{ID: "geoip-cn", Name: "中国大陆 IP", Kind: "规则集", Value: "geoip-cn", Outbound: "直连", Source: "内置", Editable: false},
	}
	for _, domain := range whitelist {
		rules = append(rules, routeRule{
			ID: "whitelist:" + domain, Name: "自定义白名单", Kind: "域名及子域名", Value: domain, Outbound: "直连", Source: "自定义", Editable: true,
		})
	}
	rules = append(rules, routeRule{ID: "final", Name: "其余流量", Kind: "默认", Value: "未匹配任何直连规则", Outbound: "代理", Source: "内置", Editable: false})
	return rules
}

func downloadNodes(subscriptionURL string) ([]vlessNode, error) {
	request, err := http.NewRequest(http.MethodGet, subscriptionURL, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("User-Agent", "singbox-web/0.1")
	response, err := (&http.Client{Timeout: 20 * time.Second}).Do(request)
	if err != nil {
		return nil, fmt.Errorf("download subscription: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download subscription: server returned HTTP %d", response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, 5*1024*1024))
	if err != nil {
		return nil, err
	}
	content, err := decodeSubscription(body)
	if err != nil {
		return nil, err
	}
	return parseSubscriptionNodes(content)
}

func decodeSubscription(body []byte) (string, error) {
	raw := strings.TrimSpace(string(body))
	if json.Valid([]byte(raw)) {
		return raw, nil
	}
	lower := strings.ToLower(raw)
	if strings.HasPrefix(lower, "vless://") || strings.HasPrefix(lower, "vmess://") || strings.HasPrefix(lower, "trojan://") || strings.HasPrefix(lower, "ss://") {
		return raw, nil
	}
	compact := strings.Map(func(r rune) rune {
		if r == '\r' || r == '\n' || r == ' ' || r == '\t' {
			return -1
		}
		return r
	}, raw)
	compact = strings.ReplaceAll(strings.ReplaceAll(compact, "-", "+"), "_", "/")
	switch len(compact) % 4 {
	case 2:
		compact += "=="
	case 3:
		compact += "="
	}
	decoded, err := base64.StdEncoding.DecodeString(compact)
	if err != nil || !strings.Contains(string(decoded), "://") {
		return "", errors.New("subscription does not contain recognizable share links")
	}
	return string(decoded), nil
}

func parseSubscriptionNodes(content string) ([]vlessNode, error) {
	if json.Valid([]byte(content)) {
		return parseSingboxConfigNodes(content)
	}
	return parseVLESSNodes(content)
}

func parseSingboxConfigNodes(content string) ([]vlessNode, error) {
	var config struct {
		Outbounds []map[string]any `json:"outbounds"`
	}
	if err := json.Unmarshal([]byte(content), &config); err != nil {
		return nil, err
	}

	nodes := make([]vlessNode, 0)
	for _, outbound := range config.Outbounds {
		typeName, _ := outbound["type"].(string)
		if typeName != "vless" {
			continue
		}
		// Keep the provider's full protocol settings, but use stable local tags.
		name, _ := outbound["tag"].(string)
		tag := fmt.Sprintf("node-%02d", len(nodes)+1)
		if name == "" {
			name = tag
		}
		sanitizeVLESSFlow(outbound)
		outbound["tag"] = tag
		nodes = append(nodes, vlessNode{Tag: tag, Name: name, Outbound: outbound})
	}
	if len(nodes) == 0 {
		return nil, errors.New("the sing-box subscription has no VLESS nodes")
	}
	return nodes, nil
}

func parseVLESSNodes(content string) ([]vlessNode, error) {
	var nodes []vlessNode
	for _, item := range strings.Fields(content) {
		if !strings.HasPrefix(strings.ToLower(item), "vless://") {
			continue
		}
		node, err := parseVLESS(item, len(nodes)+1)
		if err == nil {
			nodes = append(nodes, node)
		}
	}
	if len(nodes) == 0 {
		return nil, errors.New("the subscription has no supported VLESS nodes")
	}
	return nodes, nil
}

func parseVLESS(raw string, number int) (vlessNode, error) {
	parsedURL, err := url.Parse(raw)
	if err != nil || parsedURL.User == nil || parsedURL.User.Username() == "" {
		return vlessNode{}, errors.New("invalid VLESS link")
	}
	port, err := strconv.Atoi(parsedURL.Port())
	if err != nil || port < 1 || port > 65535 || parsedURL.Hostname() == "" {
		return vlessNode{}, errors.New("invalid VLESS server address")
	}
	query := parsedURL.Query()
	tag := fmt.Sprintf("node-%02d", number)
	outbound := map[string]any{
		"type":        "vless",
		"tag":         tag,
		"server":      parsedURL.Hostname(),
		"server_port": port,
		"uuid":        parsedURL.User.Username(),
	}
	if flow := query.Get("flow"); flow == "xtls-rprx-vision" {
		outbound["flow"] = flow
	}

	security := query.Get("security")
	if security == "tls" || security == "reality" {
		tls := map[string]any{"enabled": true}
		serverName := query.Get("sni")
		if serverName == "" {
			serverName = parsedURL.Hostname()
		}
		tls["server_name"] = serverName
		if fingerprint := query.Get("fp"); fingerprint != "" {
			tls["utls"] = map[string]any{"enabled": true, "fingerprint": fingerprint}
		}
		if security == "reality" {
			publicKey := query.Get("pbk")
			if publicKey == "" {
				return vlessNode{}, errors.New("Reality node is missing its public key")
			}
			reality := map[string]any{"enabled": true, "public_key": publicKey}
			if shortID := query.Get("sid"); shortID != "" {
				reality["short_id"] = shortID
			}
			tls["reality"] = reality
		}
		outbound["tls"] = tls
	} else if security != "" && security != "none" {
		return vlessNode{}, fmt.Errorf("unsupported VLESS security %q", security)
	}

	switch query.Get("type") {
	case "", "tcp", "raw":
	case "ws":
		transport := map[string]any{"type": "ws"}
		if path := query.Get("path"); path != "" {
			transport["path"] = path
		}
		if host := query.Get("host"); host != "" {
			transport["headers"] = map[string]any{"Host": host}
		}
		outbound["transport"] = transport
	default:
		return vlessNode{}, errors.New("unsupported VLESS transport")
	}
	name := parsedURL.Fragment
	if name == "" {
		name = tag
	}
	return vlessNode{Tag: tag, Name: name, Outbound: outbound}, nil
}

// sing-box only supports the standard Vision flow for VLESS. Some providers
// publish Xray-specific suffixes such as "xtls-rprx-vision-udp44"; dropping
// those suffix variants keeps the node importable instead of rejecting the
// entire subscription configuration.
func sanitizeVLESSFlow(outbound map[string]any) {
	flow, _ := outbound["flow"].(string)
	if flow != "" && flow != "xtls-rprx-vision" {
		delete(outbound, "flow")
	}
}

// buildSubscriptionConfig generates a sing-box configuration with one selector
// per subscription group. The top-level "proxy" selector switches between groups;
// each group selector offers its own urltest (auto) plus individual nodes.
func buildSubscriptionConfig(groups []subscriptionGroup, whitelist []string) ([]byte, error) {
	return buildSubscriptionConfigWithSettings(groups, whitelist, autoSwitchSettings{})
}

func buildSubscriptionConfigWithSettings(groups []subscriptionGroup, whitelist []string, autoSwitch autoSwitchSettings) ([]byte, error) {
	outbounds := make([]any, 0, 8)
	groupSelectors := make([]string, 0, len(groups))
	for _, group := range groups {
		if len(group.Nodes) == 0 {
			continue
		}
		nodeTags := make([]string, 0, len(group.Nodes))
		for _, node := range group.Nodes {
			nodeTags = append(nodeTags, node.Tag)
		}
		autoTag := autoTagFor(group.ID)
		groupTag := groupTagFor(group.ID)
		urltest := map[string]any{
			"type":      "urltest",
			"tag":       autoTag,
			"outbounds": nodeTags,
			// sing-box urltest supports one URL only. The application-level
			// service checks below cover Google, Gemini, and ChatGPT together.
			"url":       googleLatencyTestURL,
			"interval":  "5m",
			"tolerance": 50,
		}
		if autoSwitch.FailoverOnly {
			// A very high tolerance prevents switching to a merely faster member;
			// an unavailable member still causes urltest to select a working one.
			urltest["tolerance"] = failoverTolerance
		}
		outbounds = append(outbounds, urltest)
		defaultOutbound := autoTag
		if autoSwitch.FailoverOnly {
			// Point the selector at a concrete pinned node. urltest remains in the
			// config for latency-optimized mode, but cannot alter active traffic in
			// failover-only mode.
			if pinned := autoSwitch.Pinned[group.ID]; hasNodeTag(group.Nodes, pinned) {
				defaultOutbound = pinned
			} else {
				defaultOutbound = group.Nodes[0].Tag
			}
		}
		outbounds = append(outbounds, map[string]any{
			"type":      "selector",
			"tag":       groupTag,
			"outbounds": append([]string{autoTag}, nodeTags...),
			"default":   defaultOutbound,
		})
		groupSelectors = append(groupSelectors, groupTag)
		for _, node := range group.Nodes {
			outbounds = append(outbounds, node.Outbound)
		}
	}
	if len(groupSelectors) == 0 {
		return nil, errors.New("no subscriptions with nodes are configured")
	}
	outbounds = append([]any{map[string]any{
		"type":      "selector",
		"tag":       selectorTag,
		"outbounds": groupSelectors,
		"default":   groupSelectors[0],
	}}, outbounds...)
	outbounds = append(outbounds, map[string]any{"type": "direct", "tag": "direct"})

	config := map[string]any{
		"log": map[string]any{"level": "info", "timestamp": true},
		"inbounds": []any{map[string]any{
			"type": "socks", "tag": "socks-in", "listen": "127.0.0.1", "listen_port": 2080,
		}, map[string]any{
			"type": "http", "tag": "http-in", "listen": "127.0.0.1", "listen_port": 2081,
		}},
		"outbounds": outbounds,
		"route": map[string]any{
			"rule_set": []any{
				map[string]any{"type": "local", "format": "binary", "tag": "geosite-cn", "path": "srss/geosite-cn.srs"},
				map[string]any{"type": "local", "format": "binary", "tag": "geoip-cn", "path": "srss/geoip-cn.srs"},
				map[string]any{"type": "local", "format": "binary", "tag": "geosite-private", "path": "srss/geosite-private.srs"},
			},
			"rules": buildRouteRules(whitelist),
			"final": selectorTag,
		},
		"experimental": map[string]any{
			"clash_api": map[string]any{
				"external_controller": "127.0.0.1:9090",
			},
		},
	}
	return json.MarshalIndent(config, "", "  ")
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]string{"error": err.Error()})
}

func withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "http://localhost:5173")
		w.Header().Set("Access-Control-Allow-Methods", "GET, PUT, POST, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
