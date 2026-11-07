//go:build windows

package main

import (
	"strings"
	"testing"
)

// On Windows the copied commands are PowerShell assignments.
func TestEnvProxyCommandsWindows(t *testing.T) {
	got := envProxyCommands("127.0.0.1:12345")
	for _, want := range []string{
		"$env:HTTP_PROXY='http://127.0.0.1:12345'",
		"$env:HTTPS_PROXY='http://127.0.0.1:12345'",
		"$env:ALL_PROXY='socks5://127.0.0.1:12345'",
		"$env:NO_PROXY='localhost,127.0.0.1,::1'",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("commands missing %q: %s", want, got)
		}
	}
	if strings.Contains(got, "export ") {
		t.Error("Windows commands should not be shell exports")
	}
}
