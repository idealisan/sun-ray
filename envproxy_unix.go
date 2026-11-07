//go:build !windows

package main

// On macOS and Linux there is no way to set the environment of the
// user's shells, so the app hands back the commands to run and copies
// them to the clipboard.

import "fmt"

// envProxyCommands returns the shell commands that set the proxy
// environment variables in the current shell. The copy button offers
// these on every platform; Windows has its own PowerShell variant.
func envProxyCommands(addr string) string {
	return fmt.Sprintf("export HTTP_PROXY=http://%s\nexport HTTPS_PROXY=http://%s\nexport ALL_PROXY=socks5://%s\nexport NO_PROXY=localhost,127.0.0.1,::1\nexport http_proxy=$HTTP_PROXY\nexport https_proxy=$HTTPS_PROXY\nexport all_proxy=$ALL_PROXY\nexport no_proxy=$NO_PROXY", addr, addr, addr)
}

// applyEnvProxy returns the shell commands that set or clear the proxy
// environment variables.
func applyEnvProxy(enable bool, addr string) (string, error) {
	if !enable {
		return "unset HTTP_PROXY HTTPS_PROXY ALL_PROXY http_proxy https_proxy all_proxy NO_PROXY no_proxy", nil
	}
	return envProxyCommands(addr), nil
}
