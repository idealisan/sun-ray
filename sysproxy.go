package main

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
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

// pacTemplate is the proxy auto-config file the app writes. It sends
// everything but local addresses to the local proxy.
const pacTemplate = `function FindProxyForURL(url, host) {
	if (isPlainHostName(host) ||
		shExpMatch(host, "*.local") ||
		host == "localhost" ||
		host == "127.0.0.1" ||
		host == "::1") {
		return "DIRECT";
	}
	return "PROXY %s; SOCKS5 %s; DIRECT";
}
`

// applySystemProxy carries out one action: proxyModeOff leaves the
// system alone, proxyActionClear removes the proxy settings.
func applySystemProxy(action, addr string) error {
	if action == proxyModeOff {
		return nil
	}
	if action == proxyActionClear {
		return clearSystemProxy()
	}
	switch runtime.GOOS {
	case "windows":
		return applyWindowsProxy(action, addr)
	case "darwin":
		return applyDarwinProxy(action, addr)
	default:
		return applyLinuxProxy(action, addr)
	}
}

// pacFileURL writes the PAC file and returns the URL for it.
func pacFileURL(addr string) (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		dir = os.TempDir()
	}
	dir = filepath.Join(dir, "sun-ray")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(dir, "proxy.pac")
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return "", err
	}
	content := fmt.Sprintf(pacTemplate, host+":"+port, host+":"+port)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return "", err
	}
	return "file:///" + strings.TrimPrefix(filepath.ToSlash(path), "/"), nil
}

// syncSystemProxy applies the system proxy when the mode or the local
// port changed. It runs from the UI, so it only acts on a change.
func (a *app) syncSystemProxy(g *globalConfig) {
	addr := "127.0.0.1:" + g.ListenPort
	if g.ProxyMode == a.lastProxyMode && addr == a.lastProxyAddr {
		return
	}
	a.lastProxyMode, a.lastProxyAddr = g.ProxyMode, addr
	if g.ProxyMode == proxyModeOff {
		return // 不修改：不碰系统设置
	}
	if err := applySystemProxy(g.ProxyMode, addr); err != nil {
		a.addLog("[sun-ray] 设置系统代理失败: " + err.Error())
		return
	}
	a.addLog("[sun-ray] 已启用" + proxyModeLabel(g.ProxyMode) + "：" + addr)
}

// clearSystemProxySetting removes the proxy settings from the system.
func (a *app) clearSystemProxySetting() {
	if err := applySystemProxy(proxyActionClear, ""); err != nil {
		a.addLog("[sun-ray] 清空系统代理失败: " + err.Error())
		return
	}
	a.lastProxyMode, a.lastProxyAddr = proxyModeOff, ""
	a.addLog("[sun-ray] 已清空系统代理")
}

// restoreSystemProxy removes the proxy the app set, so the system is
// left as it was when the app quits.
func (a *app) restoreSystemProxy() {
	if a.cfg.Global.ProxyMode == proxyModeOff {
		return
	}
	_ = applySystemProxy(proxyActionClear, "")
}
