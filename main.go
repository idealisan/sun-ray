package main

// Sun Ray: a simple Xray GUI client in native UI. The xray-core library
// runs inside this very process (see core.go + protocols.go); the window
// below is drawn by mygo itself, with no webview. The UI is tabbed:
// global settings, the server list and the run log live on separate tabs.
// Each server in the list opens its own editor window, reusing serverForm.

import (
	"fmt"
	"log"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

const maxLogLines = 300

// Tab indexes in the main window.
const (
	tabGlobal = iota
	tabServer
	tabLog
)

type app struct {
	cfg        appConfig
	core       xrayCore
	win        *mygo.Window
	tray       *mygo.Tray
	trayToggle *mygo.MenuItem        // start/stop entry in the tray menu
	quitting   bool                  // a real quit is under way, so closing may proceed
	tab        int                   // selected tab in the main window
	saver      configSaver           // debounced persistence to ~/sunray.json
	tests      map[string]*testState // per server measurement results
	testing    bool                  // a measurement run is in progress

	logs []string // xray log lines, newest last, capped

	upSpeed, downSpeed string // per-second rates shown in the header

	// Server list state.
	list     ui.ListState
	chosen   ui.Selection[string] // selected server ids, for batch actions
	cursor   int                  // row the keyboard acts on, -1 for none
	sort     ui.SortOrder
	lastSort ui.SortOrder
}

func (a *app) addLog(line string) {
	a.win.Update(func() {
		a.logs = append(a.logs, line)
		if len(a.logs) > maxLogLines {
			a.logs = a.logs[len(a.logs)-maxLogLines:]
		}
	})
}

// start validates the active server, builds the config and boots the
// in-process core, all through function calls.
func (a *app) start() {
	defer a.updateTrayMenu()
	a.saver.maybeSave(a.cfg, true)
	s := a.activeServer()
	if s == nil {
		a.addLog("[sun-ray] 没有可用的服务器")
		return
	}
	cfgJSON, err := s.buildJSON(&a.cfg.Global)
	if err != nil {
		a.addLog("[sun-ray] " + err.Error())
		return
	}
	if err := a.core.Start(cfgJSON, a.addLog); err != nil {
		a.addLog("[sun-ray] 启动失败: " + err.Error())
		return
	}
	a.addLog("[sun-ray] 内核已在本进程内启动")
}

func (a *app) stop() {
	defer a.updateTrayMenu()
	a.saver.maybeSave(a.cfg, true)
	if err := a.core.Stop(); err != nil {
		a.addLog("[sun-ray] 停止出错: " + err.Error())
		return
	}
	a.addLog("[sun-ray] 内核已停止")
}

// pollTraffic samples the stats counters once a second from a goroutine,
// publishing speeds back through Window.Update.
func (a *app) pollTraffic() {
	var prevUp, prevDown int64
	for range time.Tick(time.Second) {
		up, down := a.core.Traffic()
		upRate, downRate := up-prevUp, down-prevDown
		prevUp, prevDown = up, down
		a.win.Update(func() {
			a.upSpeed = fmtBytes(upRate) + "/s"
			a.downSpeed = fmtBytes(downRate) + "/s"
		})
	}
}

func fmtBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	v := float64(n)
	for _, s := range []string{"KB", "MB", "GB", "TB"} {
		v /= unit
		if v < unit {
			return fmt.Sprintf("%.1f %s", v, s)
		}
	}
	return fmt.Sprintf("%.1f PB", v/unit)
}
func (a *app) view(c *ui.Context) {
	t := c.Theme()
	running := a.core.Running()
	a.saver.maybeSave(a.cfg, false)

	ui.Column(c).Fill().Padding(12, 16).Gap(10).Children(func() {
		// Header: title, active server, live speeds, status, start/stop.
		ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
			ui.Text(c, "Sun Ray").FontSize(18).Bold()
			if s := a.activeServer(); s != nil {
				ui.Text(c, "当前："+s.displayName()).TextColor(t.TextMuted).Grow(1)
			} else {
				ui.Text(c, "未选择服务器").TextColor(t.TextMuted).Grow(1)
			}
			if running {
				ui.Textf(c, "↑ %s  ↓ %s", a.upSpeed, a.downSpeed).TextColor(t.TextMuted)
				ui.Text(c, "运行中").Bold().TextColor(t.Accent)
				if ui.Button(c.Key("stop"), "停止").Clicked() {
					a.stop()
				}
			} else {
				ui.Text(c, "已停止").TextColor(t.TextMuted)
				if ui.PrimaryButton(c.Key("start"), "启动").Clicked() {
					a.start()
				}
			}
		})

		ui.Tabs(c, &a.tab, "全局", "服务器列表", "日志")
		switch a.tab {
		case tabGlobal:
			a.globalTab(c)
		case tabServer:
			a.serverListTab(c)
		case tabLog:
			a.logTab(c)
		}
	})
}

