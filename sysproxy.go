package main

import (
	"fmt"
	"time"
)

// Proxy modes, also the values stored in the config.
const (
	proxyModeOff    = "off"    // leave the system proxy alone
	proxyModeManual = "manual" // point the system proxy at the app
	proxyModePAC    = "pac"    // point the system at a PAC file
)

// proxyActionClear removes any proxy the app or the user configured.
const proxyActionClear = "clear"

// proxyModeOptions are the choices in the UI.
var proxyModeOptions = []string{proxyModeOff, proxyModeManual, proxyModePAC}

// proxyModeLabel names a mode for the user.
func proxyModeLabel(mode string) string {
	switch mode {
	case proxyModeManual:
		return "系统代理"
	case proxyModePAC:
		return "PAC 模式"
	default:
		return "不修改"
	}
}

// pacTemplate is the proxy auto-config script the app serves. It sends
// everything but local addresses to the local proxy, the way v2rayN does.
const pacTemplate = `function FindProxyForURL(url, host) {
	if (isPlainHostName(host) ||
		shExpMatch(host, "*.local") ||
		host == "localhost" ||
		host == "127.0.0.1" ||
		host == "::1") {
		return "DIRECT";
	}
	return "PROXY %s; DIRECT;";
}
`

// applySystemProxy carries out one action: proxyModeOff leaves the
// system alone, proxyActionClear removes the proxy settings.
func applySystemProxy(action, addr, pacURL string) error {
	if action == proxyModeOff {
		return nil
	}
	if action == proxyActionClear {
		return clearSystemProxy()
	}
	return applySystemProxyPlatform(action, addr, pacURL)
}

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
	if a.cfg.Global.ProxyMode == proxyModeOff {
		return
	}
	_ = applySystemProxy(proxyActionClear, "", "")
}
