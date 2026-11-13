//go:build !nogui

package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestNormalizePolicy(t *testing.T) {
	tests := []struct {
		name string
		in   policyConfig
		want string
		ok   bool
	}{
		{"domain lowercased", policyConfig{policyTypeDomain, " Example.COM. ", policyActionProxy}, "example.com", true},
		{"wildcard is a suffix", policyConfig{policyTypeDomain, "*.example.com", policyActionDirect}, "example.com", true},
		{"exact keeps the name", policyConfig{policyTypeFull, "Example.com", policyActionProxy}, "example.com", true},
		{"exact with a wildcard", policyConfig{policyTypeFull, "*.example.com", policyActionProxy}, "", false},
		{"spaces", policyConfig{policyTypeDomain, "a b", policyActionProxy}, "", false},
		{"port", policyConfig{policyTypeDomain, "example.com:443", policyActionProxy}, "", false},
		{"numeric host", policyConfig{policyTypeDomain, "999.1.1.1", policyActionProxy}, "", false},
		{"ipv4", policyConfig{policyTypeIP, "10.0.0.1", policyActionProxy}, "10.0.0.1", true},
		{"cidr", policyConfig{policyTypeIP, "10.0.0.1/8", policyActionProxy}, "10.0.0.0/8", true},
		{"ipv6", policyConfig{policyTypeIP, "2001:DB8::1", policyActionDirect}, "2001:db8::1", true},
		{"bad ip", policyConfig{policyTypeIP, "999.1.1.1", policyActionProxy}, "", false},
		{"bad prefix", policyConfig{policyTypeIP, "10.0.0.1/33", policyActionProxy}, "", false},
		{"bad action", policyConfig{policyTypeDomain, "example.com", "drop"}, "", false},
	}
	for _, tc := range tests {
		got, err := normalizePolicy(tc.in)
		if tc.ok {
			if err != nil {
				t.Errorf("%s: %v", tc.name, err)
			} else if got.Value != tc.want {
				t.Errorf("%s: value = %q, want %q", tc.name, got.Value, tc.want)
			}
			continue
		}
		if err == nil {
			t.Errorf("%s: no error, got %+v", tc.name, got)
		}
	}
}

func TestPolicyRules(t *testing.T) {
	g := defaultGlobalConfig()
	g.Policies = []policyConfig{
		{Type: policyTypeDomain, Value: "example.com", Action: policyActionDirect},
		{Type: policyTypeFull, Value: "full.example.com", Action: policyActionProxy},
		{Type: policyTypeIP, Value: "10.0.0.0/8", Action: policyActionBlock},
	}
	c := defaultServerConfig()
	c.Protocol = "freedom"
	data, err := c.buildJSON(&g)
	if err != nil {
		t.Fatal(err)
	}
	var cfg struct {
		Routing struct {
			Rules []struct {
				Domain      []string `json:"domain"`
				IP          []string `json:"ip"`
				OutboundTag string   `json:"outboundTag"`
			} `json:"rules"`
		} `json:"routing"`
		Outbounds []struct {
			Tag string `json:"tag"`
		} `json:"outbounds"`
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatal(err)
	}
	rules := cfg.Routing.Rules
	if len(rules) < 3 {
		t.Fatalf("rules = %d", len(rules))
	}
	want := []struct {
		domain string
		ip     string
		tag    string
	}{
		{"domain:example.com", "", policyActionDirect},
		{"full:full.example.com", "", policyActionProxy},
		{"", "10.0.0.0/8", policyActionBlock},
	}
	for i, w := range want {
		if w.domain != "" && (len(rules[i].Domain) != 1 || rules[i].Domain[0] != w.domain) {
			t.Errorf("rule %d domain = %v, want %s", i, rules[i].Domain, w.domain)
		}
		if w.ip != "" && (len(rules[i].IP) != 1 || rules[i].IP[0] != w.ip) {
			t.Errorf("rule %d ip = %v, want %s", i, rules[i].IP, w.ip)
		}
		if rules[i].OutboundTag != w.tag {
			t.Errorf("rule %d tag = %q, want %q", i, rules[i].OutboundTag, w.tag)
		}
	}
	// The private ranges come after the user's rules.
	if len(rules[3].IP) == 0 || rules[3].IP[0] != privateSpecialCIDRs[0] {
		t.Errorf("the private rule does not follow the policies: %+v", rules[3])
	}
	block := false
	for _, out := range cfg.Outbounds {
		if out.Tag == "block" {
			block = true
		}
	}
	if !block {
		t.Error("a block policy needs the block outbound even with the ads preset off")
	}
}