// globalTab: inbound ports, sniffing, log level and routing presets.
func (a *app) globalTab(c *ui.Context) {
	g := &a.cfg.Global
	ui.Row(c).Fill().Gap(24).AlignItems(ui.Stretch).Children(func() {
		ui.Column(c).Grow(1).MinWidth(0).Gap(8).Children(func() {
			ui.Text(c, "入站（本机代理）").Bold()
			ui.Field(c, "监听端口（SOCKS 与 HTTP 共用）", func() {
				ui.TextInput(c, &g.ListenPort).Placeholder("10808")
			})
			ui.Checkbox(c, &g.AllowLAN, "允许局域网连接")
			ui.Checkbox(c, &g.Sniffing, "流量嗅探（域名分流需要）")
		})
		ui.Column(c).Grow(1).MinWidth(0).Gap(8).Children(func() {
			ui.Text(c, "日志").Bold()
			renderChoice(c, "日志级别", &g.LogLevel, logLevels)

			ui.Text(c, "路由预设").Bold()
			ui.Checkbox(c, &g.RouteCN, "绕过大陆（geosite:cn / geoip:cn 直连）")
			ui.Checkbox(c, &g.RouteAds, "屏蔽广告（geosite:category-ads-all）")
		})
	})
}

// serverListTab: the list of servers. A double click edits a server in its
// own window; Enter makes the chosen server active.
func (a *app) serverListTab(c *ui.Context) {
	t := c.Theme()
	a.applySort()
	a.list.Key = func(row int) any { return a.cfg.Servers[row].ID }
	a.list.Selection = &a.chosen
	a.list.Selected = &a.cursor
	a.list.Sort = &a.sort

	ui.Column(c).Fill().Padding(0, 4, 8).Gap(8).Children(func() {
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			if ui.Button(c.Key("srvadd"), "新增").Clicked() {
				a.addServer()
			}
			if ui.Button(c.Key("srvedit"), "编辑").Clicked() {
				if id, ok := a.cursorServerID(); ok {
					a.openEditor(id)
				}
			}
			if ui.Button(c.Key("srvdup"), "复制").Clicked() {
				a.duplicateSelected()
			}
			if ui.Button(c.Key("srvdelsel"), "删除").Clicked() {
				a.deleteSelected()
			}
			if ui.Button(c.Key("srvuse"), "设为当前").Clicked() {
				if id, ok := a.cursorServerID(); ok {
					a.setActive(id)
				}
			}
			if ui.Button(c.Key("srvtest"), "测试").Disabled(a.testing).Clicked() {
				a.runTests()
			}
			ui.Text(c, fmt.Sprintf("共 %d 个", len(a.cfg.Servers))).TextColor(t.TextMuted).Grow(1)
		})

		cols := []ui.TableColumn{
			{Title: "名称", ID: "name", Sortable: true},
			{Title: "协议", ID: "protocol", Width: 100, Sortable: true},
			{Title: "地址", ID: "address", Sortable: true},
			{Title: "端口", ID: "port", Width: 80, Align: ui.End, Sortable: true},
			{Title: "当前", ID: "active", Width: 60, Align: ui.Center},
			{Title: "延迟", ID: "latency", Width: 90, Align: ui.End},
			{Title: "速度", ID: "speed", Width: 110, Align: ui.End},
		}
		dbl := -1
		tbl := ui.Table(c, &a.list, cols, len(a.cfg.Servers), func(row, col int) {
			s := &a.cfg.Servers[row]
			cell := ui.Box(c).FillWidth().Children(func() {
				switch col {
				case 0:
					ui.Text(c, s.displayName()).SingleLine()
				case 1:
					ui.Text(c, s.Protocol)
				case 2:
					ui.Text(c, s.Address).SingleLine()
				case 3:
					ui.Text(c, s.Port)
				case 4:
					if s.ID == a.cfg.ActiveID {
						ui.Text(c, "●").TextColor(t.Accent)
					}
				case 5:
					st := a.testStateOf(s.ID)
					switch {
					case st == nil:
						ui.Text(c, "-").TextColor(t.TextMuted)
					case st.running:
						ui.Text(c, "测试中…").TextColor(t.TextMuted)
					case st.failed:
						ui.Text(c, "失败").TextColor(t.TextMuted)
					default:
						ui.Text(c, formatLatency(st.latency))
					}
				case 6:
					st := a.testStateOf(s.ID)
					if st == nil || st.failed || st.speed <= 0 {
						ui.Text(c, "-").TextColor(t.TextMuted)
					} else {
						ui.Text(c, "↓ "+formatSpeed(st.speed))
					}
				}
			})
			if cell.RowDoubleClicked() {
				dbl = row
			}
		}).Grow(1)

		switch {
		case dbl >= 0:
			a.cursor = dbl
			a.openEditor(a.cfg.Servers[dbl].ID)
		case tbl.Submitted():
			if id, ok := a.cursorServerID(); ok {
				a.setActive(id)
			}
		}
	})
}

