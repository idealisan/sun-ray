package main

// The Windows implementation changes the WinINET settings of the current
// user and tells WinINET about it, so the change takes effect at once.

import (
	"fmt"
	"strings"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

const internetSettingsKey = `Software\Microsoft\Windows\CurrentVersion\Internet Settings`

// windowsProxyBypass expands the private ranges into the wildcard patterns
// WinINET understands. Address ranges that do not fall on an octet boundary
// are expanded one second octet at a time.
func windowsProxyBypass() string {
	parts := []string{
		"localhost", "<local>", "*.local", "*.lan", "*.localdomain",
		"*.internal", "*.home.arpa", "*.test", "*.example", "*.invalid",
		"0.*", "10.*", "127.*", "169.254.*",
		"192.0.0.*", "192.0.2.*", "192.168.*",
		"198.18.*", "198.19.*", "198.51.100.*", "203.0.113.*",
	}
	for i := 64; i <= 127; i++ {
		parts = append(parts, fmt.Sprintf("100.%d.*", i))
	}
	for i := 16; i <= 31; i++ {
		parts = append(parts, fmt.Sprintf("172.%d.*", i))
	}
	for i := 224; i <= 255; i++ {
		parts = append(parts, fmt.Sprintf("%d.*", i))
	}
	return strings.Join(parts, ";")
}

// applyWindowsProxy points the system at the local proxy or at the PAC
// file.
func applySystemProxyPlatform(action, addr, pacURL string) error {
	k, _, err := registry.CreateKey(registry.CURRENT_USER, internetSettingsKey, registry.SET_VALUE|registry.QUERY_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	switch action {
	case proxyModeManual:
		k.SetStringValue("ProxyServer", addr)
		k.SetStringValue("ProxyOverride", windowsProxyBypass())
		k.SetDWordValue("ProxyEnable", 1)
		// A PAC file would win over the manual settings.
		k.DeleteValue("AutoConfigURL")
	case proxyModePAC:
		k.SetStringValue("AutoConfigURL", pacURL)
		k.SetDWordValue("ProxyEnable", 0)
	default:
		return nil
	}
	notifyWinINET()
	return nil
}

// clearSystemProxy removes the proxy settings of the current user.
func clearSystemProxy() error {
	k, _, err := registry.CreateKey(registry.CURRENT_USER, internetSettingsKey, registry.SET_VALUE|registry.QUERY_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	k.SetDWordValue("ProxyEnable", 0)
	k.DeleteValue("ProxyServer")
	k.DeleteValue("ProxyOverride")
	k.DeleteValue("AutoConfigURL")
	k.DeleteValue("AutoDetect")
	notifyWinINET()
	return nil
}

// notifyWinINET tells WinINET that the settings changed, so they take
// effect without signing out or restarting the browser.
func notifyWinINET() {
	// wininet.dll exports the wide entry point; looking up the name
	// without the suffix fails, and calling a missing procedure panics.
	proc := windows.NewLazySystemDLL("wininet.dll").NewProc("InternetSetOptionW")
	if err := proc.Find(); err != nil {
		return
	}
	const (
		settingsChanged = 39
		refresh         = 37
	)
	proc.Call(0, settingsChanged, 0, 0)
	proc.Call(0, refresh, 0, 0)
}
