package main

// persistence.go saves the config to sunray.json in the user home
// directory. It is loaded once at startup and auto-saved (debounced)
// whenever the UI state changes.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

const saveDebounce = 2 * time.Second

// configPath returns <home>/sunray.json.
func configPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "sunray.json" // fall back to the working directory
	}
	return filepath.Join(home, "sunray.json")
}

// loadConfig reads the saved config; missing or broken files fall back to
// the defaults so the app always starts.
func loadConfig() guiConfig {
	cfg := defaultConfig()
	data, err := os.ReadFile(configPath())
	if err != nil {
		return cfg
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return defaultConfig()
	}
	return cfg
}

// saveConfig writes the config atomically (temp file + rename).
func saveConfig(cfg guiConfig) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	path := configPath()
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// configSaver tracks the last persisted state and debounces writes.
type configSaver struct {
	lastJSON string
	lastSave time.Time
}

// maybeSave persists the config when it changed, at most once per debounce
// interval. Runs on the UI thread (called from view and start/stop).
func (s *configSaver) maybeSave(cfg guiConfig, force bool) {
	data, err := json.Marshal(cfg)
	if err != nil {
		return
	}
	if string(data) == s.lastJSON {
		return
	}
	if !force && time.Since(s.lastSave) < saveDebounce {
		return
	}
	if err := saveConfig(cfg); err != nil {
		return // file errors are not fatal; keep the app running
	}
	s.lastJSON = string(data)
	s.lastSave = time.Now()
}