// serverForm: the protocol form, shared by the editor windows.
func (a *app) serverForm(c *ui.Context, cfg *serverConfig) {
	ui.Column(c).Gap(8).Children(func() {
		ui.Field(c, "名称（可选）", func() {
			ui.TextInput(c, &cfg.Name).Placeholder("例如 日本节点")
		})
		ui.Field(c, "协议", func() {
			ui.Select(c, &cfg.Protocol, protocolList)
		})
		sp := spec(cfg.Protocol)
		if sp.needsAddr {
			ui.Row(c).Gap(16).Children(func() {
				ui.Field(c, "地址", func() {
					ui.TextInput(c, &cfg.Address).Placeholder("example.com").Grow(1)
				})
				ui.Field(c, "端口", func() {
					ui.TextInput(c, &cfg.Port).Placeholder("443").Width(120)
				})
			})
		}
		if sp.needsSecret {
			ui.Field(c, sp.secretLabel, func() {
				ui.TextInput(c, &cfg.Secret)
			})
		}
		for _, f := range sp.fields {
			renderField(c, f, cfg)
		}

		// Transport and security apply to the TCP-family protocols.
		if sp.stream {
			ui.Text(c, "传输层").Bold()
			renderField(c, networkField, cfg)
			switch cfg.Network {
			case "ws":
				ui.Field(c, "WS Path", func() { ui.TextInput(c, &cfg.Path) })
				ui.Field(c, "WS Host", func() { ui.TextInput(c, &cfg.Host) })
			case "grpc":
				ui.Field(c, "serviceName", func() { ui.TextInput(c, &cfg.Service) })
				ui.Field(c, "authority", func() { ui.TextInput(c, &cfg.Host) })
			case "httpupgrade":
				ui.Field(c, "Path", func() { ui.TextInput(c, &cfg.Path) })
				ui.Field(c, "Host", func() { ui.TextInput(c, &cfg.Host) })
			}
		}

		// Security: full options for stream protocols, TLS-only for QUIC ones.
		if sp.stream || sp.tlsOnly {
			ui.Text(c, "安全").Bold()
			if sp.tlsOnly {
				renderField(c, securityQuicField, cfg)
			} else {
				renderField(c, securityField, cfg)
			}
			switch cfg.Security {
			case "tls":
				ui.Field(c, "SNI（留空用地址）", func() { ui.TextInput(c, &cfg.SNI) })
				ui.Field(c, "ALPN（逗号分隔，可选）", func() { ui.TextInput(c, &cfg.ALPN) })
				if sp.stream {
					renderField(c, fingerprintField, cfg)
				}
				ui.Checkbox(c, &cfg.AllowInsecure, "允许不安全证书")
			case "reality":
				ui.Field(c, "SNI / 伪装域名", func() { ui.TextInput(c, &cfg.SNI) })
				ui.Field(c, "Public Key", func() { ui.TextInput(c, &cfg.PublicKey) })
				ui.Field(c, "Short ID", func() { ui.TextInput(c, &cfg.ShortID) })
				ui.Field(c, "SpiderX", func() { ui.TextInput(c, &cfg.SpiderX) })
				renderField(c, fingerprintField, cfg)
			}
		}
	})
}