func TestPolicyRulesAfterTunEssentials(t *testing.T) {
	g := defaultGlobalConfig()
	g.TunEnabled = true
	g.TunHelper = true
	g.TunBypassInterface = "en0"
	g.Policies = []policyConfig{{Type: policyTypeIP, Value: "192.0.2.0/24", Action: policyActionDirect}}
	c := defaultServerConfig()
	c.Protocol = "freedom"
	data, err := c.buildJSON(&g)
	if err != nil {
		t.Fatal(err)
	}
	var cfg struct {
		Routing struct {
			Rules []struct {
				IP          []string `json:"ip"`
				Port        string   `json:"port"`
				OutboundTag string   `json:"outboundTag"`
			} `json:"rules"`
		} `json:"routing"`
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatal(err)
	}
	rules := cfg.Routing.Rules
	if len(rules) < 3 || rules[0].OutboundTag != "direct-lo" || rules[1].Port != "53" {
		t.Fatalf("the TUN essentials are not first: %+v", rules[:2])
	}
	if len(rules[2].IP) != 1 || rules[2].IP[0] != "192.0.2.0/24" {
		t.Errorf("policy rule = %+v", rules[2])
	}
}

func TestPolicyRejectsAnInvalidRuleAtBuild(t *testing.T) {
	g := defaultGlobalConfig()
	g.Policies = []policyConfig{{Type: policyTypeIP, Value: "nope", Action: policyActionDirect}}
	c := defaultServerConfig()
	c.Protocol = "freedom"
	if _, err := c.buildJSON(&g); err == nil {
		t.Fatal("an invalid policy must not build")
	}
}

func TestMatchPolicyRules(t *testing.T) {
	a := &app{cfg: defaultAppConfig()}
	a.cfg.Global.Policies = []policyConfig{
		{Type: policyTypeFull, Value: "example.com", Action: policyActionProxy},
		{Type: policyTypeIP, Value: "10.0.0.0/8", Action: policyActionDirect},
	}
	// An exact rule leaves the subdomains alone, so the answer is the
	// default, not the rule.
	m, err := a.matchPolicy("www.example.com")
	if err != nil {
		t.Fatal(err)
	}
	if m.Source != "未命中直连规则" || m.Action != policyActionProxy {
		t.Errorf("www.example.com = %+v", m)
	}
	m, err = a.matchPolicy("example.com")
	if err != nil {
		t.Fatal(err)
	}
	if m.Action != policyActionProxy || !strings.Contains(m.Source, "用户策略 #1") {
		t.Errorf("example.com = %+v", m)
	}
	m, err = a.matchPolicy("10.1.2.3")
	if err != nil {
		t.Fatal(err)
	}
	if m.Action != policyActionDirect || !strings.Contains(m.Source, "用户策略 #2") {
		t.Errorf("10.1.2.3 = %+v", m)
	}
	m, err = a.matchPolicy("[2001:db8::1]:443")
	if err != nil {
		t.Fatal(err)
	}
	if m.Action != policyActionDirect || !strings.Contains(m.Source, "私网") {
		t.Errorf("IPv6 private = %+v", m)
	}
}

func TestMatchPolicyCN(t *testing.T) {
	a := &app{cfg: defaultAppConfig()}
	m, err := a.matchPolicy("www.baidu.com")
	if err != nil {
		t.Fatal(err)
	}
	if m.Action != policyActionDirect || !strings.Contains(m.Source, "绕过大陆") {
		t.Errorf("baidu = %+v", m)
	}
	m, err = a.matchPolicy("www.google.com")
	if err != nil {
		t.Fatal(err)
	}
	if m.Action != policyActionProxy {
		t.Errorf("google = %+v", m)
	}
	m, err = a.matchPolicy("8.8.8.8")
	if err != nil {
		t.Fatal(err)
	}
	if m.Action != policyActionProxy {
		t.Errorf("8.8.8.8 = %+v", m)
	}
}

func TestMatchPolicyRejectsGarbage(t *testing.T) {
	a := &app{cfg: defaultAppConfig()}
	if _, err := a.matchPolicy("999.1.1.1"); err == nil {
		t.Error("999.1.1.1 should be rejected")
	}
	if _, err := a.matchPolicy("   "); err == nil {
		t.Error("an empty query should be rejected")
	}
}
