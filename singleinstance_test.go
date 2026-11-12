package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// execCommand starts this test binary again as a separate process.
func execCommand() *exec.Cmd {
	exe, err := os.Executable()
	if err != nil {
		panic(err)
	}
	return exec.Command(exe, "-test.run=TestInstanceLockExcludesSecondProcess")
}

// Two processes cannot both hold the lock. The child is this test binary
// again, started with a helper flag, so it really is a separate process.
func TestInstanceLockExcludesSecondProcess(t *testing.T) {
	if os.Getenv("SUNRAY_TEST_LOCK_CHILD") == "1" {
		if acquireInstanceLock() {
			os.Exit(2) // must NOT acquire
		}
		os.Exit(0)
	}
	dir := t.TempDir()
	t.Setenv("SUNRAY_INSTANCE_LOCK", filepath.Join(dir, "lock"))
	if !acquireInstanceLock() {
		t.Fatal("first process failed to take the lock")
	}
	cmd := execCommand()
	cmd.Env = append(os.Environ(), "SUNRAY_TEST_LOCK_CHILD=1",
		"SUNRAY_INSTANCE_LOCK="+filepath.Join(dir, "lock"))
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("second process should exit cleanly but got %v: %s", err, out)
	}
}

// A stale lock from a dead process must not block the next start.
func TestInstanceLockReleasedAfterExit(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SUNRAY_INSTANCE_LOCK", filepath.Join(dir, "lock"))
	if !acquireInstanceLock() {
		t.Fatal("first process failed to take the lock")
	}
	// Closing the file releases the lock, as a process exit would.
	if err := instanceLockFile.Close(); err != nil {
		t.Fatal(err)
	}
	instanceLockFile = nil
	if !acquireInstanceLock() {
		t.Fatal("a released lock should be available again")
	}
	_ = instanceLockFile.Close()
	instanceLockFile = nil
}

// The lock path is fixed for the user unless a test overrides it.
func TestInstanceLockPathOverride(t *testing.T) {
	t.Setenv("SUNRAY_INSTANCE_LOCK", "private-sunray.lock")
	if got := instanceLockPath(); got != "private-sunray.lock" {
		t.Fatalf("override ignored: %q", got)
	}
}
