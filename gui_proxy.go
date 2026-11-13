//go:build !nogui

package main

import (
	"fmt"
	"time"
)

// syncSystemProxy applies the system proxy when the mode or the local
// port changed. It runs from the UI, so it only acts on a change.
func (a *app) syncSystemProxy(g *globalConfig) {
	addr := "127.0.0.1:" + g.ListenPort
	if g.ProxyMode == a.lastProxyMode && addr == a.lastProxyAddr {
		return
	}
	mode := g.ProxyMode
	if mode == proxyModeOff {
		// 不修改：不碰系统设置，但停掉我们自己的 PAC 服务
		a.lastProxyMode, a.lastProxyAddr = mode, addr
		a.stopPACServer()
		return
	}
	pacURL := ""
	if mode == proxyModePAC {
		if a.pac == nil {
			p, err := startPACServer(addr, a.pacServerFailed)
			if err != nil {
				a.addLog("[sun-ray] 启动 PAC 服务失败: " + err.Error())
				// Not recorded: the next event tries again.
				return
			}
			a.pac = p
		} else {
			a.pac.setProxy(addr)
		}
		pacURL = fmt.Sprintf("%s?t=%d", a.pac.url, time.Now().UnixNano())
	} else {
		a.stopPACServer()
	}
	a.lastProxyMode, a.lastProxyAddr = mode, addr
	go func() {
		if err := applySystemProxy(mode, addr, pacURL); err != nil {
			a.addLog("[sun-ray] 设置系统代理失败: " + err.Error())
			return
		}
		a.addLog("[sun-ray] 已启用" + proxyModeLabel(mode) + "：" + addr)
	}()
}

// pacServerFailed restarts the PAC server when it stops on its own, and
// puts the proxy back if it had gone with it. It runs on the server's
// goroutine, so it hops to the UI.
func (a *app) pacServerFailed(err error) {
	if a.win == nil {
		return
	}
	a.win.Update(func() {
		a.addLog("[sun-ray] PAC 服务已停止: " + err.Error())
		a.stopPACServer()
		a.reapplySystemProxy("PAC 服务异常")
	})
}

// ensureSystemProxy puts the proxy back when the system no longer carries
// what the settings ask for. It is called by the events that can change
// the system's network configuration — a new adapter or a route, a wake,
// a return to the app — never by a timer.
func (a *app) ensureSystemProxy(reason string) {
	g := &a.cfg.Global
	if a.tunActive() || g.ProxyMode == proxyModeOff {
		return
	}
	if a.systemProxyIsCurrent(g) {
		return
	}
	a.reapplySystemProxy(reason)
}

// reapplySystemProxy applies the proxy even though the app believes it
// already did, for an event that says the system lost it.
func (a *app) reapplySystemProxy(reason string) {
	g := &a.cfg.Global
	if a.tunActive() || g.ProxyMode == proxyModeOff {
		return
	}
	a.lastProxyMode, a.lastProxyAddr = "", ""
	a.syncSystemProxy(g)
	if a.win != nil {
		a.addLog("[sun-ray] " + reason + "，已重新应用系统代理")
	}
}

// systemProxyIsCurrent reports whether the system carries the proxy the
// settings ask for, as the system itself reports it.
func (a *app) systemProxyIsCurrent(g *globalConfig) bool {
	switch g.ProxyMode {
	case proxyModePAC:
		if a.pac == nil {
			return false
		}
		return systemProxyIsPAC(a.pac.url)
	case proxyModeManual:
		return systemProxyIsManual("127.0.0.1", g.ListenPort)
	}
	return true
}

// stopPACServer stops the local PAC server, if it runs.
func (a *app) stopPACServer() {
	if a.pac != nil {
		a.pac.stop()
		a.pac = nil
	}
}

// syncProxyForTun pauses the system proxy while TUN takes the traffic,
// and puts the user's own mode back when TUN stops. The mode itself is
// never rewritten, so it is what the app returns to.
func (a *app) syncProxyForTun(g *globalConfig) {
	active := g.TunEnabled && !a.tunFallback
	if active == a.tunProxyOff {
		return
	}
	a.tunProxyOff = active
	if active {
		a.clearSystemProxySetting()
		return
	}
	a.syncSystemProxy(g)
}

// clearSystemProxySetting removes the proxy settings from the system.
func (a *app) clearSystemProxySetting() {
	a.lastProxyMode, a.lastProxyAddr = proxyModeOff, ""
	a.stopPACServer()
	go func() {
		if err := applySystemProxy(proxyActionClear, "", ""); err != nil {
			a.addLog("[sun-ray] 清空系统代理失败: " + err.Error())
			return
		}
		a.addLog("[sun-ray] 已清空系统代理")
	}()
}

// restoreSystemProxy removes the proxy the app set, so the system is
// left as it was when the app quits.
func (a *app) restoreSystemProxy() {
	a.stopPACServer()
	if a.tunProxyOff || a.cfg.Global.ProxyMode == proxyModeOff {
		return
	}
	_ = applySystemProxy(proxyActionClear, "", "")
}
