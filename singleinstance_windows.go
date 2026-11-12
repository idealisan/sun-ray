//go:build windows

package main

import (
	"os"
	"unsafe"

	"golang.org/x/sys/windows"
)

var procLockFileEx = windows.NewLazySystemDLL("kernel32.dll").NewProc("LockFileEx")

// instanceLockFileName needs no user id: Windows gives each account its own
// temporary directory.
func instanceLockFileName() string { return "sunray.lock" }

// lockInstanceFile takes an exclusive, non-blocking byte-range lock. Like
// flock, Windows releases it when the process exits.
func lockInstanceFile(f *os.File) error {
	const failImmediately, exclusive = 1, 2
	var ol windows.Overlapped
	r, _, err := procLockFileEx.Call(f.Fd(), failImmediately|exclusive, 0, 1, 0, uintptr(unsafe.Pointer(&ol)))
	if r == 0 {
		return err
	}
	return nil
}
