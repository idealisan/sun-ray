package main

// testmain_test.go points every test at a temporary config file. A test that
// edits the config (a click on a checkbox, a delete) would otherwise save it
// through configPath(), replacing the user's ~/sunray.json with the test's
// fixture. saveConfig also refuses the real path in a test, as a backstop.

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "sunray-test-*")
	if err != nil {
		panic(err)
	}
	configFileOverride = filepath.Join(dir, "sunray.json")
	// The single-instance lock is its own file; keep tests off the user's too.
	// A child process the lock test starts passes its own path, which this
	// must not overwrite.
	if os.Getenv("SUNRAY_INSTANCE_LOCK") == "" {
		os.Setenv("SUNRAY_INSTANCE_LOCK", filepath.Join(dir, "sunray.lock"))
	}
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}
