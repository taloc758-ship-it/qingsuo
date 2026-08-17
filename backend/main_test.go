package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newTestApp(t *testing.T) *app {
	t.Helper()
	dir := t.TempDir()
	a := &app{
		dataDir:       dir,
		config:        filepath.Join(dir, "config.json"),
		binary:        filepath.Join(dir, "missing-sing-box.exe"),
		subscriptions: filepath.Join(dir, "subscriptions.json"),
		whitelist:     filepath.Join(dir, "whitelist.json"),
		routingMode:   filepath.Join(dir, "routing.json"),
		tunMode:       filepath.Join(dir, "tun.json"),
		autoSwitch:    filepath.Join(dir, "auto-switch.json"),
		nodeCleanup:   filepath.Join(dir, "failed-node-cleanup.json"),
		delays:        make(map[string]serviceDelays),
	}
	if err := a.ensureConfig(); err != nil {
		t.Fatalf("ensureConfig: %v", err)
	}
	if err := a.ensureSubscriptions(); err != nil {
		t.Fatalf("ensureSubscriptions: %v", err)
	}
	if err := a.ensureWhitelist(); err != nil {
		t.Fatalf("ensureWhitelist: %v", err)
	}
	if err := a.ensureRoutingModeSettings(); err != nil {
		t.Fatalf("ensure routing mode settings: %v", err)
	}
	if err := a.ensureTunModeSettings(); err != nil {
		t.Fatalf("ensure TUN mode settings: %v", err)
	}
	if err := a.ensureAutoSwitchSettings(); err != nil {
		t.Fatalf("ensure auto-switch settings: %v", err)
	}
	return a
}

func TestEnsureConfigCreatesValidDirectTemplate(t *testing.T) {
	a := newTestApp(t)
	contents, err := os.ReadFile(a.config)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if !json.Valid(contents) || !strings.Contains(string(contents), `"type": "direct"`) {
		t.Fatalf("default configuration is not a direct JSON configuration: %s", contents)
	}
}

func TestAPIConfigSaveAndRestartWithoutBinary(t *testing.T) {
	a := newTestApp(t)
	handler := a.handler()

	get := httptest.NewRequest(http.MethodGet, "/api/config", nil)
	getResponse := httptest.NewRecorder()
	handler.ServeHTTP(getResponse, get)
	if getResponse.Code != http.StatusOK {
		t.Fatalf("get config status = %d", getResponse.Code)
	}

	updated := `{"log":{"level":"warn"},"inbounds":[],"outbounds":[{"type":"direct","tag":"direct"}],"route":{"final":"direct"}}`
	body, _ := json.Marshal(configRequest{Content: updated})
	put := httptest.NewRequest(http.MethodPut, "/api/config", bytes.NewReader(body))
	put.Header.Set("Content-Type", "application/json")
	putResponse := httptest.NewRecorder()
	handler.ServeHTTP(putResponse, put)
	if putResponse.Code != http.StatusOK {
		t.Fatalf("save config status = %d: %s", putResponse.Code, putResponse.Body.String())
	}

	restart := httptest.NewRequest(http.MethodPost, "/api/restart", nil)
	restartResponse := httptest.NewRecorder()
	handler.ServeHTTP(restartResponse, restart)
	if restartResponse.Code != http.StatusBadRequest || !strings.Contains(restartResponse.Body.String(), "binary not found") {
		t.Fatalf("expected missing binary error, got %d: %s", restartResponse.Code, restartResponse.Body.String())
	}
}

func TestIsLocalProxyServer(t *testing.T) {
	for value, want := range map[string]bool{
		"127.0.0.1:2081":     true,
		"LOCALHOST:2081":     true,
		"127.0.0.1:7890":     false,
		"proxy.example:2081": false,
	} {
		if got := isLocalProxyServer(value); got != want {
			t.Fatalf("isLocalProxyServer(%q) = %t, want %t", value, got, want)
		}
	}
}

