//go:build !nogui

package main

// Sun Ray: a simple Xray GUI client in native UI. The xray-core library
// runs inside this very process (see core.go + protocols.go); the window
// below is drawn by mygo itself, with no webview. The UI is tabbed:
// global settings, the server list and the run log live on separate tabs.
// Each server in the list opens its own editor window, reusing serverForm.

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"runtime"
	"runtime/debug"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/egoist/mygo"
	_ "github.com/egoist/mygo/locales/zh-Hans"
	"github.com/egoist/mygo/ui"
)

// maxLogChars bounds the run log. The log is a debugging aid, so keeping the
// newest 500K characters is plenty and avoids growing without limit.
const maxLogChars = 500_000

// restartDelay is how long the app waits for the settings to settle before
// it restarts a running core.
const restartDelay = 1200 * time.Millisecond

// Tab indexes in the main window.
const (
	tabGlobal = iota
	tabServer
	tabSubscription
	tabLog
)

type app struct {
	cfg            appConfig
	core           xrayCore
	win            *mygo.Window
	tray           *mygo.Tray
	trayToggle     *mygo.MenuItem        // start/stop entry in the tray menu
	quitting       bool                  // a real quit is under way, so closing may proceed
	tab            int                   // selected tab in the main window
	saver          configSaver           // debounced persistence to ~/sunray.json
	tests          map[string]*testState // per server measurement results
	lastProxyMode  string                // the mode the system proxy was set to
	lastProxyAddr  string                // the address it was set to
	pac            *pacServer            // the local server that serves the PAC file
	lastConfig     string                // fingerprint of the config the core runs with
	restartAt      time.Time             // when to restart the core after a change
	lastEnvProxy   bool                  // the env proxy setting the system has
	lastEnvAddr    string                // the address it was set to
	lastTrayTip    string                // the hover text the tray shows
	tunWarningText string                // why the TUN interface cannot be created
	testing        bool                  // a measurement run is in progress
	opMu           sync.Mutex            // serializes start/stop/restart
	tunMu          sync.Mutex            // protects tunHelper
	tunHelper      *tunHelper            // the privileged TUN helper, when running

	logs     []string // xray log lines, newest last
	logChars int      // total rune count of logs when joined with newlines

	upSpeed, downSpeed string // per-second rates shown in the header

	tempCore atomic.Bool // a temporary core (speed test) is running

	// Server list state.
	list           ui.ListState
	chosen         ui.Selection[string] // selected server ids, for batch actions
	cursor         int                  // row the keyboard acts on, -1 for none
	sort           ui.SortOrder
	lastSort       ui.SortOrder
	listCells      []ui.Handle // rows in the last server list build
	listCellN      int         // rows bound in listCells this frame
	clearSelection bool        // a click outside the rows clears the choice

	// Subscription list state.
	subList      ui.ListState
	subChosen    ui.Selection[string] // selected subscription ids
	subCursor    int                  // row the keyboard acts on, -1 for none
	subSort      ui.SortOrder
	subLastSort  ui.SortOrder
	subListCells []ui.Handle // rows in the last subscription list build
	subListCellN int         // rows bound in subListCells this frame
	subUpdating  map[string]bool
}

// syncEnvProxy sets the proxy environment variables for the user. Windows
// gets real user variables, macOS and Linux get the shell commands
// copied to the clipboard.
func (a *app) syncEnvProxy(g *globalConfig) {
	addr := "127.0.0.1:" + g.ListenPort
	if g.EnvProxy == a.lastEnvProxy && addr == a.lastEnvAddr {
		return
	}
	a.lastEnvProxy, a.lastEnvAddr = g.EnvProxy, addr
	commands, err := applyEnvProxy(g.EnvProxy, addr)
	if err != nil {
		a.addLog("[sun-ray] 代理环境变量设置失败: " + err.Error())
		return
	}
	if commands != "" {
		if a.win != nil {
			mygo.Clipboard.WriteText(commands)
		}
		a.addLog("[sun-ray] 已把设置代理环境变量的命令复制到剪贴板")
		return
	}
	if g.EnvProxy {
		a.addLog("[sun-ray] 已设置代理环境变量：" + addr)
	} else {
		a.addLog("[sun-ray] 已清除代理环境变量")
	}
} // routingModeLabel names the routing in a few words, for the tray tip.
func routingModeLabel(g globalConfig) string {
	prefix := ""
	if g.TunEnabled {
		prefix = "TUN·"
	}
	switch {
	case g.RouteCN && g.RouteAds:
		return prefix + "分流·大陆直连+拦截广告"
	case g.RouteCN:
		return prefix + "分流·大陆直连"
	case g.RouteAds:
		return prefix + "分流·拦截广告"
	default:
		return prefix + "全部代理"
	}
}

