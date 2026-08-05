package main

import (
	"bytes"
	"encoding/json"
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
	}
	if err := a.ensureConfig(); err != nil {
		t.Fatalf("ensureConfig: %v", err)
	}
	if err := a.ensureSubscriptions(); err != nil {
		t.Fatalf("ensureSubscriptions: %v", err)
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

func TestAPIConfigSaveAndStartWithoutBinary(t *testing.T) {
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

	start := httptest.NewRequest(http.MethodPost, "/api/start", nil)
	startResponse := httptest.NewRecorder()
	handler.ServeHTTP(startResponse, start)
	if startResponse.Code != http.StatusBadRequest || !strings.Contains(startResponse.Body.String(), "binary not found") {
		t.Fatalf("expected missing binary error, got %d: %s", startResponse.Code, startResponse.Body.String())
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
		name           string
		country        string
		geminiSupport  string
		chatGPTSupport string
	}{
		{name: "🇭🇰香港 01", country: "香港", geminiSupport: availabilitySupported, chatGPTSupport: availabilityUnsupported},
		{name: "🇺🇸美国 01", country: "美国", geminiSupport: availabilitySupported, chatGPTSupport: availabilitySupported},
		{name: "provider node", country: "未识别", geminiSupport: availabilityUnknown, chatGPTSupport: availabilityUnknown},
	}
	for _, test := range tests {
		result := classifyNodeAvailability(test.name)
		if result.Country != test.country || result.GeminiSupport != test.geminiSupport || result.ChatGPTSupport != test.chatGPTSupport {
			t.Fatalf("classify %q = %#v", test.name, result)
		}
	}
}
