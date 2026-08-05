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
	listenAddress  = "127.0.0.1:8787"
	maxLogBytes    = 128 * 1024
	clashAPIURL    = "http://127.0.0.1:9090"
	selectorTag    = "proxy"
	latencyTestURL = "https://www.gstatic.com/generate_204"
)

//go:embed defaults/config.json
var defaultConfig []byte

type app struct {
	mu            sync.Mutex
	dataDir       string
	config        string
	binary        string
	subscriptions string
	whitelist     string
	cmd           *exec.Cmd
	cancel        context.CancelFunc
	started       time.Time
	lastExit      string
	logs          string
	delays        map[string]int
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
	Tag            string `json:"tag"`
	Name           string `json:"name"`
	Country        string `json:"country"`
	GeminiSupport  string `json:"geminiSupport"`
	ChatGPTSupport string `json:"chatgptSupport"`
	DelayMS        int    `json:"delayMs"`
	Error          string `json:"error,omitempty"`
}

type nodeAvailability struct {
	Country        string
	GeminiSupport  string
	ChatGPTSupport string
}

type countryAvailabilityRule struct {
	Country        string
	Markers        []string
	GeminiSupport  string
	ChatGPTSupport string
}

const (
	availabilitySupported   = "supported"
	availabilityUnsupported = "unsupported"
	availabilityUnknown     = "unknown"
)

// These are static region rules, not an IP geolocation or an end-to-end service test.
var countryAvailabilityRules = []countryAvailabilityRule{
	{Country: "香港", Markers: []string{"🇭🇰", "香港", "hong kong"}, GeminiSupport: availabilitySupported, ChatGPTSupport: availabilityUnsupported},
	{Country: "美国", Markers: []string{"🇺🇸", "美国", "united states", "usa"}, GeminiSupport: availabilitySupported, ChatGPTSupport: availabilitySupported},
	{Country: "澳大利亚", Markers: []string{"🇦🇺", "澳大利亚", "australia"}, GeminiSupport: availabilitySupported, ChatGPTSupport: availabilitySupported},
	{Country: "韩国", Markers: []string{"🇰🇷", "韩国", "south korea", "korea"}, GeminiSupport: availabilitySupported, ChatGPTSupport: availabilitySupported},
	{Country: "德国", Markers: []string{"🇩🇪", "德国", "germany"}, GeminiSupport: availabilitySupported, ChatGPTSupport: availabilitySupported},
	{Country: "阿联酋", Markers: []string{"🇦🇪", "阿联酋", "迪拜", "united arab emirates", "uae", "dubai"}, GeminiSupport: availabilitySupported, ChatGPTSupport: availabilitySupported},
	{Country: "英国", Markers: []string{"🇬🇧", "英国", "united kingdom", "uk", "great britain"}, GeminiSupport: availabilitySupported, ChatGPTSupport: availabilitySupported},
	{Country: "印度", Markers: []string{"🇮🇳", "印度", "india"}, GeminiSupport: availabilitySupported, ChatGPTSupport: availabilitySupported},
	{Country: "新加坡", Markers: []string{"🇸🇬", "新加坡", "singapore"}, GeminiSupport: availabilitySupported, ChatGPTSupport: availabilitySupported},
	{Country: "日本", Markers: []string{"🇯🇵", "日本", "japan"}, GeminiSupport: availabilitySupported, ChatGPTSupport: availabilitySupported},
}

type selectionRequest struct {
	GroupID string `json:"groupId"`
	Mode    string `json:"mode"`
	Tag     string `json:"tag"`
}

type clashProxiesResponse struct {
	Proxies map[string]clashProxy `json:"proxies"`
}

type clashProxy struct {
	Now string `json:"now"`
}