// trayTipText is the hover text of the tray icon: the server in use, the
// current speeds and the routing mode.
func (a *app) trayTipText() string {
	name := "未选择服务器"
	if s := a.activeServer(); s != nil {
		name = s.displayName()
	}
	up, down := a.upSpeed, a.downSpeed
	if up == "" {
		up = "—"
	}
	if down == "" {
		down = "—"
	}
	return fmt.Sprintf("Sun Ray · %s · ↑%s ↓%s · %s", name, up, down, routingModeLabel(a.cfg.Global))
}

// updateTrayIcon shows a filled icon while the core runs, and an outlined
// one while it is stopped.
func (a *app) updateTrayIcon() {
	if a.tray == nil {
		return
	}
	var icon []byte
	if a.core.Running() {
		icon = trayTemplatePNG
	} else {
		icon = trayTemplateOutlinePNG
	}
	_ = a.tray.SetIcon(icon, true)
}

// updateTrayTip refreshes the tray hover text, when it changed.
func (a *app) updateTrayTip() {
	if a.tray == nil {
		return
	}
	tip := a.trayTipText()
	if tip == a.lastTrayTip {
		return
	}
	a.lastTrayTip = tip
	a.tray.SetToolTip(tip)
} // configFingerprint describes the settings the core runs with, so the app
// can tell when a change needs the core to restart.
func (a *app) configFingerprint() string {
	global := a.cfg.Global
	global.ProxyMode = "" // not a setting of the core
	data, _ := json.Marshal(struct {
		Global globalConfig
		Server *serverConfig
	}{global, a.activeServer()})
	return string(data)
}

// watchConfig restarts a running core when the settings it runs with
// changed, so the new settings take effect.
func (a *app) watchConfig() {
	fp := a.configFingerprint()
	if fp != a.lastConfig {
		a.lastConfig = fp
		a.restartAt = time.Now().Add(restartDelay)
		return
	}
	if a.restartAt.IsZero() || time.Now().Before(a.restartAt) {
		return
	}
	a.restartAt = time.Time{}
	if !a.core.Running() {
		return
	}
	go a.restartCore()
}

// restartCore stops and starts the core with the current settings. When
// only the in-process settings changed, a running TUN helper is kept, so
// the user is not asked for administrator rights again.
func (a *app) restartCore() {
	a.opMu.Lock()
	defer a.opMu.Unlock()

	g := a.cfg.Global
	if g.TunEnabled {
		if iface, err := defaultRouteInterface(); err == nil {
			if h := a.currentTunHelper(); h != nil && sameTun(h.settings, tunSettingsFor(&g, iface, h.settings.TunName)) {
				_ = a.core.Stop()
				a.startLocked()
				return
			}
		}
	}
	a.stopLocked()
	a.startLocked()
}

// logVersion writes the version as the first line of the log.
func (a *app) logVersion() {
	a.win.Update(func() {
		a.logs, a.logChars = appendLogLine(a.logs, a.logChars, "Sun Ray "+versionString(), maxLogChars)
	})
}

func (a *app) addLog(line string) {
	// Tell the two cores apart: the one the user runs and the temporary
	// one a speed test starts.
	source := "[当前] "
	if a.tempCore.Load() {
		source = "[测速] "
	}
	a.win.Update(func() {
		a.logs, a.logChars = appendLogLine(a.logs, a.logChars, source+line, maxLogChars)
	})
}

// appendLogLine appends line and drops the oldest lines until the joined log
// fits within limit characters. It returns the new log and its character count.
func appendLogLine(logs []string, chars int, line string, limit int) ([]string, int) {
	if len(logs) > 0 {
		chars++ // newline between the previous line and this one
	}
	logs = append(logs, line)
	chars += utf8.RuneCountInString(line)

	for chars > limit && len(logs) > 1 {
		chars -= utf8.RuneCountInString(logs[0]) + 1
		logs[0] = "" // release the dropped line for the garbage collector
		logs = logs[1:]
	}
	if chars > limit {
		// A single line is longer than the whole budget; keep its tail.
		runes := []rune(logs[0])
		if len(runes) > limit {
			runes = runes[len(runes)-limit:]
		}
		logs[0] = string(runes)
		chars = len(runes)
	}
	if cap(logs) > 2*len(logs)+64 {
		compact := make([]string, len(logs))
		copy(compact, logs)
		logs = compact
	}
	return logs, chars
}

