//go:build !windows && !darwin

package main

// The Linux implementation drives gsettings, which covers the GNOME
// desktop. Other desktops are left alone.

import (
	"net"
	"os/exec"
	"strings"
)

// systemProxyIsManual and systemProxyIsPAC are only checked on macOS for
// now; elsewhere the settings the app applied are trusted until the next
// change.
func systemProxyIsManual(host, port string) bool { return true }
func systemProxyIsPAC(url string) bool           { return true }

func gnomeIgnoreHosts() string {
	hosts := append([]string{}, localBypassDomains...)
	hosts = append(hosts, localBypassCIDRs...)
	quoted := make([]string, 0, len(hosts))
	for _, host := range hosts {
		quoted = append(quoted, "'"+host+"'")
	}
	return "[" + strings.Join(quoted, ", ") + "]"
}

// applyLinuxProxy sets the GNOME proxy for the current user.
func applySystemProxyPlatform(action, addr, pacURL string) error {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return err
	}
	switch action {
	case proxyModeManual:
		runQuiet("gsettings", "set", "org.gnome.system.proxy", "mode", "manual")
		runQuiet("gsettings", "set", "org.gnome.system.proxy.http", "host", host)
		runQuiet("gsettings", "set", "org.gnome.system.proxy.http", "port", port)
		runQuiet("gsettings", "set", "org.gnome.system.proxy.https", "host", host)
		runQuiet("gsettings", "set", "org.gnome.system.proxy.https", "port", port)
		runQuiet("gsettings", "set", "org.gnome.system.proxy", "ignore-hosts", gnomeIgnoreHosts())
	case proxyModePAC:
		runQuiet("gsettings", "set", "org.gnome.system.proxy", "mode", "auto")
		runQuiet("gsettings", "set", "org.gnome.system.proxy.autoconfig", "url", pacURL)
	}
	return nil
}

// clearSystemProxy turns the GNOME proxy off.
func clearSystemProxy() error {
	runQuiet("gsettings", "set", "org.gnome.system.proxy", "mode", "none")
	return nil
}

// runQuiet runs a command and ignores what it says.
func runQuiet(name string, args ...string) {
	_ = exec.Command(name, args...).Run()
}
