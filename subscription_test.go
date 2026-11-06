//go:build !nogui

package main

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestParseSubscriptionBody(t *testing.T) {
	vmess, err := json.Marshal(map[string]any{
		"v": "2", "ps": "VMess Node", "add": "vmess.example.com", "port": "8443",
		"id": "uuid-vmess", "aid": "2", "scy": "auto", "net": "grpc",
		"path": "svc", "tls": "tls", "sni": "vmess.example.com", "fp": "firefox",
		"alpn": "h2",
	})
	if err != nil {
		t.Fatal(err)
	}
	ssUser := base64.StdEncoding.EncodeToString([]byte("aes-256-gcm:sspass"))
	links := []string{
		"vless://11111111-2222-3333-4444-555555555555@vless.example.com:443?encryption=none&security=reality&sni=www.example.com&type=ws&path=%2Fws&host=cdn.example.com&fp=chrome&pbk=pub&sid=abcd&spx=%2F#VLESS%20Node",
		"vmess://" + base64.StdEncoding.EncodeToString(vmess),
		"trojan://pass@trojan.example.com:443?security=tls&type=ws&path=%2Ft&host=t.example.com&sni=t.example.com#Trojan%20Node",
		"ss://" + ssUser + "@ss.example.com:8388#SS%20Node",
		"hysteria2://hy2pass@hy.example.com:443?sni=hy.example.com&insecure=1#HY2%20Node",
	}
	body := base64.StdEncoding.EncodeToString([]byte(strings.Join(links, "\n")))
	nodes, err := parseSubscriptionBody([]byte(body))
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != len(links) {
		t.Fatalf("got %d nodes, want %d: %+v", len(nodes), len(links), nodes)
	}

	vless := nodes[0]
	if vless.Protocol != "vless" || vless.Name != "VLESS Node" || vless.Address != "vless.example.com" ||
		vless.Port != "443" || vless.Secret != "11111111-2222-3333-4444-555555555555" ||
		vless.Network != "ws" || vless.Security != "reality" || vless.Path != "/ws" ||
		vless.Host != "cdn.example.com" || vless.SNI != "www.example.com" ||
		vless.PublicKey != "pub" || vless.ShortID != "abcd" || vless.SpiderX != "/" {
		t.Errorf("vless parsed as %+v", vless)
	}

	vm := nodes[1]
	if vm.Protocol != "vmess" || vm.Name != "VMess Node" || vm.Address != "vmess.example.com" ||
		vm.Port != "8443" || vm.Secret != "uuid-vmess" || vm.Network != "grpc" ||
		vm.VMessAlterID != "2" || vm.Service != "svc" || vm.Security != "tls" || vm.SNI != "vmess.example.com" ||
		vm.Fingerprint != "firefox" || vm.ALPN != "h2" {
		t.Errorf("vmess parsed as %+v", vm)
	}

	trojan := nodes[2]
	if trojan.Protocol != "trojan" || trojan.Name != "Trojan Node" || trojan.Secret != "pass" ||
		trojan.Network != "ws" || trojan.Path != "/t" || trojan.Host != "t.example.com" {
		t.Errorf("trojan parsed as %+v", trojan)
	}

	ss := nodes[3]
	if ss.Protocol != "shadowsocks" || ss.Name != "SS Node" || ss.Address != "ss.example.com" ||
		ss.Port != "8388" || ss.Method != "aes-256-gcm" || ss.Secret != "sspass" {
		t.Errorf("shadowsocks parsed as %+v", ss)
	}

	hy := nodes[4]
	if hy.Protocol != "hysteria" || hy.Name != "HY2 Node" || hy.Secret != "hy2pass" ||
		hy.Security != "tls" || hy.SNI != "hy.example.com" || !hy.AllowInsecure {
		t.Errorf("hysteria2 parsed as %+v", hy)
	}
}

