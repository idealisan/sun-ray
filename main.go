package main

// Sun Ray: a simple Xray GUI client in native UI. The xray-core library
// runs inside this very process (see core.go); the window below is drawn
// by mygo itself, with no webview.

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

	logs []string // xray log lines, newest last, capped

	upSpeed, downSpeed string // per-second rates shown in the header
}

func (a *app) addLog(line string) {
	a.win.Update(func() {
		a.logs = append(a.logs, line)
		if len(a.logs) > maxLogLines {
			a.logs = a.logs[len(a.logs)-maxLogLines:]
		}
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
	running := a.core.Running()

	ui.Column(c).Fill().Padding(12, 16).Gap(10).Children(func() {
		// Header: title, live speeds, status, start/stop.
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
		})

		// Settings, inbound on the left, outbound on the right.
		ui.Row(c).Gap(20).Children(func() {
			ui.Column(c).Grow(1).Gap(6).Children(func() {
				ui.Text(c, "入站（本机代理）").Bold()
				ui.Field(c, "SOCKS 端口", func() {
					ui.TextInput(c, &a.cfg.SocksPort).Placeholder("10808")
				})
				ui.Field(c, "HTTP 端口", func() {
					ui.TextInput(c, &a.cfg.HTTPPort).Placeholder("10809")
				})
				ui.Checkbox(c, &a.cfg.AllowLAN, "允许局域网连接")
			})
			ui.Column(c).Grow(1).Gap(6).Children(func() {
				ui.Text(c, "出站（服务器）").Bold()
				ui.Field(c, "协议", func() {
					ui.Select(c, &a.cfg.Protocol, protocols)
				})
				ui.Field(c, "地址", func() {
					ui.TextInput(c, &a.cfg.Address).Placeholder("example.com")
				})
				ui.Field(c, "端口", func() {
					ui.TextInput(c, &a.cfg.Port).Placeholder("443")
				})
				if a.cfg.Protocol != "freedom" {
					ui.Field(c, "UUID / 密码", func() {
						ui.TextInput(c, &a.cfg.Secret)
					})
				}
				if a.cfg.Protocol == "shadowsocks" {
					ui.Field(c, "加密方法", func() {
						ui.TextInput(c, &a.cfg.Method)
					})
				}
				if a.cfg.Protocol != "freedom" {
					ui.Checkbox(c, &a.cfg.UseTLS, "启用 TLS")
					if a.cfg.UseTLS {
						ui.Field(c, "SNI（留空用地址）", func() {
							ui.TextInput(c, &a.cfg.SNI)
						})
					}
				}
			})
		})

		// Log pane fed by xray's in-process log handler.
		ui.Text(c, "运行日志").Bold()
		ui.Scroll(c).Grow(1).Children(func() {
			ui.Text(c, strings.Join(a.logs, "\n")).FontSize(12).TextColor(t.TextMuted)
		})
	})
}

func main() {
	a := &app{cfg: defaultConfig()}
	mygo.App.WhenReady(func() {
		a.win = mygo.NewWindow(mygo.WindowOptions{
			Title:  "Sun Ray",
			Width:  780, Height: 580,
			MinWidth: 640, MinHeight: 480,
			Content: ui.View(a.view),
		})
		go a.pollTraffic()
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}
