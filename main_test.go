package main

import (
	"strings"
	"testing"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// The view runs headless in tests; clicks behave like user input.
// Tab 0 (global) is the default.
func TestView(t *testing.T) {
	a := &app{cfg: defaultAppConfig(), cursor: -1}
	a.saver.remember(a.cfg) // keep the test from writing ~/sunray.json
	tt := ui.NewTester(a.view, 880, 620)
	for _, want := range []string{"Sun Ray", "全局", "服务器列表", "日志", "入站（本机代理）", "监听端口", "已停止", "启动", "路由预设", "日志级别"} {
		if !tt.HasText(want) {
			t.Errorf("view missing text %q; texts: %v", want, tt.Texts())
		}
	}
}

// The server tab is a list with its toolbar and columns.
func TestViewServerList(t *testing.T) {
	a := &app{cfg: defaultAppConfig(), tab: tabServer, cursor: -1}
	a.saver.remember(a.cfg) // keep the test from writing ~/sunray.json
	tt := ui.NewTester(a.view, 880, 620)
	for _, want := range []string{"服务器列表", "新增", "编辑", "复制", "删除", "设为当前", "名称", "协议", "地址", "端口", "共 1 个"} {
		if !tt.HasText(want) {
			t.Errorf("server list missing text %q; texts: %v", want, tt.Texts())
		}
	}
}

// The protocol form reused by the editor windows renders its fields.
func TestServerForm(t *testing.T) {
	a := &app{}
	s := defaultServerConfig()
	tt := ui.NewTester(func(c *ui.Context) { a.serverForm(c, &s) }, 640, 800)
	for _, want := range []string{"名称", "协议", "地址", "端口", "UUID"} {
		if !tt.HasText(want) {
			t.Errorf("server form missing text %q; texts: %v", want, tt.Texts())
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
	c := defaultServerConfig()
	g := defaultGlobalConfig()
	c.Address = "s.example.com"
	c.Secret = "11111111-2222-3333-4444-555555555555"
	c.Network = "ws"
	c.Path = "/wspath"
	c.Host = "cdn.example.com"
	c.Security = "reality"
	c.PublicKey = "pbkey123"
	c.ShortID = "abcd"
	c.Flow = "xtls-rprx-vision"
	g.RouteCN = true
	g.RouteAds = true
	data, err := c.buildJSON(&g)
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
	g := defaultGlobalConfig()
	for _, proto := range protocolList {
		c := defaultServerConfig()
		c.Protocol = proto
		c.Address, c.Port, c.Secret = "s.example.com", "443", "testsecret"
		c.PeerKey = "peerpubkey"
		c.Security = "tls" // hysteria/masque require TLS
		c.SNI = "s.example.com"
		data, err := c.buildJSON(&g)
		if err != nil {
			t.Errorf("protocol %s: %v", proto, err)
			continue
		}
		if !strings.Contains(string(data), `"protocol": "`+proto+`"`) {
			t.Errorf("protocol %s: config missing its protocol name", proto)
		}
	}
}

// allowInsecure must reach tlsSettings, otherwise the core cannot skip
// verification of self-signed / chain-incomplete servers (e.g. hysteria2).
func TestBuildJSONAllowInsecure(t *testing.T) {
	g := defaultGlobalConfig()
	c := defaultServerConfig()
	c.Protocol = "hysteria"
	c.Address, c.Port, c.Secret = "jpp-raw.example.com", "443", "auth"
	c.Security = "tls"
	c.SNI = "jpproxy.example.com"
	c.AllowInsecure = true
	data, err := c.buildJSON(&g)
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	if !strings.Contains(s, `"allowInsecure": true`) {
		t.Errorf("config missing allowInsecure; got %s", s)
	}
	if !strings.Contains(s, `"serverName": "jpproxy.example.com"`) {
		t.Errorf("config missing SNI; got %s", s)
	}
}

func TestBuildJSONErrors(t *testing.T) {
	g := defaultGlobalConfig()
	c := defaultServerConfig()
	if _, err := c.buildJSON(&g); err == nil {
		t.Error("want error for empty server address")
	}
	c.Address, c.Port, c.Secret = "s.example.com", "0", "uuid"
	if _, err := c.buildJSON(&g); err == nil {
		t.Error("want error for invalid server port")
	}
	c.Port = "443"
	if _, err := c.buildJSON(&g); err != nil {
		t.Errorf("want ok config, got %v", err)
	}
}

// The list supports add, activate, batch duplicate and batch delete.
func TestServerListOps(t *testing.T) {
	a := &app{cfg: defaultAppConfig(), cursor: -1}
	first := a.cfg.ActiveID

	a.addServer()
	if len(a.cfg.Servers) != 2 {
		t.Fatalf("after add: got %d servers, want 2", len(a.cfg.Servers))
	}
	added := a.cfg.Servers[1].ID
	a.setActive(added)
	if a.cfg.ActiveID != added {
		t.Fatalf("active: got %q, want %q", a.cfg.ActiveID, added)
	}

	a.chosen.Add(first)
	a.chosen.Add(added)
	a.duplicateSelected()
	if len(a.cfg.Servers) != 4 {
		t.Fatalf("after duplicate: got %d servers, want 4", len(a.cfg.Servers))
	}

	a.chosen.Clear()
	for _, s := range a.cfg.Servers {
		a.chosen.Add(s.ID)
	}
	a.deleteSelected()
	if len(a.cfg.Servers) != 1 {
		t.Fatalf("after deleting all: got %d servers, want 1 fallback", len(a.cfg.Servers))
	}
	if a.cfg.ActiveID != a.cfg.Servers[0].ID {
		t.Error("active server should fall back to the remaining one")
	}
}

// Smoke test: boot and close the real in-process core with the routing
// presets on, proving the embedded geo assets load from memory.
func TestCoreStartStop(t *testing.T) {
	g := defaultGlobalConfig()
	c := defaultServerConfig()
	c.Protocol = "freedom" // no server needed
	g.RouteCN, g.RouteAds = true, true
	g.ListenPort = "39441"
	data, err := c.buildJSON(&g)
	if err != nil {
		t.Fatal(err)
	}
	var a app
	a.trayToggle = &mygo.MenuItem{}
	if err := a.core.Start(data, func(string) {}); err != nil {
		t.Fatal(err)
	}
	if !a.core.Running() {
		t.Fatal("core should be running")
	}
	a.updateTrayMenu()
	if a.trayToggle.Label != "停止" {
		t.Errorf("tray label while running = %q, want 停止", a.trayToggle.Label)
	}
	if err := a.core.Stop(); err != nil {
		t.Fatal(err)
	}
	if a.core.Running() {
		t.Fatal("core should be stopped")
	}
	a.updateTrayMenu()
	if a.trayToggle.Label != "启动" {
		t.Errorf("tray label while stopped = %q, want 启动", a.trayToggle.Label)
	}
}