// VMess links written with net=none mean plain TCP, and their aid must be
// kept so the generated config matches the server.
func TestParseVMessNoneNetworkAndAlterID(t *testing.T) {
	vmess, err := json.Marshal(map[string]any{
		"v": "2", "ps": "VMess None", "add": "vmess.example.com", "port": 443,
		"id": "uuid-vmess", "aid": 2, "net": "none",
	})
	if err != nil {
		t.Fatal(err)
	}
	nodes, err := parseSubscriptionBody([]byte("vmess://" + base64.StdEncoding.EncodeToString(vmess)))
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 1 {
		t.Fatalf("got %d nodes, want 1", len(nodes))
	}
	if nodes[0].Network != "tcp" || nodes[0].VMessAlterID != "2" {
		t.Fatalf("got network=%q alterId=%q, want tcp and 2", nodes[0].Network, nodes[0].VMessAlterID)
	}
}

func TestParseSubscriptionPlainText(t *testing.T) {
	body := "vless://uuid@example.com:443?security=tls&type=tcp#Plain\n\n# comment\n"
	nodes, err := parseSubscriptionBody([]byte(body))
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 1 || nodes[0].Name != "Plain" {
		t.Fatalf("got %+v", nodes)
	}
}

func TestSubscriptionSecurityExplicitNone(t *testing.T) {
	if got := subscriptionSecurity("none", "tls"); got != "none" {
		t.Errorf("explicit none = %q, want none", got)
	}
	if got := subscriptionSecurity("", "tls"); got != "tls" {
		t.Errorf("empty security = %q, want tls", got)
	}
}

func TestParseSubscriptionNoNodes(t *testing.T) {
	if _, err := parseSubscriptionBody([]byte("not a subscription")); err == nil {
		t.Fatal("want an error for a body without nodes")
	}
}

func TestFetchSubscriptionNodes(t *testing.T) {
	body := base64.StdEncoding.EncodeToString([]byte(
		"trojan://pass@example.com:443?security=tls#Fetched\n",
	))
	var userAgent string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		userAgent = r.Header.Get("User-Agent")
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	nodes, err := fetchSubscriptionNodes(subscriptionConfig{URL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 1 || nodes[0].Name != "Fetched" {
		t.Fatalf("got %+v", nodes)
	}
	if !strings.HasPrefix(userAgent, "SunRay/") {
		t.Errorf("user agent = %q", userAgent)
	}
}

func TestApplySubscriptionNodes(t *testing.T) {
	a := &app{cfg: defaultAppConfig()}
	a.cfg.Subscriptions = []subscriptionConfig{{ID: "sub-1", Name: "sub"}}
	a.cfg.Servers = []serverConfig{
		{ID: "manual", Protocol: "freedom"},
		{ID: "old", SubscriptionID: "sub-1", Protocol: "trojan", Address: "example.com", Port: "443", Secret: "pass", Security: "tls"},
	}
	a.cfg.ActiveID = "old"
	a.cfg.Results = map[string]serverResult{"old": {LatencyMs: 100}}

	nodes := []serverConfig{
		{Protocol: "trojan", Name: "Renamed", Address: "example.com", Port: "443", Secret: "pass", Security: "tls"},
		{Protocol: "vless", Name: "New", Address: "new.example.com", Port: "443", Secret: "uuid", Security: "tls"},
	}
	a.applySubscriptionNodes(&a.cfg.Subscriptions[0], nodes)

	if len(a.cfg.Servers) != 3 {
		t.Fatalf("got %d servers, want 3: %+v", len(a.cfg.Servers), a.cfg.Servers)
	}
	var kept *serverConfig
	for i := range a.cfg.Servers {
		if a.cfg.Servers[i].Address == "example.com" {
			kept = &a.cfg.Servers[i]
		}
	}
	if kept == nil || kept.ID != "old" || kept.Name != "Renamed" || kept.SubscriptionID != "sub-1" {
		t.Fatalf("the matching node was not kept: %+v", kept)
	}
	if a.cfg.ActiveID != "old" {
		t.Errorf("active id = %q, want old", a.cfg.ActiveID)
	}
	if _, ok := a.cfg.Results["old"]; !ok {
		t.Error("the kept node should keep its measurement")
	}
	if a.cfg.Subscriptions[0].ServerCount != 2 || a.cfg.Subscriptions[0].UpdatedAt == 0 {
		t.Errorf("subscription metadata = %+v", a.cfg.Subscriptions[0])
	}
}