// start validates the active server, builds the config and boots the
// in-process core, all through function calls.
func (a *app) start() {
	a.opMu.Lock()
	defer a.opMu.Unlock()
	a.startLocked()
}

func (a *app) startLocked() {
	defer a.updateTrayMenu()
	a.saver.maybeSave(a.cfg, true)
	s := a.activeServer()
	if s == nil {
		a.addLog("[sun-ray] 没有可用的服务器")
		return
	}

	g := a.cfg.Global
	if g.TunEnabled {
		if msg := tunSupportWarning(); msg != "" {
			a.addLog("[sun-ray] " + msg)
			return
		}
		iface, err := defaultRouteInterface()
		if err != nil {
			a.addLog("[sun-ray] TUN 模式找不到默认路由的网卡: " + err.Error())
			return
		}
		if h := a.currentTunHelper(); h != nil && !sameTun(h.settings, tunSettingsFor(&g, iface, h.settings.TunName)) {
			a.stopTunHelper()
		}
		g.TunHelper = true
		g.TunBypassInterface = iface

		cfgJSON, err := s.buildJSON(&g)
		if err != nil {
			a.addLog("[sun-ray] " + err.Error())
			return
		}
		if err := a.core.Start(cfgJSON, a.addLog); err != nil {
			a.addLog("[sun-ray] 启动失败: " + err.Error())
			a.stopTunHelper()
			return
		}
		if a.currentTunHelper() == nil {
			if _, err := a.startTunHelper(&g, iface); err != nil {
				_ = a.core.Stop()
				if err == errElevationCancelled {
					a.addLog("[sun-ray] 已取消 TUN 权限请求")
				} else {
					a.addLog("[sun-ray] TUN 辅助进程启动失败: " + err.Error())
				}
				a.updateTrayIcon()
				return
			}
			a.addLog("[sun-ray] TUN 辅助进程已启动")
		}
		a.addLog("[sun-ray] 内核已在本进程内启动")
		a.addLog("[sun-ray] TUN 已接管全部流量")
		a.updateTrayIcon()
		return
	}

	a.stopTunHelper()
	g.TunHelper = false
	g.TunBypassInterface = ""
	cfgJSON, err := s.buildJSON(&g)
	if err != nil {
		a.addLog("[sun-ray] " + err.Error())
		return
	}
	if err := a.core.Start(cfgJSON, a.addLog); err != nil {
		a.addLog("[sun-ray] 启动失败: " + err.Error())
		return
	}
	a.addLog("[sun-ray] 内核已在本进程内启动")
	a.updateTrayIcon()
}

func (a *app) stop() {
	a.opMu.Lock()
	defer a.opMu.Unlock()
	a.stopLocked()
}

func (a *app) stopLocked() {
	defer a.updateTrayMenu()
	a.saver.maybeSave(a.cfg, true)
	if err := a.core.Stop(); err != nil {
		a.addLog("[sun-ray] 停止出错: " + err.Error())
	}
	a.stopTunHelper()
	a.addLog("[sun-ray] 内核已停止")
	a.updateTrayIcon()
}

// currentTunHelper returns the running helper, if any.
func (a *app) currentTunHelper() *tunHelper {
	a.tunMu.Lock()
	defer a.tunMu.Unlock()
	return a.tunHelper
}

// startTunHelper launches the helper and records it. The caller holds
// opMu; tunMu only guards the pointer.
func (a *app) startTunHelper(g *globalConfig, iface string) (*tunHelper, error) {
	a.tunMu.Lock()
	defer a.tunMu.Unlock()
	h, err := startTunHelper(g, iface, a.addLog)
	if err != nil {
		return nil, err
	}
	a.tunHelper = h
	return h, nil
}

