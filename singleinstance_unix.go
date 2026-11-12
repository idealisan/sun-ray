//go:build !windows

package main

import (
	"os"
	"syscall"
)

// lockInstanceFile takes an exclusive, non-blocking flock. The kernel
// releases it when the process exits, including a crash, so there is no
// stale lock to clean up.
func lockInstanceFile(f *os.File) error {
	return syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
}
