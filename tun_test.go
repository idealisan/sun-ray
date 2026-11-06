package main

import (
	"encoding/json"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

// The TUN inbound must appear in the config only when it is turned on,
// with the settings the user gave.
func TestDefaultTunMTU(t *testing.T) {
	if got := defaultGlobalConfig().TunMTU; got != defaultTunMTU {
		t.Fatalf("default TUN MTU = %v, want %d", got, defaultTunMTU)
	}
}

func TestParseTunMTU(t *testing.T) {
	for _, tc := range []struct {
		raw  string
		want float64
		ok   bool
	}{
		{"1500", 1500, true},
		{" 1420 ", 1420, true},
		{"576", 576, true},
		{"9000", 9000, true},
		{"575", 0, false},
		{"9001", 0, false},
		{"", 0, false},
		{"abc", 0, false},
	} {
		got, ok := parseTunMTU(tc.raw)
		if got != tc.want || ok != tc.ok {
			t.Errorf("parseTunMTU(%q) = %v,%v, want %v,%v", tc.raw, got, ok, tc.want, tc.ok)
		}
	}
}

func TestTunInbound(t *testing.T) {
	c := defaultServerConfig()
	c.Protocol = "freedom"
	c.Address, c.Port = "127.0.0.1", "1"

	g := defaultGlobalConfig()
	data, err := c.buildJSON(&g)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), `"protocol": "tun"`) {
		t.Error("the TUN inbound is there although it is off")
	}

	g.TunEnabled = true
	g.TunName = "sunray-test"
	g.TunGateway = "172.30.0.1/30"
	g.TunMTU = 1400
	data, err = c.buildJSON(&g)
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	for _, want := range []string{
		`"protocol": "tun"`,
		`"name": "sunray-test"`,
		`"mtu": 1400`,
		`"172.30.0.1/30"`,
		`"0.0.0.0/0"`,
		`"autoOutboundsInterface": "auto"`,
	} {
		if !strings.Contains(s, want) {
			t.Errorf("the config misses %s; got %s", want, s)
		}
	}
	if strings.Contains(s, `"::/0"`) {
		t.Error("the IPv6 route is there although it is off")
	}

	g.TunIPv6 = true
	data, err = c.buildJSON(&g)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"::/0"`) {
		t.Error("the IPv6 route is missing")
	}
}

// In helper mode the privileged helper owns the TUN, so the in-process
// core has no TUN inbound and pins its outbounds to the physical NIC.
func TestBuildJSONTunHelper(t *testing.T) {
	c := defaultServerConfig()
	c.Protocol = "freedom"
	c.Address, c.Port = "example.com", "443"

	g := defaultGlobalConfig()
	g.TunEnabled = true
	g.TunHelper = true
	g.TunBypassInterface = "en0"

	data, err := c.buildJSON(&g)
	if err != nil {
		t.Fatal(err)
	}
	var cfg struct {
		Inbounds  []map[string]any `json:"inbounds"`
		Outbounds []map[string]any `json:"outbounds"`
		Routing   struct {
			Rules []map[string]any `json:"rules"`
		} `json:"routing"`
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatal(err)
	}

	for _, in := range cfg.Inbounds {
		if in["protocol"] == "tun" {
			t.Fatal("the helper mode still has a TUN inbound")
		}
	}
	if got := sockoptInterface(cfg.Outbounds[0]); got != "en0" {
		t.Fatalf("proxy interface = %q, want en0", got)
	}
	if got := sockoptInterface(cfg.Outbounds[1]); got != "en0" {
		t.Fatalf("direct interface = %q, want en0", got)
	}
	foundLo := false
	for _, o := range cfg.Outbounds {
		if o["tag"] == "direct-lo" {
			foundLo = true
			if got := sockoptInterface(o); got != "" {
				t.Fatalf("direct-lo interface = %q, want none", got)
			}
		}
	}
	if !foundLo {
		t.Fatal("direct-lo outbound is missing")
	}
	if len(cfg.Routing.Rules) < 2 {
		t.Fatalf("routing rules = %d, want at least 2", len(cfg.Routing.Rules))
	}
	if cfg.Routing.Rules[0]["outboundTag"] != "direct-lo" {
		t.Fatalf("first rule outbound = %v, want direct-lo", cfg.Routing.Rules[0]["outboundTag"])
	}
	if cfg.Routing.Rules[1]["port"] != "53" || cfg.Routing.Rules[1]["outboundTag"] != "direct" {
		t.Fatalf("second rule = %v, want port 53 to direct", cfg.Routing.Rules[1])
	}
}

// A loopback proxy server must not be pinned to the physical NIC.
func TestBuildJSONTunHelperLoopbackServer(t *testing.T) {
	c := defaultServerConfig()
	c.Protocol = "freedom"
	c.Address = "127.0.0.1"

	g := defaultGlobalConfig()
	g.TunEnabled = true
	g.TunHelper = true
	g.TunBypassInterface = "en0"

	data, err := c.buildJSON(&g)
	if err != nil {
		t.Fatal(err)
	}
	var cfg struct {
		Outbounds []map[string]any `json:"outbounds"`
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatal(err)
	}
	if got := sockoptInterface(cfg.Outbounds[0]); got != "" {
		t.Fatalf("proxy interface = %q, want none", got)
	}
	if got := sockoptInterface(cfg.Outbounds[1]); got != "en0" {
		t.Fatalf("direct interface = %q, want en0", got)
	}
}

func TestBuildJSONTunHelperNeedsInterface(t *testing.T) {
	c := defaultServerConfig()
	c.Protocol = "freedom"
	g := defaultGlobalConfig()
	g.TunEnabled = true
	g.TunHelper = true
	if _, err := c.buildJSON(&g); err == nil {
		t.Fatal("the helper mode accepted a config without a physical interface")
	}
}

// The speed test runs while TUN is active: its temporary core needs the
// physical interface too, but not the helper's extra routing rules.
func TestBuildJSONBypassWithoutTun(t *testing.T) {
	c := defaultServerConfig()
	c.Protocol = "freedom"
	c.Address = "example.com"

	g := defaultGlobalConfig()
	g.TunEnabled = false
	g.TunBypassInterface = "en0"

	data, err := c.buildJSON(&g)
	if err != nil {
		t.Fatal(err)
	}
	var cfg struct {
		Inbounds  []map[string]any `json:"inbounds"`
		Outbounds []map[string]any `json:"outbounds"`
		Routing   struct {
			Rules []map[string]any `json:"rules"`
		} `json:"routing"`
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatal(err)
	}
	if got := sockoptInterface(cfg.Outbounds[0]); got != "en0" {
		t.Fatalf("proxy interface = %q, want en0", got)
	}
	if got := sockoptInterface(cfg.Outbounds[1]); got != "en0" {
		t.Fatalf("direct interface = %q, want en0", got)
	}
	for _, o := range cfg.Outbounds {
		if o["tag"] == "direct-lo" {
			t.Fatal("direct-lo is only needed in helper mode")
		}
	}
	if len(cfg.Routing.Rules) == 0 || cfg.Routing.Rules[0]["outboundTag"] != "direct" {
		t.Fatal("the helper loopback rule leaked into the speed test config")
	}
}

func TestTunNameForEmpty(t *testing.T) {
	g := defaultGlobalConfig()
	name := tunNameFor(&g)
	if name == "" {
		t.Fatal("tunNameFor returned an empty name")
	}
	switch runtime.GOOS {
	case "darwin":
		if !strings.HasPrefix(name, "utun") {
			t.Fatalf("macOS name = %q, want utunN", name)
		}
	case "linux", "windows":
		if !strings.HasPrefix(name, "sunray") {
			t.Fatalf("name = %q, want sunrayN", name)
		}
	}
}

func sockoptInterface(outbound map[string]any) string {
	stream, _ := outbound["streamSettings"].(map[string]any)
	sockopt, _ := stream["sockopt"].(map[string]any)
	iface, _ := sockopt["interface"].(string)
	return iface
}

// The helper-mode main config must be accepted by xray: no TUN inbound,
// interface-bound outbounds, and the loopback/DNS rules.
func TestStartHelperModeCore(t *testing.T) {
	iface, err := defaultRouteInterface()
	if err != nil {
		t.Skipf("no default route: %v", err)
	}
	port, err := freeLoopbackPort()
	if err != nil {
		t.Fatal(err)
	}
	c := defaultServerConfig()
	c.Protocol = "freedom"
	c.Address = "example.com"
	g := defaultGlobalConfig()
	g.ListenPort = strconv.Itoa(port)
	g.TunEnabled = true
	g.TunHelper = true
	g.TunBypassInterface = iface
	data, err := c.buildJSON(&g)
	if err != nil {
		t.Fatal(err)
	}
	inst, err := startInstance(data)
	if err != nil {
		t.Fatal(err)
	}
	inst.Close()
}
