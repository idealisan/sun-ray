package main

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// The view runs headless in tests; clicks behave like user input.
// Tab 0 (global) is the default.
func TestView(t *testing.T) {
	a := &app{cfg: defaultAppConfig(), cursor: -1}
	a.saver.remember(a.cfg) // keep the test from writing ~/sunray.json
	tt := ui.NewTester(a.view, 880, 620)
	for _, want := range []string{"Sun Ray", "全局", "服务器列表", "订阅", "日志", "入站（本机代理）", "监听端口", "已停止", "启动", "路由预设", "日志级别"} {
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

// The server list can be filtered by subscription, with an unclassified
// bucket for servers that do not belong to one.
func TestServerListFilter(t *testing.T) {
	a := &app{cfg: defaultAppConfig(), tab: tabServer, cursor: -1}
	a.cfg.Subscriptions = []subscriptionConfig{
		{ID: "sub-a", Name: "机场 A"},
		{ID: "sub-b", Name: "机场 B"},
	}
	a.cfg.Servers = []serverConfig{
		{ID: "manual", Name: "手动节点", Protocol: "freedom"},
		{ID: "a1", Name: "A 节点 1", Protocol: "freedom", SubscriptionID: "sub-a"},
		{ID: "a2", Name: "A 节点 2", Protocol: "freedom", SubscriptionID: "sub-a"},
		{ID: "b1", Name: "B 节点", Protocol: "freedom", SubscriptionID: "sub-b"},
		{ID: "orphan", Name: "孤儿节点", Protocol: "freedom", SubscriptionID: "missing"},
	}
	a.cfg.ActiveID = "a1"
	a.saver.remember(a.cfg)
	tt := ui.NewTester(a.view, 880, 620)
	if err := tt.Click("所有"); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"所有", "未分类", "机场 A", "机场 B"} {
		if !tt.HasText(want) {
			t.Errorf("filter select missing %q; texts: %v", want, tt.Texts())
		}
	}
	if err := tt.Click("机场 A"); err != nil {
		t.Fatal(err)
	}
	if a.serverFilter != "sub-a" {
		t.Fatalf("filter = %q, want sub-a", a.serverFilter)
	}
	if len(a.serverRows) != 2 || a.serverRows[0] != 1 || a.serverRows[1] != 2 {
		t.Fatalf("filtered rows = %v, want [1 2]", a.serverRows)
	}
	if tt.HasText("手动节点") || tt.HasText("B 节点") || !tt.HasText("A 节点 1") {
		t.Fatalf("visible rows after filtering: %v", tt.Texts())
	}

	if err := tt.Click("机场 A"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("未分类"); err != nil {
		t.Fatal(err)
	}
	if a.serverFilter != serverFilterUncategorized {
		t.Fatalf("filter = %q, want uncategorized", a.serverFilter)
	}
	if len(a.serverRows) != 2 || a.serverRows[0] != 0 || a.serverRows[1] != 4 {
		t.Fatalf("uncategorized rows = %v, want [0 4]", a.serverRows)
	}

	if err := tt.Click("未分类"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("所有"); err != nil {
		t.Fatal(err)
	}
	if a.serverFilter != serverFilterAll || len(a.serverRows) != 5 {
		t.Fatalf("all rows = %v, filter %q", a.serverRows, a.serverFilter)
	}
}

// The subscription tab is a list with its toolbar and columns.
func TestViewSubscriptionList(t *testing.T) {
	a := &app{cfg: defaultAppConfig(), tab: tabSubscription, subCursor: -1}
	a.cfg.Subscriptions = []subscriptionConfig{{
		ID: "sub-1", Name: "test-sub", URL: "https://example.com/sub",
	}}
	a.saver.remember(a.cfg) // keep the test from writing ~/sunray.json
	tt := ui.NewTester(a.view, 880, 620)
	for _, want := range []string{"订阅", "新增", "编辑", "复制", "删除", "更新", "名称", "订阅地址", "节点", "更新时间", "共 1 个", "test-sub", "https://example.com/sub"} {
		if !tt.HasText(want) {
			t.Errorf("subscription list missing text %q; texts: %v", want, tt.Texts())
		}
	}
}

// A row click chooses a subscription; a toolbar click keeps it for batch
// actions, while a click on the blank list clears it.
func TestSubscriptionListSelectionClears(t *testing.T) {
	a := &app{cfg: defaultAppConfig(), tab: tabSubscription, subCursor: -1}
	a.cfg.Subscriptions = []subscriptionConfig{{
		ID: "sub-1", Name: "test-sub", URL: "https://example.com/sub",
	}}
	a.saver.remember(a.cfg)
	tt := ui.NewTester(a.view, 880, 620)
	if err := tt.Click("test-sub"); err != nil {
		t.Fatal(err)
	}
	if a.subCursor != 0 || a.subChosen.Len() != 1 {
		t.Fatalf("after a row click: cursor %d, chosen %d", a.subCursor, a.subChosen.Len())
	}
	if err := tt.Click("共 1 个"); err != nil {
		t.Fatal(err)
	}
	if a.subCursor != 0 || a.subChosen.Len() != 1 {
		t.Fatalf("after a toolbar click: cursor %d, chosen %d", a.subCursor, a.subChosen.Len())
	}
	row, ok := tt.Find("test-sub")
	if !ok {
		t.Fatal("the subscription row is missing")
	}
	tt.ClickAt(440, row.Y+120)
	if a.subCursor != -1 || a.subChosen.Len() != 0 {
		t.Fatalf("after a blank list click: cursor %d, chosen %d", a.subCursor, a.subChosen.Len())
	}
}

// The subscription list supports add, edit, batch duplicate and batch
// delete; deleting a subscription also drops the servers it imported.
func TestSubscriptionListOps(t *testing.T) {
	a := &app{cfg: defaultAppConfig(), subCursor: -1}
	a.addSubscription()
	if len(a.cfg.Subscriptions) != 1 {
		t.Fatalf("after add: got %d subscriptions, want 1", len(a.cfg.Subscriptions))
	}
	added := a.cfg.Subscriptions[0].ID
	a.updateSubscription(added, subscriptionConfig{
		Name: "  机场订阅  ", URL: "  https://example.com/sub  ",
		UpdatedAt: 123, ServerCount: 4,
	})
	if got := a.cfg.Subscriptions[0]; got.Name != "机场订阅" || got.URL != "https://example.com/sub" {
		t.Fatalf("after update: %+v", got)
	}
	a.updateSubscription(added, subscriptionConfig{Name: "机场订阅", URL: "https://example.com/new"})
	if got := a.cfg.Subscriptions[0]; got.UpdatedAt != 0 || got.ServerCount != 0 {
		t.Fatalf("a changed URL should make the import stale: %+v", got)
	}

	a.subChosen.Add(added)
	a.duplicateSelectedSubscriptions()
	if len(a.cfg.Subscriptions) != 2 {
		t.Fatalf("after duplicate: got %d subscriptions, want 2", len(a.cfg.Subscriptions))
	}

	a.cfg.Servers = append(a.cfg.Servers, serverConfig{
		ID: "imported", SubscriptionID: added, Protocol: "freedom",
	})
	a.subChosen.Clear()
	a.subChosen.Add(added)
	a.serverFilter = added
	a.deleteSelectedSubscriptions()
	if len(a.cfg.Subscriptions) != 1 {
		t.Fatalf("after deleting one: got %d subscriptions, want 1", len(a.cfg.Subscriptions))
	}
	if _, ok := a.serverByID("imported"); ok {
		t.Fatal("the imported server should be removed with its subscription")
	}
	if a.serverFilter != serverFilterAll {
		t.Fatalf("deleting the filtered subscription left filter %q", a.serverFilter)
	}
}

// A row click chooses a server; a toolbar click keeps it for batch actions,
// while a click on the blank part of the list or another tab clears it.
func TestServerListSelectionClears(t *testing.T) {
	a := &app{cfg: defaultAppConfig(), tab: tabServer, cursor: -1}
	a.cfg.Servers[0].Name = "test-server"
	a.saver.remember(a.cfg) // keep the test from writing ~/sunray.json
	tt := ui.NewTester(a.view, 880, 620)

	choose := func() {
		t.Helper()
		if err := tt.Click("test-server"); err != nil {
			t.Fatal(err)
		}
		if a.cursor != 0 || a.chosen.Len() != 1 {
			t.Fatalf("after a row click: cursor %d, chosen %d", a.cursor, a.chosen.Len())
		}
	}
	cleared := func(what string) {
		t.Helper()
		if a.cursor != -1 || a.chosen.Len() != 0 {
			t.Fatalf("after %s: cursor %d, chosen %d", what, a.cursor, a.chosen.Len())
		}
	}

	choose()
	if err := tt.Click("共 1 个"); err != nil {
		t.Fatal(err)
	}
	if a.cursor != 0 || a.chosen.Len() != 1 {
		t.Fatalf("after a toolbar click: cursor %d, chosen %d", a.cursor, a.chosen.Len())
	}
	row, ok := tt.Find("test-server")
	if !ok {
		t.Fatal("the server row is missing")
	}
	tt.ClickAt(440, row.Y+120) // below the only row
	cleared("a blank list click")

	choose()
	if err := tt.Click("全局"); err != nil {
		t.Fatal(err)
	}
	if a.tab != tabGlobal {
		t.Fatalf("after clicking the global tab: tab %d", a.tab)
	}
	cleared("a tab click")
}

// Batch delete acts on the rows chosen with Shift, and a right-click delete
// removes the chosen rows after the table has finished building.
func TestServerListBatchDelete(t *testing.T) {
	a := &app{cfg: defaultAppConfig(), tab: tabServer, cursor: -1}
	a.cfg.Servers = []serverConfig{
		{ID: "s1", Name: "S1", Protocol: "freedom"},
		{ID: "s2", Name: "S2", Protocol: "freedom"},
		{ID: "s3", Name: "S3", Protocol: "freedom"},
		{ID: "keep", Name: "Keep", Protocol: "freedom"},
	}
	a.cfg.ActiveID = "keep"
	a.saver.remember(a.cfg)

	for _, rightClick := range []bool{false, true} {
		a.cfg.Servers = []serverConfig{
			{ID: "s1", Name: "S1", Protocol: "freedom"},
			{ID: "s2", Name: "S2", Protocol: "freedom"},
			{ID: "s3", Name: "S3", Protocol: "freedom"},
			{ID: "keep", Name: "Keep", Protocol: "freedom"},
		}
		a.cfg.ActiveID = "keep"
		a.chosen.Clear()
		a.cursor = -1
		a.saver.remember(a.cfg)
		tt := ui.NewTester(a.view, 880, 620)
		if err := tt.Click("S1"); err != nil {
			t.Fatal(err)
		}
		if err := tt.ClickWith(ui.Shift, "S3"); err != nil {
			t.Fatal(err)
		}
		if rightClick {
			if err := tt.RightClick("S2"); err != nil {
				t.Fatal(err)
			}
			if err := tt.ChooseMenuItem("删除"); err != nil {
				t.Fatal(err)
			}
		} else if err := tt.Click("删除"); err != nil {
			t.Fatal(err)
		}
		if len(a.cfg.Servers) != 1 || a.cfg.Servers[0].ID != "keep" {
			t.Fatalf("right click %v: after delete: %+v", rightClick, a.cfg.Servers)
		}
	}
}

// Deleting the only server must leave an empty list, not silently create a
// replacement that looks like the delete failed.
func TestServerListDeleteLast(t *testing.T) {
	a := &app{cfg: defaultAppConfig(), tab: tabServer, cursor: -1}
	a.cfg.Servers = []serverConfig{{ID: "only", Name: "Only", Protocol: "freedom"}}
	a.cfg.ActiveID = "only"
	a.saver.remember(a.cfg)
	tt := ui.NewTester(a.view, 880, 620)
	if err := tt.Click("Only"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("删除"); err != nil {
		t.Fatal(err)
	}
	if len(a.cfg.Servers) != 0 || a.cfg.ActiveID != "" {
		t.Fatalf("after deleting the last server: %+v, active %q", a.cfg.Servers, a.cfg.ActiveID)
	}
	if !tt.HasText("共 0 个") {
		t.Fatalf("list should show an empty count; texts: %v", tt.Texts())
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

// VMess alterId is part of the link and must reach the generated user.
func TestBuildJSONVMessAlterID(t *testing.T) {
	g := defaultGlobalConfig()
	c := defaultServerConfig()
	c.Protocol = "vmess"
	c.Address, c.Port, c.Secret = "vmess.example.com", "443", "uuid-vmess"
	c.VMessAlterID = "2"
	data, err := c.buildJSON(&g)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"alterId": 2`) {
		t.Errorf("config missing alterId 2; got %s", data)
	}
	c.VMessAlterID = "bad"
	if _, err := c.buildJSON(&g); err == nil {
		t.Error("want error for invalid alterId")
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

// The test button acts on the chosen servers within the filtered list, and on
// the whole filtered list when none are chosen. A context menu on a chosen
// row acts on the whole selection; on an unchosen row, on that row alone.
func TestServerTestTargets(t *testing.T) {
	a := &app{cfg: defaultAppConfig()}
	a.cfg.Servers = []serverConfig{
		{ID: "s1", Name: "S1"},
		{ID: "s2", Name: "S2"},
		{ID: "s3", Name: "S3"},
	}
	// The filter shows s1 and s2; s3 is hidden and must never be tested.
	a.serverRows = []int{0, 1}

	a.chosen.Add("s2")
	if got := a.testTargets(); len(got) != 1 || got[0] != "s2" {
		t.Fatalf("selected targets = %v, want [s2]", got)
	}
	a.chosen.Add("s1")
	if got := a.testTargets(); len(got) != 2 || got[0] != "s1" || got[1] != "s2" {
		t.Fatalf("multi targets = %v, want [s1 s2] in list order", got)
	}
	if got := a.testTargetsFor("s2"); len(got) != 2 {
		t.Fatalf("context menu on a chosen row = %v, want the selection", got)
	}
	if got := a.testTargetsFor("s3"); len(got) != 1 || got[0] != "s3" {
		t.Fatalf("context menu on an unchosen row = %v, want [s3]", got)
	}
	a.chosen.Clear()
	if got := a.testTargets(); len(got) != 2 {
		t.Fatalf("all targets = %v, want the filtered list only", got)
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
	if len(a.cfg.Servers) != 0 {
		t.Fatalf("after deleting all: got %d servers, want 0", len(a.cfg.Servers))
	}
	if a.cfg.ActiveID != "" {
		t.Errorf("active server = %q, want empty", a.cfg.ActiveID)
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

func TestAppendLogLineCapsChars(t *testing.T) {
	logs, chars := appendLogLine(nil, 0, "hello", 10)
	logs, chars = appendLogLine(logs, chars, "world", 10)
	if got := strings.Join(logs, "\n"); got != "world" {
		t.Fatalf("log = %q, want %q", got, "world")
	}
	if chars != len("world") {
		t.Fatalf("chars = %d, want %d", chars, len("world"))
	}

	logs, chars = appendLogLine(nil, 0, strings.Repeat("x", 20), 10)
	if got := strings.Join(logs, "\n"); got != strings.Repeat("x", 10) {
		t.Fatalf("single long line = %q", got)
	}
	if chars != 10 {
		t.Fatalf("chars = %d, want 10", chars)
	}

	logs, chars = appendLogLine(nil, 0, strings.Repeat("日", 20), 10)
	got := strings.Join(logs, "\n")
	if !utf8.ValidString(got) {
		t.Fatalf("trimmed log is not valid UTF-8: %q", got)
	}
	if n := utf8.RuneCountInString(got); n != 10 || chars != n {
		t.Fatalf("runes = %d, chars = %d, want 10", n, chars)
	}

	for i := 0; i < 100; i++ {
		logs, chars = appendLogLine(logs, chars, strings.Repeat("a", 7), 50)
	}
	if got := strings.Join(logs, "\n"); utf8.RuneCountInString(got) > 50 || chars != utf8.RuneCountInString(got) {
		t.Fatalf("log runes = %d, chars = %d, want <= 50", utf8.RuneCountInString(got), chars)
	}
}