func main() {
	dataDir := os.Getenv("SINGBOX_WEB_DATA_DIR")
	if dataDir == "" {
		dataDir = "data"
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
		delays:        make(map[string]int),
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

	server := &http.Server{
		Addr:              listenAddress,
		Handler:           withCORS(application.handler()),
		ReadHeaderTimeout: 5 * time.Second,
	}

	log.Printf("singbox-web API is listening on http://%s", listenAddress)
	log.Printf("sing-box binary: %s", application.binary)
	if err := server.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}

func (a *app) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/status", a.handleStatus)
	mux.HandleFunc("GET /api/config", a.handleGetConfig)
	mux.HandleFunc("PUT /api/config", a.handleSaveConfig)
	mux.HandleFunc("GET /api/logs", a.handleLogs)
	mux.HandleFunc("POST /api/start", a.handleStart)
	mux.HandleFunc("POST /api/stop", a.handleStop)
	mux.HandleFunc("GET /api/system-proxy", a.handleSystemProxyStatus)
	mux.HandleFunc("POST /api/system-proxy", a.handleSystemProxyUpdate)
	mux.HandleFunc("GET /api/subscriptions", a.handleListSubscriptions)
	mux.HandleFunc("POST /api/subscriptions", a.handleImportSubscription)
	mux.HandleFunc("POST /api/subscriptions/{id}/refresh", a.handleRefreshSubscription)
	mux.HandleFunc("DELETE /api/subscriptions/{id}", a.handleDeleteSubscription)
	mux.HandleFunc("GET /api/nodes", a.handleNodes)
	mux.HandleFunc("POST /api/nodes/test", a.handleTestAllNodes)
	mux.HandleFunc("POST /api/nodes/{tag}/test", a.handleTestNode)
	mux.HandleFunc("POST /api/selection", a.handleSelection)
	mux.HandleFunc("GET /api/whitelist", a.handleGetWhitelist)
	mux.HandleFunc("POST /api/whitelist", a.handleAddWhitelist)
	mux.HandleFunc("DELETE /api/whitelist/{domain}", a.handleDeleteWhitelist)
	return mux
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

	if _, err := os.Stat(binary); err != nil {
		return fmt.Errorf("sing-box binary not found at %s; place it there or set SINGBOX_BINARY", binary)
	}
	checkCmd := exec.Command(binary, "check", "-c", config)
	checkCmd.Dir = filepath.Dir(config)
	if output, err := checkCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("configuration is invalid: %s", strings.TrimSpace(string(output)))
	}

	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, binary, "run", "-c", config)
	cmd.Dir = filepath.Dir(config)
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

func (a *app) handleStart(w http.ResponseWriter, _ *http.Request) {
	if err := a.start(); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusAccepted, a.status())
}

