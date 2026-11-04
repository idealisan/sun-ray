package main

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

// The view runs headless in tests; clicks behave like user input.
func TestView(t *testing.T) {
	a := &app{cfg: defaultConfig()}
	tt := ui.NewTester(a.view, 780, 580)
	for _, want := range []string{"Sun Ray", "入站（本机代理）", "出站（服务器）", "已停止", "启动", "协议", "地址"} {
		if !tt.HasText(want) {
			t.Errorf("view missing text %q; texts: %v", want, tt.Texts())
		}
	}
}

func TestBuildJSONVLESS(t *testing.T) {
	c := defaultConfig()
	c.Address, c.Secret, c.UseTLS, c.SNI = "s.example.com", "11111111-2222-3333-4444-555555555555", true, "cdn.example.com"
	data, err := c.buildJSON()
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	for _, want := range []string{`"protocol": "vless"`, `"id": "1111`, `"serverName": "cdn.example.com"`, `"port": 10808`, `"port": 10809`, `"stats"`} {
		if !strings.Contains(s, want) {
			t.Errorf("config missing %s; got %s", want, s)
		}
	}
}

func TestBuildJSONErrors(t *testing.T) {
	c := defaultConfig()
	if _, err := c.buildJSON(); err == nil {
		t.Error("want error for empty server address")
	}
	c.Address, c.Port, c.Secret = "s.example.com", "0", "uuid"
	if _, err := c.buildJSON(); err == nil {
		t.Error("want error for invalid server port")
	}
	c.Port = "443"
	if _, err := c.buildJSON(); err != nil {
		t.Errorf("want ok config, got %v", err)
	}
}

// Smoke test: boot and close the real in-process core, function calls only.
func TestCoreStartStop(t *testing.T) {
	c := defaultConfig()
	c.Protocol = "freedom" // no server needed
	c.SocksPort, c.HTTPPort = "39441", "39442"
	data, err := c.buildJSON()
	if err != nil {
		t.Fatal(err)
	}
	var a app
	if err := a.core.Start(data, func(string) {}); err != nil {
		t.Fatal(err)
	}
	if !a.core.Running() {
		t.Fatal("core should be running")
	}
	if err := a.core.Stop(); err != nil {
		t.Fatal(err)
	}
	if a.core.Running() {
		t.Fatal("core should be stopped")
	}
}