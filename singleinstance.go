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

// instanceLockPath names the lock in the user's application data
// directory, not at the top of their home. Tests point SUNRAY_INSTANCE_LOCK
// at a private path so they never contend with the running app.
//
// The lock is not the config file itself: saves replace it by rename, which
// swaps the inode, so a lock held on it would be on a file no longer at that
// path and a second instance could lock the new one. A file of its own never
// replaced keeps the lock meaningful for the life of the process.
func instanceLockPath() string {
	if p := os.Getenv("SUNRAY_INSTANCE_LOCK"); p != "" {
		return p
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return "sunray.lock"
	}
	return filepath.Join(dir, "sun-ray", "instance.lock")
}

// instanceLockFile stays open for the life of the process: closing it, or
// the process exiting, is what releases the lock.
var instanceLockFile *os.File

// acquireInstanceLock reports whether this is the only instance. It takes a
// non-blocking exclusive lock on a fixed file. If the lock cannot be taken
// at all (unwritable directory), it lets the app start rather than refusing
// to run.
func acquireInstanceLock() bool {
	path := instanceLockPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return true
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
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