func (a *app) handleStop(w http.ResponseWriter, _ *http.Request) {
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

func systemProxyStatus() (systemProxyResponse, error) {
	if runtime.GOOS != "windows" {
		return systemProxyResponse{Supported: false}, nil
	}
	output, err := exec.Command("reg", "query", `HKCU\Software\Microsoft\Windows\CurrentVersion\Internet Settings`, "/v", "ProxyEnable").CombinedOutput()
	if err != nil {
		return systemProxyResponse{}, fmt.Errorf("read ProxyEnable: %s", strings.TrimSpace(string(output)))
	}
	enabled := strings.Contains(string(output), "0x1")
	serverOutput, _ := exec.Command("reg", "query", `HKCU\Software\Microsoft\Windows\CurrentVersion\Internet Settings`, "/v", "ProxyServer").CombinedOutput()
	fields := strings.Fields(string(serverOutput))
	server := ""
	if len(fields) >= 3 {
		server = fields[len(fields)-1]
	}
	return systemProxyResponse{Supported: true, Enabled: enabled, Server: server}, nil
}

func setSystemProxy(enabled bool) error {
	if runtime.GOOS != "windows" {
		return errors.New("system proxy control is currently supported only on Windows")
	}
	if enabled {
		if output, err := exec.Command("reg", "add", `HKCU\Software\Microsoft\Windows\CurrentVersion\Internet Settings`, "/v", "ProxyServer", "/t", "REG_SZ", "/d", "127.0.0.1:2081", "/f").CombinedOutput(); err != nil {
			return fmt.Errorf("set ProxyServer: %s", strings.TrimSpace(string(output)))
		}
		if output, err := exec.Command("reg", "add", `HKCU\Software\Microsoft\Windows\CurrentVersion\Internet Settings`, "/v", "ProxyOverride", "/t", "REG_SZ", "/d", "<local>;localhost;127.0.0.1", "/f").CombinedOutput(); err != nil {
			return fmt.Errorf("set ProxyOverride: %s", strings.TrimSpace(string(output)))
		}
	}
	value := "0"
	if enabled {
		value = "1"
	}
	if output, err := exec.Command("reg", "add", `HKCU\Software\Microsoft\Windows\CurrentVersion\Internet Settings`, "/v", "ProxyEnable", "/t", "REG_DWORD", "/d", value, "/f").CombinedOutput(); err != nil {
		return fmt.Errorf("set ProxyEnable: %s", strings.TrimSpace(string(output)))
	}
	// WinINet clients notice this registry update immediately; Chromium rechecks it on the next navigation.
	return nil
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
func (a *app) handleTestAllNodes(w http.ResponseWriter, _ *http.Request) {
	groups, err := a.readSubscriptions()
	if err != nil || len(groups) == 0 {
		writeError(w, http.StatusBadRequest, errors.New("import a subscription before testing nodes"))
		return
	}
	if !a.status().Running {
		writeError(w, http.StatusConflict, errors.New("start sing-box before testing nodes"))
		return
	}

	count := 0
	for _, group := range groups {
		for _, node := range group.Nodes {
			a.testNodeAsync(node.Tag)
			count++
		}
	}
	writeJSON(w, http.StatusAccepted, map[string]int{"testing": count})
}

func (a *app) handleTestNode(w http.ResponseWriter, r *http.Request) {
	tag := r.PathValue("tag")
	groups, err := a.readSubscriptions()
	if err != nil {
		writeError(w, http.StatusBadRequest, errors.New("import a subscription before testing nodes"))
		return
	}
	found := false
	for _, group := range groups {
		if hasNodeVless(group.Nodes, tag) {
			found = true
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
	a.testNodeAsync(tag)
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
	if request.Mode == "manual" {
		if !hasNodeVless(group.Nodes, request.Tag) {
			writeError(w, http.StatusBadRequest, errors.New("select a node from this subscription group"))
			return
		}
		target = request.Tag
	} else if request.Mode != "auto" {
		writeError(w, http.StatusBadRequest, errors.New("mode must be auto or manual"))
		return
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
					mode[activeGroup] = "manual"
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
			gs.Nodes = append(gs.Nodes, nodeStatus{
				Tag:            node.Tag,
				Name:           node.Name,
				Country:        availability.Country,
				GeminiSupport:  availability.GeminiSupport,
				ChatGPTSupport: availability.ChatGPTSupport,
				DelayMS:        a.delays[node.Tag],
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
					Country:        rule.Country,
					GeminiSupport:  rule.GeminiSupport,
					ChatGPTSupport: rule.ChatGPTSupport,
				}
			}
		}
	}
	return nodeAvailability{
		Country:        "未识别",
		GeminiSupport:  availabilityUnknown,
		ChatGPTSupport: availabilityUnknown,
	}
}

func (a *app) testNodeAsync(tag string) {
	go func() {
		result, err := a.delayTest(tag)
		a.mu.Lock()
		if err == nil {
			a.delays[tag] = result
		} else {
			a.delays[tag] = -1
		}
		a.mu.Unlock()
		if err != nil {
			a.appendLog(fmt.Sprintf("Delay test %s failed: %v", tag, err))
		}
	}()
}

func (a *app) delayTest(tag string) (int, error) {
	endpoint := fmt.Sprintf("%s/proxies/%s/delay?timeout=10000&url=%s", clashAPIURL, url.PathEscape(tag), url.QueryEscape(latencyTestURL))
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
	whitelist, _ := a.readWhitelist()
	config, err := buildSubscriptionConfig(groups, whitelist)
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
	domain := r.PathValue("domain")
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
	if flow := query.Get("flow"); flow != "" {
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

// buildSubscriptionConfig generates a sing-box configuration with one selector
// per subscription group. The top-level "proxy" selector switches between groups;
// each group selector offers its own urltest (auto) plus individual nodes.
func buildSubscriptionConfig(groups []subscriptionGroup, whitelist []string) ([]byte, error) {
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
		outbounds = append(outbounds, map[string]any{
			"type":      "urltest",
			"tag":       autoTag,
			"outbounds": nodeTags,
			"url":       latencyTestURL,
			"interval":  "5m",
			"tolerance": 50,
		})
		outbounds = append(outbounds, map[string]any{
			"type":      "selector",
			"tag":       groupTag,
			"outbounds": append([]string{autoTag}, nodeTags...),
			"default":   autoTag,
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
