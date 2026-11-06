package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfigFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sunray.json")
	data := `{"global":{"listenPort":"10808"},"servers":[{"id":"a","name":"n","protocol":"freedom"}],"subscriptions":[{"name":"sub","url":"https://example.com/sub"}],"activeId":"a"}`
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := loadConfigFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Global.ListenPort != "10808" || len(cfg.Servers) != 1 || cfg.ActiveID != "a" {
		t.Fatalf("loaded config = %+v", cfg)
	}
	if cfg.Results == nil {
		t.Fatal("Results should be initialized")
	}
	if cfg.Global.TunMTU != defaultTunMTU {
		t.Fatalf("zero TUN MTU should load as %d, got %v", defaultTunMTU, cfg.Global.TunMTU)
	}
	if len(cfg.Subscriptions) != 1 || cfg.Subscriptions[0].ID == "" {
		t.Fatalf("subscription should load with an id: %+v", cfg.Subscriptions)
	}
}

func TestLoadConfigFileEmptyServerList(t *testing.T) {
	path := filepath.Join(t.TempDir(), "empty.json")
	if err := os.WriteFile(path, []byte(`{"servers":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := loadConfigFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Servers) != 0 {
		t.Fatalf("loaded %d servers, want 0", len(cfg.Servers))
	}
}

func TestLoadConfigFileErrors(t *testing.T) {
	dir := t.TempDir()
	for name, data := range map[string]string{
		"missing":    "",
		"broken":     "{",
		"no-servers": `{"global":{}}`,
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(dir, name+".json")
			if data != "" {
				if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := loadConfigFile(path); err == nil {
				t.Fatal("loadConfigFile should fail")
			}
			if got := loadConfigFrom(path); got.Global.ListenPort != defaultAppConfig().Global.ListenPort {
				t.Fatalf("loadConfigFrom did not fall back to defaults: %+v", got.Global)
			}
		})
	}
}

func TestConfigPathOverride(t *testing.T) {
	old := configFileOverride
	defer func() { configFileOverride = old }()
	path := filepath.Join(t.TempDir(), "dev.json")
	configFileOverride = path
	if got := configPath(); got != path {
		t.Fatalf("configPath = %q, want %q", got, path)
	}
}

func TestConfigArg(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"-config", "/tmp/dev.json"}, "/tmp/dev.json"},
		{[]string{"--config", "/tmp/dev.json"}, "/tmp/dev.json"},
		{[]string{"-config=/tmp/dev.json"}, "/tmp/dev.json"},
		{[]string{"--config=/tmp/dev.json"}, "/tmp/dev.json"},
		{[]string{"-psn_0_123", "-config", "/tmp/dev.json"}, "/tmp/dev.json"},
		{[]string{"-config"}, ""},
		{[]string{"-version"}, ""},
	} {
		if got := configArg(tc.args); got != tc.want {
			t.Errorf("configArg(%v) = %q, want %q", tc.args, got, tc.want)
		}
	}
}