// stopTunHelper removes the TUN interface and clears the pointer.
func (a *app) stopTunHelper() {
	a.tunMu.Lock()
	h := a.tunHelper
	a.tunHelper = nil
	a.tunMu.Unlock()
	if h != nil {
		h.stop()
		a.addLog("[sun-ray] TUN 辅助进程已停止")
	}
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
			a.updateTrayTip()
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
	a.watchConfig()
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

		ui.Tabs(c, &a.tab, "全局", "服务器列表", "订阅", "日志")
		switch a.tab {
		case tabGlobal:
			a.globalTab(c)
		case tabServer:
			a.serverListTab(c)
		case tabSubscription:
			a.subscriptionListTab(c)
		case tabLog:
			a.logTab(c)
		}
		if a.tab != tabServer {
			a.listCellN = 0
		}
		if a.tab != tabSubscription {
			a.subListCellN = 0
		}

		// A click anywhere outside a list row clears the choice, after
		// the widgets have handled it: a toolbar button still acts on the
		// selection it was clicked with.
		if a.clearSelection {
			a.clearSelection = false
			a.chosen.Clear()
			a.cursor = -1
			a.subChosen.Clear()
			a.subCursor = -1
		}
	}).HandleInput(func(ev ui.InputEvent) bool {
		if ev.Kind == ui.InputPointerDown && ev.Button == 0 && !a.pointerOverListRow(c, ev.X, ev.Y) {
			a.clearSelection = true
		}
		return false
	})
}

// pointerOverListRow reports whether the point is over a row of the list
// on the current tab, as the last frame built it.
func (a *app) pointerOverListRow(c *ui.Context, x, y float32) bool {
	cells, n := a.listCells, a.listCellN
	if a.tab == tabSubscription {
		cells, n = a.subListCells, a.subListCellN
	}
	for i := 0; i < n && i < len(cells); i++ {
		r := cells[i].Bounds(c)
		if x >= r.X && x < r.X+r.W && y >= r.Y && y < r.Y+r.H {
			return true
		}
	}
	return false
}

// globalTab: inbound ports, sniffing, log level and routing presets.
func (a *app) globalTab(c *ui.Context) {
	g := &a.cfg.Global
	t := c.Theme()
	ui.Row(c).Fill().Gap(24).AlignItems(ui.Stretch).Children(func() {
		ui.Column(c).Grow(1).MinWidth(0).Gap(8).Children(func() {
			ui.Text(c, "入站（本机代理）").Bold()
			ui.Field(c, "监听端口（SOCKS 与 HTTP 共用）", func() {
				ui.TextInput(c, &g.ListenPort).Placeholder("10808")
			})
			ui.Checkbox(c, &g.AllowLAN, "允许局域网连接")
			ui.Checkbox(c, &g.AutoStart, "启动后自动运行代理")
			ui.Checkbox(c, &g.EnvProxy, "设置代理环境变量（终端可用）")
			ui.Checkbox(c, &g.SoftRender, "软件渲染（省内存约 25MB，Windows/Linux 有效）")
			a.syncEnvProxy(g)

			ui.Text(c, "TUN 模式").Bold()
			ui.Checkbox(c, &g.TunEnabled, "启用 TUN（接管全部流量，启动时请求管理员权限）")
			if g.TunEnabled {
				ui.Field(c, "接口名", func() {
					ui.TextInput(c, &g.TunName).Placeholder(tunNameHint())
				})
				ui.Field(c, "MTU", func() {
					ui.NumberInput(c, &g.TunMTU, 576, 9000, 100)
				})
				ui.Field(c, "网关地址", func() {
					ui.TextInput(c, &g.TunGateway).Placeholder(defaultTunGateway)
				})
				ui.Checkbox(c, &g.TunIPv6, "同时接管 IPv6")
				if msg := a.tunWarning(); msg != "" {
					ui.Text(c, msg).TextColor(t.TextMuted)
				}
			}
		})

		ui.Column(c).Grow(1).MinWidth(0).Gap(8).Children(func() {
			if g.TunEnabled {
				ui.Text(c, "系统代理").Bold()
				ui.Text(c, "TUN 模式接管全部流量，不再修改系统代理").TextColor(t.TextMuted)
				if g.ProxyMode != proxyModeOff {
					g.ProxyMode = proxyModeOff
					a.clearSystemProxySetting()
				}
			} else {
				ui.Text(c, "系统代理").Bold()
				ui.Field(c, "代理模式", func() {
					ui.RadioGroup(c, func() {
						ui.Row(c).Gap(14).Children(func() {
							for _, mode := range proxyModeOptions {
								ui.Radio(c, &g.ProxyMode, mode, proxyModeLabel(mode))
							}
						})
					})
				})
				if ui.Button(c.Key("clearsysproxy"), "清空系统代理").Clicked() {
					g.ProxyMode = proxyModeOff
					a.clearSystemProxySetting()
				}
				a.syncSystemProxy(g)
			}

			ui.Text(c, "日志").Bold()
			renderChoice(c, "日志级别", &g.LogLevel, logLevels)
			ui.Text(c, "路由预设").Bold()
			ui.Checkbox(c, &g.RouteCN, "绕过大陆（geosite:cn / geoip:cn 直连）")
			ui.Checkbox(c, &g.RouteAds, "屏蔽广告（geosite:category-ads-all）")
			a.updateTrayTip()
		})
	})
}

