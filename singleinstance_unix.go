//go:build !windows

package main

import (
	"os"
	"strconv"
	"syscall"
)

// instanceLockFileName carries the user id: /tmp is shared between accounts
// on Linux, so two users running the app must not lock each other out.
func instanceLockFileName() string {
	return "sunray-" + strconv.Itoa(os.Getuid()) + ".lock"
}

// lockInstanceFile takes an exclusive, non-blocking flock. The kernel
// releases it when the process exits, including a crash, so there is no
// stale lock to clean up.
func lockInstanceFile(f *os.File) error {
	return syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
}
