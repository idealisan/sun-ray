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