// tunWarning says why the TUN interface cannot be created, if that is the
// case. The rights do not change while the app runs, so it asks once.
func (a *app) tunWarning() string {
	if a.tunWarningText == "" {
		a.tunWarningText = tunSupportWarning()
	}
	return a.tunWarningText
}

// serverListTab: the list of servers. A double click edits a server in its
// own window; Enter makes the chosen server active.
func (a *app) serverListTab(c *ui.Context) {
	t := c.Theme()
	a.applySort()
	a.listCellN = 0
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
			{Title: "当前", ID: "active", Width: 60, Align: ui.Center},
			{Title: "名称", ID: "name", Sortable: true},
			{Title: "协议", ID: "protocol", Width: 100, Sortable: true},
			{Title: "地址", ID: "address", Sortable: true},
			{Title: "端口", ID: "port", Width: 80, Align: ui.End, Sortable: true},
			{Title: "延迟", ID: "latency", Width: 90, Align: ui.End},
			{Title: "速度", ID: "speed", Width: 110, Align: ui.End},
		}
		dbl := -1
		tbl := ui.Table(c, &a.list, cols, len(a.cfg.Servers), func(row, col int) {
			s := &a.cfg.Servers[row]
			cell := ui.Box(c).FillWidth().Children(func() {
				switch col {
				case 0:
					if s.ID == a.cfg.ActiveID {
						ui.Text(c, "✓")
					}
				case 1:
					ui.Text(c, s.displayName()).SingleLine()
				case 2:
					ui.Text(c, s.Protocol)
				case 3:
					ui.Text(c, s.Address).SingleLine()
				case 4:
					ui.Text(c, s.Port)
				case 5:
					st := a.testStateOf(s.ID)
					switch {
					case st != nil && st.running:
						ui.Text(c, "测试中…")
					case st != nil && st.failed:
						ui.Text(c, "失败")
					default:
						if r, ok := a.cfg.Results[s.ID]; ok {
							ui.Text(c, formatLatency(time.Duration(r.LatencyMs)*time.Millisecond))
						} else {
							ui.Text(c, "-")
						}
					}
				case 6:
					st := a.testStateOf(s.ID)
					switch {
					case st != nil && st.running:
						ui.Text(c, "测试中…")
					case st != nil && st.failed:
						ui.Text(c, "-")
					default:
						if r, ok := a.cfg.Results[s.ID]; ok && r.SpeedBps > 0 {
							ui.Text(c, "↓ "+formatSpeed(r.SpeedBps))
						} else {
							ui.Text(c, "-")
						}
					}
				}
			})
			cell = cell.ContextMenu(func(m *ui.Menu) {
				if m.Item("设为当前使用").Chosen() {
					a.setActive(s.ID)
				}
				m.Separator()
				if m.Item("测试").Chosen() {
					a.runTestsFor([]string{s.ID})
				}
				if m.Item("删除").Chosen() {
					a.deleteServers(a.deleteTargets(s.ID))
				}
			})
			if cell.RowDoubleClicked() {
				dbl = row
			}
			if a.listCellN == len(a.listCells) {
				a.listCells = append(a.listCells, ui.Handle{})
			}
			cell.Bind(&a.listCells[a.listCellN])
			a.listCellN++
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

// subscriptionListTab: the subscription list. A double click edits a
// subscription in its own window; Enter does the same.
func (a *app) subscriptionListTab(c *ui.Context) {
	t := c.Theme()
	a.applySubscriptionSort()
	a.subListCellN = 0
	a.subList.Key = func(row int) any { return a.cfg.Subscriptions[row].ID }
	a.subList.Selection = &a.subChosen
	a.subList.Selected = &a.subCursor
	a.subList.Sort = &a.subSort

	ui.Column(c).Fill().Padding(0, 4, 8).Gap(8).Children(func() {
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			if ui.Button(c.Key("subadd"), "新增").Clicked() {
				a.addSubscription()
			}
			if ui.Button(c.Key("subedit"), "编辑").Clicked() {
				if id, ok := a.cursorSubscriptionID(); ok {
					a.openSubscriptionEditor(id)
				}
			}
			if ui.Button(c.Key("subdup"), "复制").Clicked() {
				a.duplicateSelectedSubscriptions()
			}
			if ui.Button(c.Key("subdelsel"), "删除").Clicked() {
				a.deleteSelectedSubscriptions()
			}
			ui.Text(c, fmt.Sprintf("共 %d 个", len(a.cfg.Subscriptions))).TextColor(t.TextMuted).Grow(1)
		})

		cols := []ui.TableColumn{
			{Title: "名称", ID: "name", Sortable: true},
			{Title: "订阅地址", ID: "url", Sortable: true},
			{Title: "节点", ID: "servers", Width: 80, Align: ui.End, Sortable: true},
			{Title: "更新时间", ID: "updated", Width: 150, Align: ui.End, Sortable: true},
		}
		dbl := -1
		tbl := ui.Table(c, &a.subList, cols, len(a.cfg.Subscriptions), func(row, col int) {
			s := &a.cfg.Subscriptions[row]
			cell := ui.Box(c).FillWidth().Children(func() {
				switch col {
				case 0:
					ui.Text(c, s.displayName()).SingleLine()
				case 1:
					ui.Text(c, s.URL).SingleLine()
				case 2:
					if s.ServerCount > 0 {
						ui.Text(c, strconv.Itoa(s.ServerCount))
					} else {
						ui.Text(c, "-")
					}
				case 3:
					ui.Text(c, formatSubscriptionTime(s.UpdatedAt))
				}
			})
			cell = cell.ContextMenu(func(m *ui.Menu) {
				if m.Item("编辑").Chosen() {
					a.openSubscriptionEditor(s.ID)
				}
				if m.Item("复制").Chosen() {
					a.duplicateSubscription(s.ID)
				}
				m.Separator()
				if m.Item("删除").Chosen() {
					a.deleteSubscriptions(a.subscriptionDeleteTargets(s.ID))
				}
			})
			if cell.RowDoubleClicked() {
				dbl = row
			}
			if a.subListCellN == len(a.subListCells) {
				a.subListCells = append(a.subListCells, ui.Handle{})
			}
			cell.Bind(&a.subListCells[a.subListCellN])
			a.subListCellN++
		}).Grow(1)

		switch {
		case dbl >= 0:
			a.subCursor = dbl
			a.openSubscriptionEditor(a.cfg.Subscriptions[dbl].ID)
		case tbl.Submitted():
			if id, ok := a.cursorSubscriptionID(); ok {
				a.openSubscriptionEditor(id)
			}
		}
	})
}

