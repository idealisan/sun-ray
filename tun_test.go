package main

import (
	"strings"
	"testing"
)

// The TUN inbound must appear in the config only when it is turned on,
// with the settings the user gave.
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
