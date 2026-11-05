package main

// The Windows implementation changes the WinINET settings of the current
// user and tells WinINET about it, so the change takes effect at once.

import (
	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

const internetSettingsKey = `Software\Microsoft\Windows\CurrentVersion\Internet Settings`

// applyWindowsProxy points the system at the local proxy or at the PAC
// file.
func applySystemProxyPlatform(action, addr string) error {
	k, _, err := registry.CreateKey(registry.CURRENT_USER, internetSettingsKey, registry.SET_VALUE|registry.QUERY_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	switch action {
	case proxyModeManual:
		k.SetStringValue("ProxyServer", addr)
		k.SetStringValue("ProxyOverride", "localhost;127.*;<local>")
		k.SetDWordValue("ProxyEnable", 1)
		// A PAC file would win over the manual settings.
		k.DeleteValue("AutoConfigURL")
	case proxyModePAC:
		pacURL, err := pacFileURL(addr)
		if err != nil {
			return err
		}
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
	proc := windows.NewLazySystemDLL("wininet.dll").NewProc("InternetSetOption")
	const (
		settingsChanged = 39
		refresh         = 37
	)
	proc.Call(0, settingsChanged, 0, 0)
	proc.Call(0, refresh, 0, 0)
}