func formatSubscriptionTime(ts int64) string {
	if ts <= 0 {
		return "-"
	}
	return time.Unix(ts, 0).Format("2006-01-02 15:04")
}

// subscriptionForm is the content of a subscription's editor window.
func (a *app) subscriptionForm(c *ui.Context, s *subscriptionConfig) {
	t := c.Theme()
	ui.Column(c).Gap(8).Children(func() {
		ui.Field(c, "名称（可选）", func() {
			ui.TextInput(c, &s.Name).Placeholder("例如 机场订阅")
		})
		ui.Field(c, "订阅地址", func() {
			ui.TextInput(c, &s.URL).Placeholder("https://example.com/api/v1/client/subscribe?token=...")
		})
		ui.Text(c, "常见订阅会返回 Base64 编码的节点链接列表，也可能直接返回明文链接。").TextColor(t.TextMuted)
	})
}

func (a *app) subscriptionEditorView(c *ui.Context, win *mygo.Window, s *subscriptionConfig, id string) {
	ui.Column(c).Fill().Padding(12, 16).Gap(10).Children(func() {
		ui.Text(c, "订阅设置").FontSize(16).Bold()
		ui.Scroll(c).Grow(1).Children(func() {
			a.subscriptionForm(c, s)
		})
		ui.Row(c).Gap(8).Children(func() {
			if ui.PrimaryButton(c.Key("subeditorsave"), "保存").Clicked() {
				a.updateSubscription(id, *s)
				a.saver.maybeSave(a.cfg, true)
				win.Close()
			}
			if ui.Button(c.Key("subeditorcancel"), "取消").Clicked() {
				win.Close()
			}
		})
	})
}

