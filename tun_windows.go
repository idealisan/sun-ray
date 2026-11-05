//go:build windows

package main

import "golang.org/x/sys/windows"

// isElevated reports whether the process runs with an elevated token, which
// creating a Wintun interface needs.
func isElevated() bool {
	tok := windows.GetCurrentProcessToken()
	return tok.IsElevated()
}
