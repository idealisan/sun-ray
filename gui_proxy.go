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
	a.lastProxyMode, a.lastProxyAddr = g.ProxyMode, addr
	mode := g.ProxyMode
	if mode == proxyModeOff {
		// 不修改：不碰系统设置，但停掉我们自己的 PAC 服务
		a.stopPACServer()
		return
	}
	pacURL := ""
	if mode == proxyModePAC {
		if a.pac == nil {
			p, err := startPACServer(addr)
			if err != nil {
				a.addLog("[sun-ray] 启动 PAC 服务失败: " + err.Error())
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
	go func() {
		if err := applySystemProxy(mode, addr, pacURL); err != nil {
			a.addLog("[sun-ray] 设置系统代理失败: " + err.Error())
			return
		}
		a.addLog("[sun-ray] 已启用" + proxyModeLabel(mode) + "：" + addr)
	}()
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
	if g.TunEnabled == a.tunProxyOff {
		return
	}
	a.tunProxyOff = g.TunEnabled
	if g.TunEnabled {
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
