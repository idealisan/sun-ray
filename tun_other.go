//go:build !windows

package main

// isElevated reports whether the process runs with an elevated token. Only
// Windows needs it; the other systems check their own rights.
func isElevated() bool { return true }
