package main

// The Windows implementation writes the proxy environment variables of
// the current user, so new processes (terminals, tools) pick them up.

import (
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

// envProxyCommands returns the PowerShell commands that set the proxy
// environment variables in the current session. The copy button offers
// these on every platform; Unix has its own shell variant.
func envProxyCommands(addr string) string {
	return fmt.Sprintf("$env:HTTP_PROXY='http://%s'\n$env:HTTPS_PROXY='http://%s'\n$env:ALL_PROXY='socks5://%s'\n$env:NO_PROXY='localhost,127.0.0.1,::1'", addr, addr, addr)
}

// applyEnvProxy sets or removes the proxy environment variables. It
// returns the shell commands to copy, which Windows does not need.
func applyEnvProxy(enable bool, addr string) (string, error) {
	k, _, err := registry.CreateKey(registry.CURRENT_USER, `Environment`, registry.SET_VALUE|registry.QUERY_VALUE)
	if err != nil {
		return "", err
	}
	defer k.Close()
	values := map[string]string{
		"HTTP_PROXY":  "http://" + addr,
		"HTTPS_PROXY": "http://" + addr,
		"ALL_PROXY":   "socks5://" + addr,
		"http_proxy":  "http://" + addr,
		"https_proxy": "http://" + addr,
		"all_proxy":   "socks5://" + addr,
		"NO_PROXY":    "localhost,127.0.0.1,::1",
		"no_proxy":    "localhost,127.0.0.1,::1",
	}
	for name, value := range values {
		if enable {
			if err := k.SetStringValue(name, value); err != nil {
				return "", err
			}
		} else {
			_ = k.DeleteValue(name)
		}
	}
	notifyEnvironment()
	return "", nil
}

// notifyEnvironment tells Windows that the environment changed, so
// programs started from now on see the new variables.
func notifyEnvironment() {
	proc := windows.NewLazySystemDLL("user32.dll").NewProc("SendMessageTimeoutW")
	if err := proc.Find(); err != nil {
		return
	}
	name, err := windows.UTF16PtrFromString("Environment")
	if err != nil {
		return
	}
	const (
		wmSettingChange = 0x1A
		smtoAbortIfHung = 0x2
	)
	proc.Call(uintptr(0xffff), wmSettingChange, 0, uintptr(unsafe.Pointer(name)), smtoAbortIfHung, 1000, 0)
}