func TestFrontendHandlerServesEmbeddedIndex(t *testing.T) {
	webDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(webDir, "index.html"), []byte("<div id=\"root\"></div>"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SINGBOX_WEB_DIR", webDir)
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	response := httptest.NewRecorder()
	frontendHandler().ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "<div id=\"root\"></div>") {
		t.Fatalf("frontend response = %d: %s", response.Code, response.Body.String())
	}
}

func TestFrontendHandlerRequiresExternalWebDirectory(t *testing.T) {
	t.Setenv("SINGBOX_WEB_DIR", "")
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	response := httptest.NewRecorder()
	frontendHandler().ServeHTTP(response, request)
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("frontend response = %d", response.Code)
	}
}

func TestConfigureListenAddressUsesValidCustomPort(t *testing.T) {
	t.Setenv("SINGBOX_WEB_LISTEN_PORT", "8788")
	previous := listenAddress
	t.Cleanup(func() { listenAddress = previous })
	listenAddress = defaultListenAddress
	configureListenAddress()
	if listenAddress != "127.0.0.1:8788" {
		t.Fatalf("listen address = %q", listenAddress)
	}
}

func TestParseVLESSRealityAndWebSocket(t *testing.T) {
	reality, err := parseVLESS("vless://11111111-1111-1111-1111-111111111111@example.com:443?encryption=none&security=reality&sni=example.org&fp=chrome&pbk=public-key&sid=abcd&flow=xtls-rprx-vision&type=tcp#Reality", 1)
	if err != nil {
		t.Fatalf("parse Reality VLESS: %v", err)
	}
	if reality.Outbound["type"] != "vless" || reality.Outbound["flow"] != "xtls-rprx-vision" {
		t.Fatalf("unexpected Reality outbound: %#v", reality.Outbound)
	}

	websocket, err := parseVLESS("vless://22222222-2222-2222-2222-222222222222@example.com:443?encryption=none&security=tls&sni=example.org&fp=chrome&type=ws&host=cdn.example.org&path=%2Fws#WebSocket", 2)
	if err != nil {
		t.Fatalf("parse WebSocket VLESS: %v", err)
	}
	transport := websocket.Outbound["transport"].(map[string]any)
	if transport["type"] != "ws" || transport["path"] != "/ws" {
		t.Fatalf("unexpected WebSocket transport: %#v", transport)
	}
}

