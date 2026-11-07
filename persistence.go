package main

// persistence.go saves the config to sunray.json in the user home
// directory. It is loaded once at startup and auto-saved (debounced)
// whenever the UI state changes.

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const saveDebounce = 2 * time.Second

// configFileOverride is set by -config at startup. Keeping it separate from
// the default path lets development runs use their own file and leaves the
// user's ~/sunray.json untouched.
var configFileOverride string

// configPath returns the -config path when given, otherwise <home>/sunray.json.
func configPath() string {
	if configFileOverride != "" {
		return configFileOverride
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "sunray.json" // fall back to the working directory
	}
	return filepath.Join(home, "sunray.json")
}

// loadConfig reads the saved config. A missing file is not an error: the app
// starts with the defaults. A file that exists but cannot be read or parsed
// is an error, and the caller must not write over it.
func loadConfig() (appConfig, error) { return loadConfigFrom(configPath()) }

// loadConfigFrom reads a config file. A missing file yields the defaults and
// no error; a broken one yields the error, so the caller can ask the user
// what to do instead of silently replacing what is on disk.
func loadConfigFrom(path string) (appConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return defaultAppConfig(), nil
		}
		return appConfig{}, err
	}
	return parseConfig(data, path)
}

// parseConfig turns the bytes of a config file into a config. A file that
// does not parse, or that names no servers, is broken: the caller must not
// write over it.
func parseConfig(data []byte, path string) (appConfig, error) {
	var saved appConfig
	if err := json.Unmarshal(data, &saved); err != nil {
		return appConfig{}, fmt.Errorf("%s: %w", path, err)
	}
	// A missing "servers" key means the file is incomplete; an explicit
	// empty list is valid, so the user can delete every server.
	if saved.Servers == nil {
		return appConfig{}, fmt.Errorf("%s: config has no servers", path)
	}
	for i := range saved.Servers {
		if saved.Servers[i].ID == "" {
			saved.Servers[i].ID = newServerID()
		}
	}
	for i := range saved.Subscriptions {
		if saved.Subscriptions[i].ID == "" {
			saved.Subscriptions[i].ID = newServerID()
		}
	}
	if saved.Subscriptions == nil {
		saved.Subscriptions = []subscriptionConfig{}
	}
	if saved.Results == nil {
		saved.Results = map[string]serverResult{}
	}
	if saved.Global.TunMTU <= 0 {
		saved.Global.TunMTU = defaultTunMTU
	}
	if saved.Global.DNSPreset == "" {
		saved.Global.DNSPreset = dnsPresetSystem
	}
	return saved, nil
}

// loadConfigFile is parseConfig with the file read, for the headless build
// and the tests.
func loadConfigFile(path string) (appConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return appConfig{}, err
	}
	return parseConfig(data, path)
}

// saveConfig writes the config atomically (temp file + rename).
func saveConfig(cfg appConfig) error {
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
	// blocked stops every write. It is set while the config on disk
	// failed to load, so nothing can replace that file before the user
	// says what to do with it.
	blocked bool
	// onError is told when a write failed, so the user can be warned.
	onError func(error)
}

// remember records cfg as already persisted, so the first frame does not
// rewrite a file that was just loaded.
func (s *configSaver) remember(cfg appConfig) {
	if data, err := json.Marshal(cfg); err == nil {
		s.lastJSON = string(data)
	}
}

// maybeSave persists the config when it changed, at most once per debounce
// interval. Runs on the UI thread (called from view and start/stop). A
// failed write is reported through onError and leaves the file as it was.
func (s *configSaver) maybeSave(cfg appConfig, force bool) {
	if s.blocked {
		return
	}
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
		if s.onError != nil {
			s.onError(err)
		}
		return // file errors are not fatal; keep the app running
	}
	s.lastJSON = string(data)
	s.lastSave = time.Now()
}

// configArg returns the path after -config/--config (or -config=path).
// Unknown arguments are ignored so the macOS launcher's -psn_* argument and
// other framework flags do not stop parsing.
func configArg(args []string) string {
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "-config" || arg == "--config":
			if i+1 < len(args) {
				return args[i+1]
			}
			return ""
		case strings.HasPrefix(arg, "-config="):
			return strings.TrimPrefix(arg, "-config=")
		case strings.HasPrefix(arg, "--config="):
			return strings.TrimPrefix(arg, "--config=")
		}
	}
	return ""
}