// editorView is the content of one server's editor window: the protocol
// form plus Save and Cancel, nothing else.
func (a *app) editorView(c *ui.Context, win *mygo.Window, s *serverConfig, id string) {
	ui.Column(c).Fill().Padding(12, 16).Gap(10).Children(func() {
		ui.Text(c, "服务器设置").FontSize(16).Bold()
		ui.Scroll(c).Grow(1).Children(func() {
			a.serverForm(c, s)
		})
		ui.Row(c).Gap(8).Children(func() {
			if ui.PrimaryButton(c.Key("editorsave"), "保存").Clicked() {
				a.updateServer(id, *s)
				a.saver.maybeSave(a.cfg, true)
				win.Close()
			}
			if ui.Button(c.Key("editorcancel"), "取消").Clicked() {
				win.Close()
			}
		})
	})
}

// openEditor opens a separate, non-modal window for one server. Several
// editors may be open at once.
func (a *app) openEditor(id string) {
	s, ok := a.serverByID(id)
	if !ok {
		return
	}
	edit := *s
	title := "编辑服务器 · " + edit.displayName()
	// Create the window from another goroutine so it happens after this
	// build pass, not in the middle of it.
	go func() {
		var win *mygo.Window
		win = mygo.NewWindow(mygo.WindowOptions{
			Title:    title,
			Width:    720,
			Height:   640,
			MinWidth: 600, MinHeight: 480,
			Content: ui.View(func(c *ui.Context) {
				a.editorView(c, win, &edit, id)
			}),
		})
		_ = win.SetIcon(appIconPNG)
	}()
}

func (a *app) serverByID(id string) (*serverConfig, bool) {
	for i := range a.cfg.Servers {
		if a.cfg.Servers[i].ID == id {
			return &a.cfg.Servers[i], true
		}
	}
	return nil, false
}

func (a *app) activeServer() *serverConfig {
	if s, ok := a.serverByID(a.cfg.ActiveID); ok {
		return s
	}
	if len(a.cfg.Servers) > 0 {
		return &a.cfg.Servers[0]
	}
	return nil
}

func (a *app) setActive(id string) {
	if _, ok := a.serverByID(id); !ok {
		return
	}
	a.cfg.ActiveID = id
}

// cursorServerID is the server the keyboard acts on: the cursor row, or the
// first chosen one.
func (a *app) cursorServerID() (string, bool) {
	if a.cursor >= 0 && a.cursor < len(a.cfg.Servers) {
		return a.cfg.Servers[a.cursor].ID, true
	}
	for id := range a.chosen.All() {
		return id, true
	}
	return "", false
}

func (a *app) selectedIDs() []string {
	ids := make([]string, 0, a.chosen.Len())
	for id := range a.chosen.All() {
		ids = append(ids, id)
	}
	return ids
}

func (a *app) addServer() {
	s := defaultServerConfig()
	s.Name = "新服务器"
	a.cfg.Servers = append(a.cfg.Servers, s)
	if a.cfg.ActiveID == "" {
		a.cfg.ActiveID = s.ID
	}
}

func (a *app) duplicateSelected() {
	ids := a.selectedIDs()
	if len(ids) == 0 {
		return
	}
	for _, id := range ids {
		s, ok := a.serverByID(id)
		if !ok {
			continue
		}
		cp := *s
		cp.ID = newServerID()
		if cp.Name != "" {
			cp.Name += " 副本"
		}
		a.cfg.Servers = append(a.cfg.Servers, cp)
	}
}

