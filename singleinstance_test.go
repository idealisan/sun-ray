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

// The lock lives in the app data directory, not at the top of the user's
// home, and its directory is created on demand.
func TestInstanceLockPathIsInAppData(t *testing.T) {
	t.Setenv("SUNRAY_INSTANCE_LOCK", "")
	dir, err := os.UserConfigDir()
	if err != nil {
		t.Skipf("no user config dir: %v", err)
	}
	want := filepath.Join(dir, "sun-ray", "instance.lock")
	if got := instanceLockPath(); got != want {
		t.Fatalf("instanceLockPath = %q, want %q", got, want)
	}
}

// acquireInstanceLock creates the lock directory when it is missing.
func TestAcquireLockCreatesDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "deeper")
	path := filepath.Join(dir, "instance.lock")
	t.Setenv("SUNRAY_INSTANCE_LOCK", path)
	if !acquireInstanceLock() {
		t.Fatal("the lock should be taken in a fresh directory")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("the lock file was not created: %v", err)
	}
	_ = instanceLockFile.Close()
	instanceLockFile = nil
}
