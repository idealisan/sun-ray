package main

// sharelink_test.go checks that exported share URLs can be read back by the
// app's own parser, both for community-standard protocols and for the
// private sunray:// envelope.

import (
	"encoding/base64"
	"strings"
	"testing"
)

// standardCases are servers whose share links follow community formats.
func standardCases() []serverConfig {
	return []serverConfig{
		{Protocol: "vless", Address: "vless.example.com", Port: "443", Secret: "uuid-vless", Name: "VLESS 节点",
			Network: "ws", Security: "reality", SNI: "sni.example.com", ALPN: "h2,http/1.1",
			Fingerprint: "chrome", Path: "/ws", Host: "host.example.com", Flow: "xtls-rprx-vision",
			PublicKey: "pbk-value", ShortID: "sid-value", SpiderX: "/spx", VlessEncrypt: "none"},
		{Protocol: "vmess", Address: "vmess.example.com", Port: "443", Secret: "uuid-vmess", Name: "VMess 节点",
			Network: "ws", Security: "tls", VMessSecurity: "auto", VMessAlterID: "0",
			SNI: "sni.example.com", Path: "/vmess", Host: "ws.example.com"},
		{Protocol: "trojan", Address: "trojan.example.com", Port: "443", Secret: "trojan-pass", Name: "Trojan 节点",
			Network: "tcp", Security: "tls", SNI: "sni.example.com", ALPN: "h2"},
		{Protocol: "shadowsocks", Address: "ss.example.com", Port: "8388", Secret: "ss-pass", Name: "SS 节点",
			Method: "aes-256-gcm"},
		{Protocol: "hysteria", Address: "hy2.example.com", Port: "443", Secret: "hy2-auth", Name: "Hy2 节点",
			SNI: "sni.example.com", ALPN: "h3", AllowInsecure: true},
		{Protocol: "socks", Address: "socks.example.com", Port: "1080", User: "socks-user", Pass: "socks-pass", Name: "Socks 节点"},
		{Protocol: "http", Address: "http.example.com", Port: "8080", User: "http-user", Pass: "http-pass", Name: "HTTP 节点"},
	}
}

// privateCases are protocols with no community-standard share link.
func privateCases() []serverConfig {
	return []serverConfig{
		{Protocol: "wireguard", Address: "wg.example.com", Port: "51820", Secret: "wg-private-key", Name: "WG 节点",
			PeerKey: "wg-peer-key", WGPreSharedKey: "wg-psk", WGKeepAlive: "25", WGMTU: "1420",
			WGLocalAddr: "10.0.0.2/32"},
		{Protocol: "masque", Address: "masque.example.com", Port: "443", Name: "Masque 节点",
			User: "masque-user", RemoteDNS: "8.8.8.8,1.1.1.1"},
		{Protocol: "freedom", Name: "Freedom 节点", FreedomDomainStrategy: "UseIP"},
		{Protocol: "blackhole", Name: "Blackhole 节点"},
		{Protocol: "dns", Name: "DNS 节点"},
		{Protocol: "loopback", Name: "Loopback 节点", InboundTag: "mixed-in"},
	}
}

func TestShareLinkRoundTripStandard(t *testing.T) {
	for _, want := range standardCases() {
		t.Run(want.Protocol, func(t *testing.T) {
			link, err := shareLink(want)
			if err != nil {
				t.Fatalf("shareLink: %v", err)
			}
			got, err := parseShareLink(link)
			if err != nil {
				t.Fatalf("parseShareLink(%q): %v", link, err)
			}
			assertCoreFields(t, want, got)
		})
	}
}

func TestShareLinkRoundTripPrivate(t *testing.T) {
	for _, want := range privateCases() {
		t.Run(want.Protocol, func(t *testing.T) {
			link, err := shareLink(want)
			if err != nil {
				t.Fatalf("shareLink: %v", err)
			}
			if !strings.HasPrefix(link, "sunray://") {
				t.Fatalf("expected a sunray:// link, got %q", link)
			}
			got, err := parseShareLink(link)
			if err != nil {
				t.Fatalf("parseShareLink: %v", err)
			}
			// The private envelope carries every field, unchanged.
			want.ID, want.SubscriptionID = "", ""
			if got != want {
				t.Errorf("sunray:// lost fields\nwant: %+v\ngot:  %+v", want, got)
			}
		})
	}
}

