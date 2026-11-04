package main

// Sun Ray: a simple Xray GUI client in native UI. The xray-core library
// runs inside this very process (see core.go + protocols.go); the window
// below is drawn by mygo itself, with no webview. The run log lives in a
// second window, opened from the toolbar.

import (
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

const maxLogLines = 300

type app struct {
	cfg  guiConfig
	core xrayCore
	win  *mygo.Window

	logWin *mygo.Window // the run log, in its own window
	logs   []string     // xray log lines, newest last, capped

	upSpeed, downSpeed string // per-second rates shown in the header
}

func (a *app) addLog(line string) {
	a.win.Update(func() {
		a.logs = append(a.logs, line)
		if len(a.logs) > maxLogLines {
			a.logs = a.logs[len(a.logs)-maxLogLines:]
		}
	})
	if a.logWin != nil && !a.logWin.IsDestroyed() {
		a.logWin.Update(func() {}) // invalidate the log window
	}
}

// toggleLogWindow opens the log window, or brings it back to the front.
func (a *app) toggleLogWindow() {
	if a.logWin != nil && !a.logWin.IsDestroyed() {
		a.logWin.Show()
		return
	}
	a.logWin = mygo.NewWindow(mygo.WindowOptions{
		Title: "Sun Ray 运行日志",
		Width: 660, Height: 440,
		Content: ui.View(a.logView),
	})
	a.logWin.OnClosed(func() { a.logWin = nil })
}

// logView is the content of the log window: nothing but the log itself.
func (a *app) logView(c *ui.Context) {
	t := c.Theme()
	ui.Column(c).Fill().Padding(10, 12).Children(func() {
		ui.Scroll(c).Grow(1).Children(func() {
			ui.Text(c, strings.Join(a.logs, "\n")).FontSize(12).TextColor(t.TextMuted)
		})
	})
}

// start validates the fields, builds the config and boots the in-process
// core, all through function calls.
func (a *app) start() {
	cfgJSON, err := a.cfg.buildJSON()
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
	cfg := &a.cfg
	running := a.core.Running()

	ui.Column(c).Fill().Padding(12, 16).Gap(10).Children(func() {
		// Header: title, live speeds, status, log window, start/stop.
		ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
			ui.Text(c, "Sun Ray").FontSize(18).Bold().Grow(1)
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
			if ui.Button(c.Key("logwin"), "日志").Clicked() {
				a.toggleLogWindow()
			}
		})

		// Settings scroll: inbound and routing on the left, outbound on the right.
		ui.Scroll(c).Grow(1).Children(func() {
			ui.Row(c).Gap(24).AlignItems(ui.Stretch).Children(func() {
				// Left: inbound, log level, routing presets.
				ui.Column(c).Grow(1).MinWidth(0).Gap(8).Children(func() {
					ui.Text(c, "入站（本机代理）").Bold()
					ui.Field(c, "SOCKS 端口", func() {
						ui.TextInput(c, &cfg.SocksPort).Placeholder("10808")
					})
					ui.Field(c, "HTTP 端口", func() {
						ui.TextInput(c, &cfg.HTTPPort).Placeholder("10809")
					})
					ui.Checkbox(c, &cfg.AllowLAN, "允许局域网连接")
					ui.Checkbox(c, &cfg.Sniffing, "流量嗅探（域名分流需要）")

					ui.Text(c, "日志").Bold()
					ui.Field(c, "日志级别", func() {
						ui.Select(c, &cfg.LogLevel, logLevels)
					})

					ui.Text(c, "路由预设").Bold()
					ui.Checkbox(c, &cfg.RouteCN, "绕过大陆（geosite:cn / geoip:cn 直连）")
					ui.Checkbox(c, &cfg.RouteAds, "屏蔽广告（geosite:category-ads-all）")
				})
				// Right: the active protocol spec drives which fields appear.
				ui.Column(c).Grow(1).MinWidth(0).Gap(8).Children(func() {
					ui.Text(c, "出站（服务器）").Bold()
					ui.Field(c, "协议", func() {
						ui.Select(c, &cfg.Protocol, protocolList)
					})
					sp := spec(cfg.Protocol)
					if sp.needsAddr {
						ui.Field(c, "地址", func() {
							ui.TextInput(c, &cfg.Address).Placeholder("example.com")
						})
						ui.Field(c, "端口", func() {
							ui.TextInput(c, &cfg.Port).Placeholder("443")
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
						ui.Field(c, "传输", func() { ui.Select(c, &cfg.Network, networks) })
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

						ui.Text(c, "安全").Bold()
						ui.Field(c, "方式", func() { ui.Select(c, &cfg.Security, securities) })
						switch cfg.Security {
						case "tls":
							ui.Field(c, "SNI（留空用地址）", func() { ui.TextInput(c, &cfg.SNI) })
							ui.Field(c, "ALPN（逗号分隔）", func() { ui.TextInput(c, &cfg.ALPN) })
							ui.Field(c, "uTLS 指纹", func() { ui.TextInput(c, &cfg.Fingerprint).Placeholder("chrome") })
							ui.Checkbox(c, &cfg.AllowInsecure, "允许不安全证书")
						case "reality":
							ui.Field(c, "SNI / 伪装域名", func() { ui.TextInput(c, &cfg.SNI) })
							ui.Field(c, "Public Key", func() { ui.TextInput(c, &cfg.PublicKey) })
							ui.Field(c, "Short ID", func() { ui.TextInput(c, &cfg.ShortID) })
							ui.Field(c, "SpiderX", func() { ui.TextInput(c, &cfg.SpiderX) })
							ui.Field(c, "uTLS 指纹", func() { ui.TextInput(c, &cfg.Fingerprint).Placeholder("chrome") })
						}
					}
				})
			})
		})
	})
}

// renderField draws one fieldSpec: text input, select or checkbox.
func renderField(c *ui.Context, f fieldSpec, cfg *guiConfig) {
	switch {
	case f.options != nil:
		ui.Field(c, f.label, func() { ui.Select(c, f.bindString(cfg), f.options) })
	case f.bindBool != nil:
		ui.Checkbox(c, f.bindBool(cfg), f.label)
	default:
		ui.Field(c, f.label, func() { ui.TextInput(c, f.bindString(cfg)).Placeholder(f.placeholder) })
	}
}

func main() {
	a := &app{cfg: defaultConfig()}
	mygo.App.WhenReady(func() {
		a.win = mygo.NewWindow(mygo.WindowOptions{
			Title: "Sun Ray",
			Width: 900, Height: 640,
			MinWidth: 720, MinHeight: 520,
			Content: ui.View(a.view),
		})
		go a.pollTraffic()
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}