func (a *app) openSubscriptionEditor(id string) {
	s, ok := a.subscriptionByID(id)
	if !ok {
		return
	}
	edit := *s
	title := "编辑订阅 · " + edit.displayName()
	go func() {
		var win *mygo.Window
		win = mygo.NewWindow(mygo.WindowOptions{
			Title:    title,
			Width:    620,
			Height:   360,
			MinWidth: 520, MinHeight: 300,
			Content: ui.View(func(c *ui.Context) {
				a.subscriptionEditorView(c, win, &edit, id)
			}),
		})
		_ = win.SetIcon(appIconPNG)
	}()
}

func (a *app) subscriptionByID(id string) (*subscriptionConfig, bool) {
	for i := range a.cfg.Subscriptions {
		if a.cfg.Subscriptions[i].ID == id {
			return &a.cfg.Subscriptions[i], true
		}
	}
	return nil, false
}

func (a *app) cursorSubscriptionID() (string, bool) {
	if a.subCursor >= 0 && a.subCursor < len(a.cfg.Subscriptions) {
		return a.cfg.Subscriptions[a.subCursor].ID, true
	}
	for id := range a.subChosen.All() {
		return id, true
	}
	return "", false
}

func (a *app) selectedSubscriptionIDs() []string {
	ids := make([]string, 0, a.subChosen.Len())
	for id := range a.subChosen.All() {
		ids = append(ids, id)
	}
	return ids
}

func (a *app) addSubscription() {
	a.cfg.Subscriptions = append(a.cfg.Subscriptions, defaultSubscriptionConfig())
}

func (a *app) duplicateSelectedSubscriptions() {
	for _, id := range a.selectedSubscriptionIDs() {
		a.duplicateSubscription(id)
	}
}

func (a *app) duplicateSubscription(id string) {
	s, ok := a.subscriptionByID(id)
	if !ok {
		return
	}
	cp := *s
	cp.ID = newServerID()
	cp.UpdatedAt = 0
	cp.ServerCount = 0
	cp.LastError = ""
	if cp.Name != "" {
		cp.Name += " 副本"
	}
	a.cfg.Subscriptions = append(a.cfg.Subscriptions, cp)
}

func (a *app) deleteSelectedSubscriptions() {
	a.deleteSubscriptions(a.selectedSubscriptionIDs())
}

func (a *app) subscriptionDeleteTargets(id string) []string {
	for sel := range a.subChosen.All() {
		if sel == id {
			return a.selectedSubscriptionIDs()
		}
	}
	return []string{id}
}

func (a *app) deleteSubscriptions(ids []string) {
	if len(ids) == 0 {
		return
	}
	drop := make(map[string]bool, len(ids))
	for _, id := range ids {
		drop[id] = true
	}
	kept := a.cfg.Subscriptions[:0]
	for _, s := range a.cfg.Subscriptions {
		if !drop[s.ID] {
			kept = append(kept, s)
		}
	}
	a.cfg.Subscriptions = kept
	a.subChosen.Clear()
	a.subCursor = -1

	var servers []string
	for _, s := range a.cfg.Servers {
		if drop[s.SubscriptionID] {
			servers = append(servers, s.ID)
		}
	}
	a.deleteServers(servers)
}

func (a *app) updateSubscription(id string, s subscriptionConfig) {
	for i := range a.cfg.Subscriptions {
		if a.cfg.Subscriptions[i].ID == id {
			old := a.cfg.Subscriptions[i]
			s.ID = id
			s.Name = strings.TrimSpace(s.Name)
			s.URL = strings.TrimSpace(s.URL)
			if old.URL != s.URL {
				s.UpdatedAt = 0
				s.ServerCount = 0
				s.LastError = ""
			}
			a.cfg.Subscriptions[i] = s
			return
		}
	}
}