// assertCoreFields checks the fields the community parsers actually restore.
func assertCoreFields(t *testing.T, want, got serverConfig) {
	t.Helper()
	for _, c := range []struct {
		name, want, got string
	}{
		{"protocol", want.Protocol, got.Protocol},
		{"address", want.Address, got.Address},
		{"port", want.Port, got.Port},
		{"secret", want.Secret, got.Secret},
		{"name", want.Name, got.Name},
		{"network", want.Network, got.Network},
		{"security", want.Security, got.Security},
		{"sni", want.SNI, got.SNI},
		{"alpn", want.ALPN, got.ALPN},
		{"fingerprint", want.Fingerprint, got.Fingerprint},
		{"path", want.Path, got.Path},
		{"host", want.Host, got.Host},
		{"flow", want.Flow, got.Flow},
		{"vlessEncrypt", want.VlessEncrypt, got.VlessEncrypt},
		{"publicKey", want.PublicKey, got.PublicKey},
		{"shortID", want.ShortID, got.ShortID},
		{"spiderX", want.SpiderX, got.SpiderX},
		{"method", want.Method, got.Method},
		{"user", want.User, got.User},
		{"pass", want.Pass, got.Pass},
	} {
		if c.want != "" && c.want != c.got {
			t.Errorf("%s: want %q, got %q", c.name, c.want, c.got)
		}
	}
	if want.AllowInsecure != got.AllowInsecure {
		t.Errorf("allowInsecure: want %v, got %v", want.AllowInsecure, got.AllowInsecure)
	}
}

func TestShareLinkNameEscaping(t *testing.T) {
	s := serverConfig{Protocol: "vless", Address: "a.example.com", Port: "443", Secret: "uuid", Name: "名称 #1 & 空格 100%"}
	link, err := shareLink(s)
	if err != nil {
		t.Fatal(err)
	}
	got, err := parseShareLink(link)
	if err != nil {
		t.Fatalf("parseShareLink(%q): %v", link, err)
	}
	if got.Name != s.Name {
		t.Errorf("name: want %q, got %q (link %q)", s.Name, got.Name, link)
	}
}

func TestParseShareLinkRejectsBadSunray(t *testing.T) {
	for _, raw := range []string{
		"sunray://not-base64!!",
		"sunray://" + base64.RawURLEncoding.EncodeToString([]byte(`{"protocol":"vless"}`)),
		"sunray://" + base64.RawURLEncoding.EncodeToString([]byte(`not json`)),
	} {
		if _, err := parseShareLink(raw); err == nil {
			t.Errorf("expected an error for %q", raw)
		}
	}
}

func TestShareLinkMissingFields(t *testing.T) {
	if _, err := shareLink(serverConfig{Protocol: "vless", Address: "", Port: "443"}); err == nil {
		t.Error("an empty address should fail")
	}
	if _, err := shareLink(serverConfig{Protocol: "trojan", Address: "a", Port: "443"}); err == nil {
		t.Error("a missing password should fail")
	}
	if _, err := shareLink(serverConfig{Protocol: "unknown", Address: "a", Port: "443"}); err == nil {
		t.Error("an unknown protocol should fail")
	}
}

func TestShareLinksForServers(t *testing.T) {
	links, failures := shareLinksForServers([]serverConfig{
		{Protocol: "vless", Address: "a.example.com", Port: "443", Secret: "uuid-1", Name: "A"},
		{Protocol: "freedom", Name: "B"},
		{Protocol: "vless", Address: "", Port: "443", Secret: "uuid-2", Name: "bad"},
	})
	if len(failures) != 1 {
		t.Errorf("expected one failure, got %v", failures)
	}
	lines := strings.Split(links, "\n")
	if len(lines) != 2 {
		t.Fatalf("expected two links, got %q", links)
	}
	if !strings.HasPrefix(lines[0], "vless://") || !strings.HasPrefix(lines[1], "sunray://") {
		t.Errorf("unexpected links: %q", lines)
	}
}