func (a *app) deleteSelected() {
	ids := make(map[string]bool, a.chosen.Len())
	for id := range a.chosen.All() {
		ids[id] = true
	}
	if len(ids) == 0 {
		return
	}
	kept := a.cfg.Servers[:0]
	for _, s := range a.cfg.Servers {
		if !ids[s.ID] {
			kept = append(kept, s)
		}
	}
	a.cfg.Servers = kept
	a.chosen.Clear()
	a.cursor = -1
	if ids[a.cfg.ActiveID] {
		if len(a.cfg.Servers) > 0 {
			a.cfg.ActiveID = a.cfg.Servers[0].ID
		} else {
			s := defaultServerConfig()
			a.cfg.Servers = []serverConfig{s}
			a.cfg.ActiveID = s.ID
		}
	}
}

func (a *app) updateServer(id string, s serverConfig) {
	for i := range a.cfg.Servers {
		if a.cfg.Servers[i].ID == id {
			s.ID = id
			a.cfg.Servers[i] = s
			return
		}
	}
}

// applySort reorders the list in place when the user clicks a sortable
// column header.
func (a *app) applySort() {
	if a.sort == a.lastSort {
		return
	}
	a.lastSort = a.sort
	if a.sort.Column == "" {
		return
	}
	sort.SliceStable(a.cfg.Servers, func(i, j int) bool {
		x, y := a.cfg.Servers[i], a.cfg.Servers[j]
		var xi, yi string
		switch a.sort.Column {
		case "name":
			xi, yi = x.displayName(), y.displayName()
		case "protocol":
			xi, yi = x.Protocol, y.Protocol
		case "address":
			xi, yi = x.Address, y.Address
		case "port":
			xi, yi = x.Port, y.Port
		default:
			return false
		}
		if a.sort.Descending {
			return xi > yi
		}
		return xi < yi
	})
}

// logTab: the run log, inline in the main window. The text is selectable
// (drag, double-click, Ctrl+C) and a button copies everything at once.
func (a *app) logTab(c *ui.Context) {
	t := c.Theme()
	ui.Column(c).Fill().Padding(0, 4, 8).Gap(6).Children(func() {
		ui.Row(c).Children(func() {
			ui.Text(c, "选中文本后 Ctrl+C 复制，或").TextColor(t.TextMuted).Grow(1)
			if ui.Button(c.Key("copylog"), "复制全部日志").Clicked() {
				mygo.Clipboard.WriteText(strings.Join(a.logs, "\n"))
			}
		})
		ui.Scroll(c).Grow(1).Children(func() {
			ui.Text(c, strings.Join(a.logs, "\n")).FontSize(12).TextColor(t.TextMuted).Selectable()
		})
	})
}

// Shared field specs for settings that appear outside protocol fields.
var (
	networkField      = selectField("传输", func(c *serverConfig) *string { return &c.Network }, networks)
	securityField     = selectField("方式", func(c *serverConfig) *string { return &c.Security }, securities)
	securityQuicField = selectField("方式", func(c *serverConfig) *string { return &c.Security }, quicSecurities)
	fingerprintField  = selectField("uTLS 指纹", func(c *serverConfig) *string { return &c.Fingerprint }, fingerprints)
)

// renderChoice draws a labeled choice: radio buttons for up to four options,
// a dropdown for five or more. See docs/design-guidelines.md.
func renderChoice(c *ui.Context, label string, selected *string, options []string) {
	ui.Field(c, label, func() {
		if len(options) <= 4 {
			ui.RadioGroup(c, func() {
				for _, opt := range options {
					ui.Radio(c, selected, opt, opt)
				}
			}).Row().Gap(14).Wrap()
			return
		}
		ui.Select(c, selected, options)
	})
}

// renderField draws one fieldSpec: a choice, a checkbox or a text input.
func renderField(c *ui.Context, f fieldSpec, cfg *serverConfig) {
	switch {
	case f.options != nil:
		renderChoice(c, f.label, f.bindString(cfg), f.options)
	case f.bindBool != nil:
		ui.Checkbox(c, f.bindBool(cfg), f.label)
	default:
		ui.Field(c, f.label, func() { ui.TextInput(c, f.bindString(cfg)).Placeholder(f.placeholder) })
	}
}

