//go:build !windows && !darwin

package main

// The Linux implementation drives gsettings, which covers the GNOME
// desktop. Other desktops are left alone.

import (
	"net"
	"os/exec"
)

// applyLinuxProxy sets the GNOME proxy for the current user.
func applySystemProxyPlatform(action, addr string) error {
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
		runQuiet("gsettings", "set", "org.gnome.system.proxy", "ignore-hosts", "['localhost', '127.0.0.0/8', '::1']")
	case proxyModePAC:
		pacURL, err := pacFileURL(addr)
		if err != nil {
			return err
		}
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
