package main

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/xtls/xray-core/infra/conf/serial"
)

func TestRoutingPrivateAndLocalRules(t *testing.T) {
	c := defaultServerConfig()
	c.Protocol = "freedom"
	c.Address = "example.com"
	g := defaultGlobalConfig()
	data, err := c.buildJSON(&g)
	if err != nil {
		t.Fatal(err)
	}
	var cfg struct {
		Routing struct {
			Rules []struct {
				IP     []string `json:"ip"`
				Domain []string `json:"domain"`
			} `json:"rules"`
		} `json:"routing"`
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatal(err)
	}
	if len(cfg.Routing.Rules) < 2 {
		t.Fatalf("got %d rules, want at least 2", len(cfg.Routing.Rules))
	}
	if !reflect.DeepEqual(cfg.Routing.Rules[0].IP, privateSpecialCIDRs) {
		t.Errorf("private rule = %v", cfg.Routing.Rules[0].IP)
	}
	if !reflect.DeepEqual(cfg.Routing.Rules[1].Domain, localDomainRules) {
		t.Errorf("local domain rule = %v", cfg.Routing.Rules[1].Domain)
	}
	for _, want := range []string{"100.64.0.0/10", "198.18.0.0/15", "fc00::/7", "ff00::/8"} {
		if !strings.Contains(strings.Join(cfg.Routing.Rules[0].IP, " "), want) {
			t.Errorf("private rule misses %s", want)
		}
	}
	for _, want := range []string{"domain:local", "domain:home.arpa", "domain:router.asus.com", "dotless:"} {
		if !strings.Contains(strings.Join(cfg.Routing.Rules[1].Domain, " "), want) {
			t.Errorf("local domain rule misses %s", want)
		}
	}
}

func TestDNSPresets(t *testing.T) {
	tests := []struct {
		preset string
		custom string
		want   []string
	}{
		{dnsPresetSystem, "", nil},
		{dnsPresetAli, "", []string{"223.5.5.5", "223.6.6.6"}},
		{dnsPresetAliDoH, "", []string{"https://dns.alidns.com/dns-query"}},
		{dnsPresetCloudflare, "", []string{"1.1.1.1", "1.0.0.1"}},
		{dnsPresetCFDoH, "", []string{"https://cloudflare-dns.com/dns-query"}},
		{dnsPresetCustom, "9.9.9.9, https://dns.quad9.net/dns-query", []string{"9.9.9.9", "https://dns.quad9.net/dns-query"}},
		{dnsPresetCustom, "", nil},
	}
	for _, tc := range tests {
		g := defaultGlobalConfig()
		g.DNSPreset, g.DNSCustom = tc.preset, tc.custom
		if got := dnsServersFor(&g); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("dnsServersFor(%s, %q) = %v, want %v", tc.preset, tc.custom, got, tc.want)
		}
	}
}

func TestBuildJSONDNS(t *testing.T) {
	c := defaultServerConfig()
	c.Protocol = "freedom"
	c.Address = "example.com"

	g := defaultGlobalConfig()
	data, err := c.buildJSON(&g)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), `"dns"`) {
		t.Error("system DNS should not add a dns section")
	}

	g.DNSPreset = dnsPresetAliDoH
	data, err = c.buildJSON(&g)
	if err != nil {
		t.Fatal(err)
	}
	var cfg struct {
		DNS struct {
			Servers []struct {
				Address      string `json:"address"`
				SkipFallback bool   `json:"skipFallback"`
			} `json:"servers"`
		} `json:"dns"`
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatal(err)
	}
	if len(cfg.DNS.Servers) != 2 {
		t.Fatalf("got %d DNS servers, want 2", len(cfg.DNS.Servers))
	}
	if cfg.DNS.Servers[0].Address != "localhost" || !cfg.DNS.Servers[0].SkipFallback {
		t.Errorf("local DNS server = %+v", cfg.DNS.Servers[0])
	}
	if cfg.DNS.Servers[1].Address != "https://dns.alidns.com/dns-query" {
		t.Errorf("upstream = %q", cfg.DNS.Servers[1].Address)
	}
}

func TestSplitDNSServers(t *testing.T) {
	got := splitDNSServers(" 223.5.5.5,https://dns.alidns.com/dns-query; 1.1.1.1\n")
	want := []string{"223.5.5.5", "https://dns.alidns.com/dns-query", "1.1.1.1"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("splitDNSServers = %v, want %v", got, want)
	}
}

func TestDNSConfigAcceptedByCore(t *testing.T) {
	c := defaultServerConfig()
	c.Protocol = "freedom"
	c.Address = "example.com"
	for _, preset := range []string{dnsPresetAli, dnsPresetAliDoH, dnsPresetCloudflare, dnsPresetCFDoH} {
		g := defaultGlobalConfig()
		g.DNSPreset = preset
		data, err := c.buildJSON(&g)
		if err != nil {
			t.Fatalf("%s: %v", preset, err)
		}
		pb, err := serial.DecodeJSONConfig(strings.NewReader(string(data)))
		if err != nil {
			t.Fatalf("%s: %v", preset, err)
		}
		if _, err := pb.Build(); err != nil {
			t.Fatalf("%s: %v", preset, err)
		}
	}
}
