package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestTunHelperConfig(t *testing.T) {
	g := defaultGlobalConfig()
	g.TunEnabled = true
	g.TunName = "utun-test"
	g.TunMTU = 1400
	g.TunGateway = "172.30.0.1/30"

	data, err := tunHelperConfig(&g, "10808", "utun-test", "127.0.0.1:1234", "token")
	if err != nil {
		t.Fatal(err)
	}
	var cfg struct {
		Inbounds  []map[string]any `json:"inbounds"`
		Outbounds []map[string]any `json:"outbounds"`
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatal(err)
	}
	if len(cfg.Inbounds) != 1 || cfg.Inbounds[0]["protocol"] != "tun" {
		t.Fatalf("inbounds = %v, want one TUN inbound", cfg.Inbounds)
	}
	settings, _ := cfg.Inbounds[0]["settings"].(map[string]any)
	if settings["name"] != "utun-test" {
		t.Fatalf("TUN name = %v, want utun-test", settings["name"])
	}
	if settings["autoOutboundsInterface"] != "auto" {
		t.Fatalf("autoOutboundsInterface = %v, want auto", settings["autoOutboundsInterface"])
	}
	if len(cfg.Outbounds) != 1 || cfg.Outbounds[0]["protocol"] != "socks" {
		t.Fatalf("outbounds = %v, want one SOCKS outbound", cfg.Outbounds)
	}
	servers, _ := cfg.Outbounds[0]["settings"].(map[string]any)["servers"].([]any)
	if len(servers) != 1 {
		t.Fatalf("SOCKS servers = %v", servers)
	}
	server, _ := servers[0].(map[string]any)
	if server["address"] != "127.0.0.1" || server["port"] != float64(10808) {
		t.Fatalf("SOCKS server = %v, want 127.0.0.1:10808", server)
	}
	if strings.Contains(string(data), `"tag": "proxy"`) {
		t.Fatal("the helper config contains the main proxy outbound")
	}
}

func TestTunHelperConfigInvalidPort(t *testing.T) {
	g := defaultGlobalConfig()
	if _, err := tunHelperConfig(&g, "not-a-port", "utun10", "127.0.0.1:1", "token"); err == nil {
		t.Fatal("tunHelperConfig accepted an invalid port")
	}
}

func TestTunSettingsReusesCurrentName(t *testing.T) {
	g := defaultGlobalConfig()
	a := tunSettingsFor(&g, "en0", "utun42")
	if a.TunName != "utun42" {
		t.Fatalf("TunName = %q, want utun42", a.TunName)
	}
	if !sameTun(a, tunSettingsFor(&g, "en0", "utun42")) {
		t.Fatal("the same settings should match")
	}
	b := a
	b.MTU++
	if sameTun(a, b) {
		t.Fatal("an MTU change should not match")
	}
	g.TunName = "utun99"
	if got := tunSettingsFor(&g, "en0", "utun42").TunName; got != "utun99" {
		t.Fatalf("explicit name = %q, want utun99", got)
	}
}
