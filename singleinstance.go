package main

// singleinstance.go keeps only one copy of the app running, whatever
// started it: the installed .app, a development `go run`, or the headless
// build. They all read and write the same ~/sunray.json and listen on the
// same ports, so two of them overwrite each other's state.
//
// The lock lives at a fixed path for the user, so every build contends for
// the same lock. mygo's own single-instance lock keys on the bundle
// identifier, which only a packaged macOS .app has, so it would let a
// development build run beside the installed one.

import (
	"os"
	"path/filepath"
)

// instanceLockPath is fixed for the user. Tests point SUNRAY_INSTANCE_LOCK
// at a private path so they never contend with the running app.
func instanceLockPath() string {
	if p := os.Getenv("SUNRAY_INSTANCE_LOCK"); p != "" {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "sunray.lock"
	}
	return filepath.Join(home, "sunray.lock")
}

// instanceLockFile stays open for the life of the process: closing it, or
// the process exiting, is what releases the lock.
var instanceLockFile *os.File

// acquireInstanceLock reports whether this is the only instance. It takes a
// non-blocking exclusive lock on a fixed file. If the lock cannot be taken
// at all (unwritable directory), it lets the app start rather than refusing
// to run.
func acquireInstanceLock() bool {
	f, err := os.OpenFile(instanceLockPath(), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return true
	}
	if err := lockInstanceFile(f); err != nil {
		_ = f.Close()
		return false
	}
	instanceLockFile = f
	return true
}