func (a *app) applySubscriptionSort() {
	if a.subSort == a.subLastSort {
		return
	}
	a.subLastSort = a.subSort
	if a.subSort.Column == "" {
		return
	}
	sort.SliceStable(a.cfg.Subscriptions, func(i, j int) bool {
		x, y := a.cfg.Subscriptions[i], a.cfg.Subscriptions[j]
		var cmp int
		switch a.subSort.Column {
		case "name":
			cmp = strings.Compare(x.displayName(), y.displayName())
		case "url":
			cmp = strings.Compare(x.URL, y.URL)
		case "servers":
			cmp = x.ServerCount - y.ServerCount
		case "updated":
			if x.UpdatedAt < y.UpdatedAt {
				cmp = -1
			} else if x.UpdatedAt > y.UpdatedAt {
				cmp = 1
			}
		default:
			return false
		}
		if a.subSort.Descending {
			return cmp > 0
		}
		return cmp < 0
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
	a.updateTrayTip()
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
	a.deleteServers(a.selectedIDs())
}

// deleteTargets returns the ids a delete action should remove: the whole
// selection when the row is part of it, otherwise just that row.
func (a *app) deleteTargets(id string) []string {
	for sel := range a.chosen.All() {
		if sel == id {
			return a.selectedIDs()
		}
	}
	return []string{id}
}

// deleteServers removes the given servers from the list.
func (a *app) deleteServers(ids []string) {
	if len(ids) == 0 {
		return
	}
	drop := make(map[string]bool, len(ids))
	for _, id := range ids {
		drop[id] = true
		delete(a.cfg.Results, id)
	}
	kept := a.cfg.Servers[:0]
	for _, s := range a.cfg.Servers {
		if !drop[s.ID] {
			kept = append(kept, s)
		}
	}
	a.cfg.Servers = kept
	a.chosen.Clear()
	a.cursor = -1
	if drop[a.cfg.ActiveID] {
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
			old := a.cfg.Servers[i]
			s.ID = id
			a.cfg.Servers[i] = s
			// A changed endpoint makes the stored measurement stale.
			if old.Address != s.Address || old.Port != s.Port || old.Protocol != s.Protocol {
				delete(a.cfg.Results, id)
			}
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
			ui.Text(c, strings.Join(a.logs, "\n")).FontSize(12).TextColor(t.TextMuted).
				Selectable().
				ContextMenu(func(m *ui.Menu) {
					m.EditItems() // copy and select all, in the app language
					m.Separator()
					if m.Item("清空日志").Chosen() {
						a.logs = nil
						a.logChars = 0
					}
				})
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
		icon, template = trayTemplateOutlinePNG, true
	}
	tray, err := mygo.NewTray(mygo.TrayOptions{
		ID:             "sun-ray",
		Icon:           icon,
		IconIsTemplate: template,
		ToolTip:        a.trayTipText(),
	})
	if err != nil {
		log.Print("tray: ", err)
		return
	}
	a.tray = tray
	a.updateTrayIcon()
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
	// The same executable is the privileged helper when the app launches
	// it with -tun-helper. Handle that before any UI initialization.
	if runTunHelperIfRequested() {
		return
	}

	// Keep the resident size in check: the core and the UI together do not
	// need much, and a tighter heap costs a little CPU. GOMEMLIMIT and GOGC
	// in the environment win, so the user can tune the footprint.
	if os.Getenv("GOMEMLIMIT") == "" {
		debug.SetMemoryLimit(256 << 20)
	}
	if os.Getenv("GOGC") == "" {
		debug.SetGCPercent(50)
	}
	// The software rasterizer uses much less memory than the GPU, which the
	// patched mygo asks for when MYGO_WARP is set. macOS draws with Metal,
	// which has no software fallback, so the setting does nothing there.
	a := &app{cfg: loadConfig(), cursor: -1, subCursor: -1, tests: map[string]*testState{}}
	if a.cfg.Global.SoftRender {
		switch runtime.GOOS {
		case "windows":
			os.Setenv("MYGO_WARP", "1")
		case "linux":
			// Mesa draws on the CPU with llvmpipe.
			os.Setenv("LIBGL_ALWAYS_SOFTWARE", "1")
		}
	}
	a.saver.remember(a.cfg)
	a.lastConfig = a.configFingerprint()
	mygo.App.SetLocale("zh-Hans")
	mygo.App.SetName("Sun Ray")
	mygo.App.OnBeforeQuit(func(*mygo.QuitEvent) { a.quitting = true })
	mygo.App.OnWillQuit(func(*mygo.QuitEvent) {
		a.saver.maybeSave(a.cfg, true)
		_ = a.core.Stop()
		a.stopTunHelper()
		a.restoreSystemProxy()
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
		// The version is the first line of the log, so it is easy to
		// find out which build is running.
		a.logVersion()
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
		if a.cfg.Global.AutoStart {
			a.start()
		}
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}