// showMain brings the main window back from the tray.
func (a *app) showMain() {
	if a.win == nil || a.win.IsDestroyed() {
		return
	}
	a.win.Show()
	a.win.Focus()
}

// toggleMain shows the main window, or hides it to the tray when it is
// already visible.
func (a *app) toggleMain() {
	if a.win != nil && a.win.IsVisible() {
		a.win.Hide()
		return
	}
	a.showMain()
}

// coreActionLabel is the tray entry for the core's next action.
func (a *app) coreActionLabel() string {
	if a.core.Running() {
		return "停止"
	}
	return "启动"
}

// updateTrayMenu keeps the start/stop entry in step with the core. The
// item is shared by the popup menus of macOS and Windows and by the
// persistent menu of Linux.
func (a *app) updateTrayMenu() {
	if a.trayToggle != nil {
		a.trayToggle.SetLabel(a.coreActionLabel())
	}
}

// toggleCore starts or stops the core from the tray.
func (a *app) toggleCore() {
	if a.core.Running() {
		a.stop()
		return
	}
	a.start()
}

// setUpTray adds the menu bar / notification area icon. A right click opens
// a menu with start/stop, Show and Quit; a left click shows or hides the
// main window.
func (a *app) setUpTray() {
	icon, template := trayColorPNG, false
	if runtime.GOOS == "darwin" || runtime.GOOS == "windows" {
		// macOS tints a template icon to match the menu bar; Windows turns
		// it white on a dark taskbar.
		icon, template = trayTemplatePNG, true
	}
	tray, err := mygo.NewTray(mygo.TrayOptions{
		ID:             "sun-ray",
		Icon:           icon,
		IconIsTemplate: template,
		ToolTip:        "Sun Ray",
	})
	if err != nil {
		log.Print("tray: ", err)
		return
	}
	a.tray = tray
	a.trayToggle = &mygo.MenuItem{
		Label: a.coreActionLabel(),
		Click: func(*mygo.MenuItem, *mygo.Window) { a.toggleCore() },
	}
	menu := mygo.NewMenu([]*mygo.MenuItem{
		a.trayToggle,
		{Label: "显示主窗口", Click: func(*mygo.MenuItem, *mygo.Window) { a.showMain() }},
		mygo.Separator(),
		{Role: mygo.RoleQuit},
	})
	if runtime.GOOS == "linux" {
		// AppIndicator only shows menus, so its icon opens this one.
		tray.SetMenu(menu)
		return
	}
	tray.OnClick(a.toggleMain)
	tray.OnRightClick(func() {
		a.updateTrayMenu()
		tray.SetMenu(menu)
		defer tray.SetMenu(nil)
		tray.PopUpMenu()
	})
}

func main() {
	a := &app{cfg: loadConfig(), cursor: -1, tests: map[string]*testState{}}
	a.saver.remember(a.cfg)
	mygo.App.SetName("Sun Ray")
	mygo.App.OnBeforeQuit(func(*mygo.QuitEvent) { a.quitting = true })
	mygo.App.OnWillQuit(func(*mygo.QuitEvent) {
		a.saver.maybeSave(a.cfg, true)
		_ = a.core.Stop()
		if a.tray != nil {
			a.tray.Destroy()
			a.tray = nil
		}
	})
	mygo.App.OnActivate(func(hasVisibleWindows bool) {
		if !hasVisibleWindows {
			a.showMain()
		}
	})
	mygo.App.WhenReady(func() {
		a.win = mygo.NewWindow(mygo.WindowOptions{
			Title: "Sun Ray",
			Width: 880, Height: 620,
			MinWidth: 720, MinHeight: 520,
			Content: ui.View(a.view),
		})
		_ = a.win.SetIcon(appIconPNG)
		a.win.OnClose(func(e *mygo.CloseEvent) {
			// Closing the main window keeps the app in the tray. Without a
			// tray there would be no way back, so let it close normally.
			if a.quitting || a.tray == nil {
				return
			}
			e.PreventDefault()
			a.win.Hide()
		})
		a.setUpTray()
		go a.pollTraffic()
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}
