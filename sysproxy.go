package main

import ()

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
		shExpMatch(host, "*.lan") ||
		shExpMatch(host, "*.localdomain") ||
		shExpMatch(host, "*.internal") ||
		shExpMatch(host, "*.home.arpa") ||
		shExpMatch(host, "*.test") ||
		shExpMatch(host, "*.example") ||
		shExpMatch(host, "*.invalid") ||
		host == "localhost" ||
		host == "127.0.0.1" ||
		host == "::1" ||
		isInNet(host, "0.0.0.0", "255.0.0.0") ||
		isInNet(host, "10.0.0.0", "255.0.0.0") ||
		isInNet(host, "100.64.0.0", "255.192.0.0") ||
		isInNet(host, "127.0.0.0", "255.0.0.0") ||
		isInNet(host, "169.254.0.0", "255.255.0.0") ||
		isInNet(host, "172.16.0.0", "255.240.0.0") ||
		isInNet(host, "192.0.0.0", "255.255.255.0") ||
		isInNet(host, "192.0.2.0", "255.255.255.0") ||
		isInNet(host, "192.168.0.0", "255.255.0.0") ||
		isInNet(host, "198.18.0.0", "255.254.0.0") ||
		isInNet(host, "198.51.100.0", "255.255.255.0") ||
		isInNet(host, "203.0.113.0", "255.255.255.0") ||
		isInNet(host, "224.0.0.0", "240.0.0.0") ||
		isInNet(host, "240.0.0.0", "240.0.0.0")) {
		return "DIRECT";
	}
	return "PROXY %s; DIRECT;";
}
`

var localBypassDomains = []string{
	"localhost", "127.0.0.1", "::1", "*.local", "*.lan", "*.localdomain",
	"*.internal", "*.home.arpa", "*.test", "*.example", "*.invalid",
}

var localBypassCIDRs = []string{
	"0.0.0.0/8", "10.0.0.0/8", "100.64.0.0/10", "127.0.0.0/8",
	"169.254.0.0/16", "172.16.0.0/12", "192.0.0.0/24", "192.0.2.0/24",
	"192.168.0.0/16", "198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24",
	"224.0.0.0/4", "240.0.0.0/4", "::1/128", "fc00::/7", "fe80::/10", "ff00::/8",
}

func macProxyBypass() []string {
	bypass := append([]string{}, localBypassDomains...)
	return append(bypass, localBypassCIDRs...)
}

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
