package main

import (
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
)

// The view runs headless in tests; clicks behave like user input.
// Tab 0 (global) is the default.
func TestView(t *testing.T) {
	a := &app{cfg: defaultConfig()}
	tt := ui.NewTester(a.view, 880, 620)
	for _, want := range []string{"Sun Ray", "全局", "服务器", "日志", "入站（本机代理）", "监听端口", "已停止", "启动", "路由预设", "日志级别"} {
		if !tt.HasText(want) {
			t.Errorf("view missing text %q; texts: %v", want, tt.Texts())
		}
	}
}

// The server tab shows the protocol spec driven fields.
func TestViewServerTab(t *testing.T) {
	a := &app{cfg: defaultConfig(), tab: tabServer}
	a.cfg.Security = "tls" // exercise the tls branch fields
	tt := ui.NewTester(a.view, 880, 620)
	for _, want := range []string{"出站（服务器）", "协议", "地址", "端口", "UUID", "传输层", "安全", "uTLS 指纹"} {
		if !tt.HasText(want) {
			t.Errorf("server tab missing text %q; texts: %v", want, tt.Texts())
		}
	}
}

// Every protocol in the list must be registered and build a config.
func TestRegistryComplete(t *testing.T) {
	if len(protocolRegistry) != len(protocolList) {
		t.Errorf("registry has %d entries, list has %d", len(protocolRegistry), len(protocolList))
	}
	for _, name := range protocolList {
		sp, ok := protocolRegistry[name]
		if !ok {
			t.Errorf("protocol %s not registered", name)
			continue
		}
		if sp.build == nil {
			t.Errorf("protocol %s has no build func", name)
		}
	}
}

func TestBuildJSONVLESS(t *testing.T) {
	c := defaultConfig()
	c.Address = "s.example.com"
	c.Secret = "11111111-2222-3333-4444-555555555555"
	c.Network = "ws"
	c.Path = "/wspath"
	c.Host = "cdn.example.com"
	c.Security = "reality"
	c.PublicKey = "pbkey123"
	c.ShortID = "abcd"
	c.Flow = "xtls-rprx-vision"
	c.RouteCN = true
	c.RouteAds = true
	data, err := c.buildJSON()
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	for _, want := range []string{
		`"protocol": "vless"`,
		`"id": "1111`,
		`"flow": "xtls-rprx-vision"`,
		`"network": "ws"`,
		`"realitySettings"`,
		`"publicKey": "pbkey123"`,
		`"shortId": "abcd"`,
		`"port": 10808`,
		`"protocol": "mixed"`,
		`"stats"`,
		`"geosite:cn"`,
		`"geosite:category-ads-all"`,
		`"blackhole"`,
		`"sniffing"`,
		`"loglevel": "info"`,
	} {
		if !strings.Contains(s, want) {
			t.Errorf("config missing %s; got %s", want, s)
		}
	}
}

// Every registered outbound protocol must build a config without error.
func TestBuildJSONAllProtocols(t *testing.T) {
	for _, proto := range protocolList {
		c := defaultConfig()
		c.Protocol = proto
		c.Address, c.Port, c.Secret = "s.example.com", "443", "testsecret"
		c.PeerKey = "peerpubkey"
		c.Security = "tls" // hysteria/masque require TLS
		c.SNI = "s.example.com"
		data, err := c.buildJSON()
		if err != nil {
			t.Errorf("protocol %s: %v", proto, err)
			continue
		}
		if !strings.Contains(string(data), `"protocol": "`+proto+`"`) {
			t.Errorf("protocol %s: config missing its protocol name", proto)
		}
	}
}

func TestBuildJSONErrors(t *testing.T) {
	c := defaultConfig()
	if _, err := c.buildJSON(); err == nil {
		t.Error("want error for empty server address")
	}
	c.Address, c.Port, c.Secret = "s.example.com", "0", "uuid"
	if _, err := c.buildJSON(); err == nil {
		t.Error("want error for invalid server port")
	}
	c.Port = "443"
	if _, err := c.buildJSON(); err != nil {
		t.Errorf("want ok config, got %v", err)
	}
}

// Smoke test: boot and close the real in-process core with the routing
// presets on, proving the embedded geo assets load from memory.
func TestCoreStartStop(t *testing.T) {
	c := defaultConfig()
	c.Protocol = "freedom" // no server needed
	c.RouteCN, c.RouteAds = true, true
	c.ListenPort = "39441"
	data, err := c.buildJSON()
	if err != nil {
		t.Fatal(err)
	}
	var a app
	if err := a.core.Start(data, false, func(string) {}); err != nil {
		t.Fatal(err)
	}
	if !a.core.Running() {
		t.Fatal("core should be running")
	}
	if err := a.core.Stop(); err != nil {
		t.Fatal(err)
	}
	if a.core.Running() {
		t.Fatal("core should be stopped")
	}
}