func TestParseVLESSDropsUnsupportedFlow(t *testing.T) {
	node, err := parseVLESS("vless://11111111-1111-1111-1111-111111111111@example.com:443?encryption=none&security=reality&sni=example.org&pbk=public-key&flow=xtls-rprx-vision-udp44#UnsupportedFlow", 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := node.Outbound["flow"]; exists {
		t.Fatalf("unsupported flow was kept: %#v", node.Outbound)
	}
}

func TestParseSingboxConfigDropsUnsupportedFlow(t *testing.T) {
	content := `{"outbounds":[{"type":"vless","tag":"bad-flow","server":"example.com","server_port":443,"uuid":"11111111-1111-1111-1111-111111111111","flow":"xtls-rprx-vision-udp44"}]}`
	nodes, err := parseSingboxConfigNodes(content)
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := nodes[0].Outbound["flow"]; exists {
		t.Fatalf("unsupported JSON flow was kept: %#v", nodes[0].Outbound)
	}
}

func TestBuildSubscriptionConfigHasUrltestMembers(t *testing.T) {
	config, err := buildSubscriptionConfig([]subscriptionGroup{
		{ID: "g1", Nodes: []vlessNode{
			{Tag: "g1-01", Name: "Test node", Outbound: map[string]any{"type": "vless", "tag": "g1-01"}},
		}},
	}, nil)
	if err != nil {
		t.Fatalf("build config: %v", err)
	}
	if !strings.Contains(string(config), `"grp-g1"`) || !strings.Contains(string(config), `"auto-g1"`) || !strings.Contains(string(config), `"g1-01"`) || !strings.Contains(string(config), `"clash_api"`) || !strings.Contains(string(config), `"http-in"`) {
		t.Fatalf("generated config omitted urltest members: %s", config)
	}
}

func TestBuildSubscriptionConfigHasWhitelistRouting(t *testing.T) {
	config, err := buildSubscriptionConfig([]subscriptionGroup{
		{ID: "g1", Nodes: []vlessNode{
			{Tag: "g1-01", Name: "Test node", Outbound: map[string]any{"type": "vless", "tag": "g1-01"}},
		}},
	}, nil)
	if err != nil {
		t.Fatalf("build config: %v", err)
	}
	body := string(config)
	for _, expected := range []string{`"rule_set"`, `"geosite-cn"`, `"geoip-cn"`, `"ip_is_private"`, `"srss/geosite-cn.srs"`, `"outbound": "direct"`, `"final": "proxy"`} {
		if !strings.Contains(body, expected) {
			t.Fatalf("generated config missing whitelist routing token %q: %s", expected, config)
		}
	}
}

func TestBuildSubscriptionConfigCustomWhitelist(t *testing.T) {
	config, err := buildSubscriptionConfig([]subscriptionGroup{
		{ID: "g1", Nodes: []vlessNode{
			{Tag: "g1-01", Name: "Test node", Outbound: map[string]any{"type": "vless", "tag": "g1-01"}},
		}},
	}, []string{"example.com", "foo.org"})
	if err != nil {
		t.Fatalf("build config: %v", err)
	}
	body := string(config)
	for _, expected := range []string{`"domain_suffix"`, `"example.com"`, `"foo.org"`} {
		if !strings.Contains(body, expected) {
			t.Fatalf("custom whitelist missing %q: %s", expected, config)
		}
	}
}

func TestBuildSubscriptionConfigGlobalProxyHasNoDirectRoutes(t *testing.T) {
	config, err := buildSubscriptionConfigWithSettings([]subscriptionGroup{
		{ID: "g1", Nodes: []vlessNode{
			{Tag: "g1-01", Name: "Test node", Outbound: map[string]any{"type": "vless", "tag": "g1-01"}},
		}},
	}, []string{"example.com"}, autoSwitchSettings{AutoSelection: true}, true, false)
	if err != nil {
		t.Fatalf("build global config: %v", err)
	}
	body := string(config)
	for _, excluded := range []string{`"type": "direct"`, `"rule_set"`, `"domain_suffix"`, `"geosite-cn"`, `"geoip-cn"`, `"ip_is_private"`} {
		if strings.Contains(body, excluded) {
			t.Fatalf("global config must not contain direct routing token %q: %s", excluded, config)
		}
	}
	if !strings.Contains(body, `"final": "proxy"`) {
		t.Fatalf("global config must still route to proxy: %s", config)
	}
}
func TestBuildSubscriptionConfigMultipleGroups(t *testing.T) {
	config, err := buildSubscriptionConfig([]subscriptionGroup{
		{ID: "g1", Nodes: []vlessNode{{Tag: "g1-01", Name: "a", Outbound: map[string]any{"type": "vless", "tag": "g1-01"}}}},
		{ID: "g2", Nodes: []vlessNode{{Tag: "g2-01", Name: "b", Outbound: map[string]any{"type": "vless", "tag": "g2-01"}}}},
	}, nil)
	if err != nil {
		t.Fatalf("build config: %v", err)
	}
	body := string(config)
	for _, expected := range []string{`"grp-g1"`, `"grp-g2"`, `"g1-01"`, `"g2-01"`, `"final": "proxy"`} {
		if !strings.Contains(body, expected) {
			t.Fatalf("multi-group config missing %q: %s", expected, config)
		}
	}
}

func TestNextGroupID(t *testing.T) {
	if id := nextGroupID(nil); id != "g1" {
		t.Fatalf("empty -> g1, got %s", id)
	}
	if id := nextGroupID([]subscriptionGroup{{ID: "g1"}, {ID: "g2"}}); id != "g3" {
		t.Fatalf("g1,g2 -> g3, got %s", id)
	}
}

func TestRelabelNodes(t *testing.T) {
	nodes := []vlessNode{
		{Tag: "node-01", Outbound: map[string]any{"type": "vless", "tag": "node-01"}},
		{Tag: "node-02", Outbound: map[string]any{"type": "vless", "tag": "node-02"}},
	}
	relabelNodes(nodes, "g3")
	if nodes[0].Tag != "g3-01" || nodes[1].Tag != "g3-02" {
		t.Fatalf("tags not relabeled: %+v", nodes)
	}
	if nodes[0].Outbound["tag"] != "g3-01" {
		t.Fatalf("outbound tag not relabeled: %v", nodes[0].Outbound)
	}
}

func TestParseSingboxConfigNodes(t *testing.T) {
	content := `{"outbounds":[{"type":"selector","tag":"group","outbounds":["remote-name"]},{"type":"vless","tag":"remote-name","server":"example.com","server_port":443,"uuid":"11111111-1111-1111-1111-111111111111","tls":{"enabled":true}}]}`
	nodes, err := parseSubscriptionNodes(content)
	if err != nil {
		t.Fatalf("parse sing-box subscription: %v", err)
	}
	if len(nodes) != 1 || nodes[0].Tag != "node-01" || nodes[0].Name != "remote-name" || nodes[0].Outbound["tag"] != "node-01" {
		t.Fatalf("unexpected parsed nodes: %#v", nodes)
	}
}

func TestClassifyNodeAvailability(t *testing.T) {
	tests := []struct {
		name    string
		country string
	}{
		{name: "🇭🇰香港 01", country: "香港"},
		{name: "🇺🇸美国 01", country: "美国"},
		{name: "provider node", country: "未识别"},
	}
	for _, test := range tests {
		result := classifyNodeAvailability(test.name)
		if result.Country != test.country {
			t.Fatalf("classify %q = %#v", test.name, result)
		}
	}
}

func TestParseDelayTestService(t *testing.T) {
	for raw, want := range map[string]delayTestService{
		"":        delayTestAll,
		"google":  delayTestGoogle,
		"gemini":  delayTestGemini,
		"chatgpt": delayTestChatGPT,
	} {
		request := httptest.NewRequest(http.MethodPost, "/api/nodes/test?service="+raw, nil)
		got, err := parseDelayTestService(request)
		if err != nil || got != want {
			t.Fatalf("service %q = %q, %v; want %q", raw, got, err, want)
		}
	}
	request := httptest.NewRequest(http.MethodPost, "/api/nodes/test?service=unsupported", nil)
	if _, err := parseDelayTestService(request); err == nil {
		t.Fatal("unsupported service was accepted")
	}
}

func TestServiceDelayMergePreservesOtherResults(t *testing.T) {
	current := serviceDelays{Google: 100, Gemini: 200, ChatGPT: 300}
	updated := current.merge(serviceDelays{Gemini: 250}, delayTestGemini)
	want := serviceDelays{Google: 100, Gemini: 250, ChatGPT: 300}
	if updated != want {
		t.Fatalf("merged result = %#v, want %#v", updated, want)
	}
	all := current.merge(serviceDelays{Google: 400, Gemini: 500, ChatGPT: 600}, delayTestAll)
	if all != (serviceDelays{Google: 400, Gemini: 500, ChatGPT: 600}) {
		t.Fatalf("all-services result = %#v", all)
	}
}

func TestNodeSelectionKeepsAutomaticMode(t *testing.T) {
	a := newTestApp(t)
	groups := []subscriptionGroup{{
		ID: "g1",
		Nodes: []vlessNode{
			{Tag: "g1-01", Outbound: map[string]any{"type": "vless", "tag": "g1-01"}},
			{Tag: "g1-02", Outbound: map[string]any{"type": "vless", "tag": "g1-02"}},
		},
	}}
	if err := a.writeSubscriptions(groups); err != nil {
		t.Fatal(err)
	}
	settings := autoSwitchSettings{AutoSelection: true, FailoverOnly: true, Pinned: map[string]string{"g1": "g1-01"}}
	if err := a.writeAutoSwitchSettings(settings); err != nil {
		t.Fatal(err)
	}

	// The core is absent in this unit test, so validate the persistence rule
	// that handleSelection applies after a successful live selector change.
	settings.Pinned["g1"] = "g1-02"
	if err := a.writeAutoSwitchSettings(settings); err != nil {
		t.Fatal(err)
	}
	got, err := a.readAutoSwitchSettings()
	if err != nil || !got.AutoSelection || got.Pinned["g1"] != "g1-02" {
		t.Fatalf("node selection did not stay automatic: %#v, %v", got, err)
	}
}

func TestRouteRulesIncludeBuiltInsAndCustomWhitelist(t *testing.T) {
	a := newTestApp(t)
	rules := a.routeRules([]string{"example.com"}, false)
	if len(rules) != 6 {
		t.Fatalf("expected 6 route rules, got %#v", rules)
	}
	custom := rules[4]
	if custom.Value != "example.com" || !custom.Editable || custom.Outbound != "直连" {
		t.Fatalf("unexpected custom route rule: %#v", custom)
	}
	if rules[len(rules)-1].Outbound != "代理" {
		t.Fatalf("final route rule should proxy: %#v", rules[len(rules)-1])
	}
}

func TestGlobalRouteRulesSuspendDirectRules(t *testing.T) {
	a := newTestApp(t)
	rules := a.routeRules([]string{"example.com"}, true)
	if len(rules) != 2 || rules[0].ID != "global" || rules[0].Outbound != "代理" {
		t.Fatalf("unexpected global rules: %#v", rules)
	}
	if rules[1].Value != "example.com" || rules[1].Outbound != "已停用" || !rules[1].Editable {
		t.Fatalf("custom whitelist should be retained but suspended: %#v", rules[1])
	}
}

func TestRoutingModeUpdatePersistsWithoutSubscriptions(t *testing.T) {
	a := newTestApp(t)
	request := httptest.NewRequest(http.MethodPost, "/api/routing-mode", strings.NewReader(`{"globalProxy":true}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	a.handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("routing mode update status = %d: %s", response.Code, response.Body.String())
	}
	settings, err := a.readRoutingModeSettings()
	if err != nil || !settings.GlobalProxy {
		t.Fatalf("routing mode settings = %#v, %v", settings, err)
	}
}

func TestBuildSubscriptionConfigAddsTunInbound(t *testing.T) {
	config, err := buildSubscriptionConfigWithSettings([]subscriptionGroup{
		{ID: "g1", Nodes: []vlessNode{{Tag: "g1-01", Outbound: map[string]any{"type": "vless", "tag": "g1-01"}}}},
	}, nil, autoSwitchSettings{AutoSelection: true}, false, true)
	if err != nil {
		t.Fatalf("build TUN config: %v", err)
	}
	body := string(config)
	for _, expected := range []string{`"type": "tun"`, `"interface_name": "QingSuo TUN"`, `"auto_route": true`, `"strict_route": true`, `"stack": "mixed"`} {
		if !strings.Contains(body, expected) {
			t.Fatalf("TUN config missing %q: %s", expected, config)
		}
	}
}

func TestTunConfigurationValidatesWithBundledSingBox(t *testing.T) {
	binary := filepath.Join("data", "bin", "sing-box.exe")
	if _, err := os.Stat(binary); errors.Is(err, os.ErrNotExist) {
		t.Skip("bundled sing-box binary is not available")
	}
	absBinary, err := filepath.Abs(binary)
	if err != nil {
		t.Fatal(err)
	}
	a := newTestApp(t)
	a.binary = absBinary
	config, err := buildSubscriptionConfigWithSettings([]subscriptionGroup{
		{ID: "g1", Nodes: []vlessNode{{Tag: "g1-01", Outbound: map[string]any{"type": "vless", "tag": "g1-01", "server": "example.com", "server_port": 443, "uuid": "11111111-1111-1111-1111-111111111111"}}}},
	}, nil, autoSwitchSettings{AutoSelection: true}, true, true)
	if err != nil {
		t.Fatalf("build global TUN config: %v", err)
	}
	if err := a.validateConfig(config); err != nil {
		t.Fatalf("bundled sing-box rejected TUN config: %v", err)
	}
}

func TestTunModeUpdateRequiresSupportAndElevation(t *testing.T) {
	a := newTestApp(t)
	request := httptest.NewRequest(http.MethodPost, "/api/tun-mode", strings.NewReader(`{"enabled":true}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	a.handler().ServeHTTP(response, request)
	if !tunModeSupported() && response.Code != http.StatusNotImplemented {
		t.Fatalf("unsupported TUN request status = %d: %s", response.Code, response.Body.String())
	}
	if tunModeSupported() && !processElevated() && response.Code != http.StatusForbidden {
		t.Fatalf("non-elevated TUN request status = %d: %s", response.Code, response.Body.String())
	}
}

func TestUpdateWhitelistIsAtomic(t *testing.T) {
	a := newTestApp(t)
	if err := a.writeWhitelist([]string{"old.example"}); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPut, "/api/whitelist/old.example", strings.NewReader(`{"domain":"new.example"}`))
	response := httptest.NewRecorder()
	a.handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("update status = %d: %s", response.Code, response.Body.String())
	}
	domains, err := a.readWhitelist()
	if err != nil || len(domains) != 1 || domains[0] != "new.example" {
		t.Fatalf("updated whitelist = %#v, %v", domains, err)
	}
}

func TestNormalizeDomainSupportsSuffixWildcards(t *testing.T) {
	for input, want := range map[string]string{
		"*.vip":              ".vip",
		"https://*.vip/path": ".vip",
		".example.com":       ".example.com",
		"foo*.example.com":   "",
	} {
		if got := normalizeDomain(input); got != want {
			t.Fatalf("normalizeDomain(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestGroupDelayTestRouteRejectsUnknownGroup(t *testing.T) {
	a := newTestApp(t)
	request := httptest.NewRequest(http.MethodPost, "/api/groups/missing/nodes/test", nil)
	response := httptest.NewRecorder()
	a.handler().ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("missing subscription response = %d: %s", response.Code, response.Body.String())
	}
}

func TestFailedNodeCleanupSettingsRoundTrip(t *testing.T) {
	a := newTestApp(t)
	want := failedNodeCleanupSettings{RemoveFailed: true}
	if err := a.writeFailedNodeCleanupSettings(want); err != nil {
		t.Fatal(err)
	}
	got, err := a.readFailedNodeCleanupSettings()
	if err != nil || got != want {
		t.Fatalf("cleanup settings = %#v, %v", got, err)
	}
}

func TestPrepareSavedSelectionConfigUsesSavedActiveGroup(t *testing.T) {
	a := newTestApp(t)
	groups := []subscriptionGroup{
		{ID: "g1", Nodes: []vlessNode{{Tag: "g1-01", Outbound: map[string]any{"type": "vless", "tag": "g1-01"}}}},
		{ID: "g2", Nodes: []vlessNode{{Tag: "g2-01", Outbound: map[string]any{"type": "vless", "tag": "g2-01"}}}},
	}
	if err := a.writeSubscriptions(groups); err != nil {
		t.Fatal(err)
	}
	if err := a.writeAutoSwitchSettings(autoSwitchSettings{
		AutoSelection: true,
		ActiveGroup:   "g2",
		Pinned:        map[string]string{"g2": "g2-01"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := a.prepareSavedSelectionConfig(); err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(a.config)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(contents), `"default": "grp-g1"`) {
		t.Fatalf("config did not preserve stable selector structure: %s", contents)
	}
	settings, err := a.readAutoSwitchSettings()
	if err != nil || !settings.AutoSelection || settings.ActiveGroup != "g2" || settings.Pinned["g2"] != "g2-01" {
		t.Fatalf("saved selection changed unexpectedly: %#v, %v", settings, err)
	}
}

func TestRemoveFailedNodesKeepsOneNode(t *testing.T) {
	a := newTestApp(t)
	groups := []subscriptionGroup{{
		ID:    "g1",
		Nodes: []vlessNode{{Tag: "g1-01", Outbound: map[string]any{"type": "vless", "tag": "g1-01"}}},
	}}
	if err := a.writeSubscriptions(groups); err != nil {
		t.Fatal(err)
	}
	a.removeFailedNodes("g1", map[string]bool{"g1-01": true})
	got, err := a.readSubscriptions()
	if err != nil || len(got) != 1 || len(got[0].Nodes) != 1 {
		t.Fatalf("all-failed group should stay intact: %#v, %v", got, err)
	}
}

func TestBuildSubscriptionConfigFailoverOnlyUsesHighTolerance(t *testing.T) {
	config, err := buildSubscriptionConfigWithSettings([]subscriptionGroup{
		{ID: "g1", Nodes: []vlessNode{{Tag: "g1-01", Outbound: map[string]any{"type": "vless", "tag": "g1-01"}}}},
	}, nil, autoSwitchSettings{FailoverOnly: true, Pinned: map[string]string{"g1": "g1-01"}}, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(config), `"tolerance": 65535`) {
		t.Fatalf("failover-only tolerance missing from config: %s", config)
	}
	if !strings.Contains(string(config), `"default": "g1-01"`) {
		t.Fatalf("failover-only selector did not pin node: %s", config)
	}
}

func TestNormalizeAutoSwitchSettingsDefaultsInterval(t *testing.T) {
	settings := autoSwitchSettings{}
	normalizeAutoSwitchSettings(&settings)
	if settings.SwitchInterval != "5m" {
		t.Fatalf("expected default interval 5m, got %q", settings.SwitchInterval)
	}
	settings.SwitchInterval = "invalid"
	normalizeAutoSwitchSettings(&settings)
	if settings.SwitchInterval != "5m" {
		t.Fatalf("expected invalid interval to normalize to 5m, got %q", settings.SwitchInterval)
	}
}

func TestBuildSubscriptionConfigUsesSwitchInterval(t *testing.T) {
	config, err := buildSubscriptionConfigWithSettings([]subscriptionGroup{{ID: "g1", Nodes: []vlessNode{{Tag: "g1-01", Outbound: map[string]any{"type": "vless", "tag": "g1-01"}}}}}, nil, autoSwitchSettings{AutoSelection: true, SwitchInterval: "30s"}, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(config), `"interval": "30s"`) {
		t.Fatalf("switch interval missing from config: %s", config)
	}
}

func TestAutoSwitchIntervalAPIUpdatesAndRejectsInvalidValues(t *testing.T) {
	a := newTestApp(t)
	handler := a.handler()

	update := httptest.NewRequest(http.MethodPost, "/api/auto-switch", strings.NewReader(`{"switchInterval":"3m"}`))
	update.Header.Set("Content-Type", "application/json")
	updateResponse := httptest.NewRecorder()
	handler.ServeHTTP(updateResponse, update)
	if updateResponse.Code != http.StatusOK {
		t.Fatalf("update interval status = %d, body = %s", updateResponse.Code, updateResponse.Body.String())
	}
	settings, err := a.readAutoSwitchSettings()
	if err != nil || settings.SwitchInterval != "3m" || !settings.AutoSelection {
		t.Fatalf("interval update was not persisted: %#v, %v", settings, err)
	}

	invalid := httptest.NewRequest(http.MethodPost, "/api/auto-switch", strings.NewReader(`{"switchInterval":"2h"}`))
	invalid.Header.Set("Content-Type", "application/json")
	invalidResponse := httptest.NewRecorder()
	handler.ServeHTTP(invalidResponse, invalid)
	if invalidResponse.Code != http.StatusBadRequest {
		t.Fatalf("invalid interval status = %d, body = %s", invalidResponse.Code, invalidResponse.Body.String())
	}
}
