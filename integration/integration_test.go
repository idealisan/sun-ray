//go:build integration

// Package integration runs the headless Sun Ray binary against local xray-core
// servers, one protocol at a time, and checks the proxy with curl. The origin
// is a tiny HTTP server on the loopback interface; every upstream redirects
// the target to it, so the test never needs the public network.
package integration

import (
	"bufio"
	"bytes"
	"context"
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/xtls/xray-core/core"
	"github.com/xtls/xray-core/infra/conf/serial"
	_ "github.com/xtls/xray-core/main/distro/all"
)

const (
	originBody = "sun-ray integration ok\n"
	targetURL  = "http://1.1.1.1/"
)

var sunrayBin string

func TestMain(m *testing.M) {
	if bin := os.Getenv("SUNRAY_BIN"); bin != "" {
		sunrayBin = bin
		os.Exit(m.Run())
	}

	dir, err := os.MkdirTemp("", "sunray-integration-*")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	name := "sun-ray-nogui"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	bin := filepath.Join(dir, name)
	cmd := exec.Command("go", "build", "-tags", "nogui", "-o", bin, ".")
	cmd.Dir = ".."
	if out, err := cmd.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "build the headless binary: %v\n%s", err, out)
		os.RemoveAll(dir)
		os.Exit(1)
	}
	sunrayBin = bin
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

type env struct {
	certFile string
	keyFile  string

	uuid     string
	password string
	auth     string

	wgServerPrivate string
	wgServerPublic  string
	wgClientPrivate string
	wgClientPublic  string
}

type testCase struct {
	name       string
	udp        bool
	expectFail bool
	build      func(e *env, upstreamPort int) (inbound map[string]any, server map[string]any)
}

func TestProtocols(t *testing.T) {
	dir := t.TempDir()
	certFile, keyFile := writeCertificate(t, dir)
	e := &env{
		certFile: certFile,
		keyFile:  keyFile,
		uuid:     randomUUID(),
		password: "sun-ray-test-password",
		auth:     "sun-ray-test-auth",
	}
	e.wgServerPrivate, e.wgServerPublic = wireGuardKeyPair(t)
	e.wgClientPrivate, e.wgClientPublic = wireGuardKeyPair(t)

	cases := []testCase{
		{name: "vless-tcp", build: buildVLESS("tcp", false)},
		{name: "vless-ws-tls", build: buildVLESS("ws", true)},
		{name: "vless-grpc-tls", build: buildVLESS("grpc", true)},
		{name: "vless-httpupgrade-tls", build: buildVLESS("httpupgrade", true)},
		{name: "vmess-tcp", build: buildVMess("tcp", false)},
		{name: "vmess-ws-tls", build: buildVMess("ws", true)},
		{name: "trojan-tls", build: buildTrojan},
		{name: "shadowsocks", build: buildShadowsocks},
		{name: "socks", build: buildSocks},
		{name: "http", build: buildHTTP},
		{name: "wireguard", udp: true, build: buildWireGuard},
		{name: "hysteria2", udp: true, build: buildHysteria},
		{name: "masque", udp: true, build: buildMasque},
		{name: "blackhole", expectFail: true, build: buildBlackhole},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			upstreamPort := freeTCPPort(t)
			if tc.udp {
				upstreamPort = freeUDPPort(t)
			}
			inbound, server := tc.build(e, upstreamPort)
			if inbound != nil {
				startUpstream(t, inbound, originPort(t))
			}

			clientPort := freeTCPPort(t)
			cfgPath := writeClientConfig(t, t.TempDir(), clientPort, server)
			runSunRay(t, cfgPath, clientPort, tc.expectFail)
		})
	}

	t.Run("freedom", func(t *testing.T) {
		clientPort := freeTCPPort(t)
		server := map[string]any{
			"id": "test", "name": "freedom", "protocol": "freedom",
			"network": "tcp", "security": "none",
		}
		cfgPath := writeClientConfig(t, t.TempDir(), clientPort, server)
		runSunRayTarget(t, cfgPath, clientPort, fmt.Sprintf("http://127.0.0.1:%d/", originPort(t)), false)
	})
}

func buildVLESS(network string, tls bool) func(*env, int) (map[string]any, map[string]any) {
	return func(e *env, port int) (map[string]any, map[string]any) {
		inbound := inboundConfig("vless", port, map[string]any{
			"clients":    []any{map[string]any{"id": e.uuid, "level": 0}},
			"decryption": "none",
		}, transportStream(network, tls, e, nil))
		server := clientServer("vless", port)
		server["secret"] = e.uuid
		server["vlessEncrypt"] = "none"
		setTransport(server, network, tls)
		return inbound, server
	}
}

func buildVMess(network string, tls bool) func(*env, int) (map[string]any, map[string]any) {
	return func(e *env, port int) (map[string]any, map[string]any) {
		inbound := inboundConfig("vmess", port, map[string]any{
			"clients": []any{map[string]any{"id": e.uuid, "alterId": 0, "level": 0}},
		}, transportStream(network, tls, e, nil))
		server := clientServer("vmess", port)
		server["secret"] = e.uuid
		server["vmessSecurity"] = "auto"
		setTransport(server, network, tls)
		return inbound, server
	}
}

func buildTrojan(e *env, port int) (map[string]any, map[string]any) {
	inbound := inboundConfig("trojan", port, map[string]any{
		"clients": []any{map[string]any{"password": e.password}},
	}, transportStream("tcp", true, e, nil))
	server := clientServer("trojan", port)
	server["secret"] = e.password
	setTransport(server, "tcp", true)
	return inbound, server
}

func buildShadowsocks(e *env, port int) (map[string]any, map[string]any) {
	inbound := inboundConfig("shadowsocks", port, map[string]any{
		"method": "aes-256-gcm", "password": e.password, "network": "tcp,udp",
	}, nil)
	server := clientServer("shadowsocks", port)
	server["secret"] = e.password
	server["method"] = "aes-256-gcm"
	return inbound, server
}

func buildSocks(e *env, port int) (map[string]any, map[string]any) {
	inbound := inboundConfig("socks", port, map[string]any{"auth": "noauth", "udp": true}, nil)
	return inbound, clientServer("socks", port)
}

func buildHTTP(e *env, port int) (map[string]any, map[string]any) {
	inbound := inboundConfig("http", port, map[string]any{}, nil)
	return inbound, clientServer("http", port)
}

func buildWireGuard(e *env, port int) (map[string]any, map[string]any) {
	inbound := inboundConfig("wireguard", port, map[string]any{
		"secretKey": e.wgServerPrivate,
		"address":   []any{"172.16.0.1/24"},
		"peers": []any{map[string]any{
			"publicKey":  e.wgClientPublic,
			"allowedIPs": []any{"172.16.0.2/32"},
		}},
		"mtu": 1420,
	}, nil)
	server := clientServer("wireguard", port)
	server["secret"] = e.wgClientPrivate
	server["peerKey"] = e.wgServerPublic
	server["wgLocalAddr"] = "172.16.0.2/32"
	server["wgMtu"] = "1420"
	return inbound, server
}

func buildHysteria(e *env, port int) (map[string]any, map[string]any) {
	stream := transportStream("hysteria", true, e, map[string]any{
		"hysteriaSettings": map[string]any{"version": 2},
	})
	inbound := inboundConfig("hysteria", port, map[string]any{
		"version": 2,
		"users":   []any{map[string]any{"auth": e.auth, "email": "test@example.com"}},
	}, stream)
	server := clientServer("hysteria", port)
	server["secret"] = e.auth
	server["security"] = "tls"
	server["sni"] = "localhost"
	server["allowInsecure"] = true
	return inbound, server
}

func buildMasque(e *env, port int) (map[string]any, map[string]any) {
	stream := transportStream("masque", true, e, nil)
	inbound := inboundConfig("masque", port, map[string]any{
		"users":   []any{map[string]any{"email": "test@example.com", "pass": e.password}},
		"address": []any{"10.14.0.1/24"},
	}, stream)
	server := clientServer("masque", port)
	server["secret"] = e.password
	server["user"] = "test@example.com"
	server["security"] = "tls"
	server["sni"] = "localhost"
	server["allowInsecure"] = true
	return inbound, server
}

func buildBlackhole(e *env, port int) (map[string]any, map[string]any) {
	return nil, map[string]any{
		"id": "test", "name": "blackhole", "protocol": "blackhole",
		"network": "tcp", "security": "none",
	}
}

func inboundConfig(protocol string, port int, settings map[string]any, stream map[string]any) map[string]any {
	in := map[string]any{
		"tag": "in", "listen": "127.0.0.1", "port": port,
		"protocol": protocol, "settings": settings,
	}
	if stream != nil {
		in["streamSettings"] = stream
	}
	return in
}

func clientServer(protocol string, port int) map[string]any {
	return map[string]any{
		"id": "test", "name": protocol,
		"protocol": protocol,
		"address":  "127.0.0.1",
		"port":     strconv.Itoa(port),
		"network":  "tcp",
		"security": "none",
	}
}

func setTransport(server map[string]any, network string, tls bool) {
	server["network"] = network
	if network != "tcp" {
		server["path"] = "/sun-ray"
		server["host"] = "localhost"
		server["service"] = "sun-ray"
	}
	if tls {
		server["security"] = "tls"
		server["sni"] = "localhost"
		server["allowInsecure"] = true
	}
}

func transportStream(network string, tls bool, e *env, extra map[string]any) map[string]any {
	s := map[string]any{"network": network}
	if tls {
		s["security"] = "tls"
		s["tlsSettings"] = map[string]any{
			"certificates": []any{map[string]any{
				"certificateFile": e.certFile,
				"keyFile":         e.keyFile,
			}},
		}
	}
	switch network {
	case "ws":
		s["wsSettings"] = map[string]any{
			"path": "/sun-ray", "headers": map[string]any{"Host": "localhost"},
		}
	case "grpc":
		s["grpcSettings"] = map[string]any{"serviceName": "sun-ray"}
	case "httpupgrade":
		s["httpupgradeSettings"] = map[string]any{"path": "/sun-ray", "host": "localhost"}
	}
	for k, v := range extra {
		s[k] = v
	}
	return s
}

func startUpstream(t *testing.T, inbound map[string]any, origin int) {
	t.Helper()
	cfg := map[string]any{
		"log":      map[string]any{"loglevel": "warning"},
		"inbounds": []any{inbound},
		"outbounds": []any{map[string]any{
			"protocol": "freedom",
			"settings": map[string]any{
				"redirect":   fmt.Sprintf("127.0.0.1:%d", origin),
				"finalRules": []any{map[string]any{"action": "allow"}},
			},
		}},
	}
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	pb, err := serial.DecodeJSONConfig(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("decode upstream config: %v", err)
	}
	conf, err := pb.Build()
	if err != nil {
		t.Fatalf("build upstream config: %v", err)
	}
	inst, err := core.New(conf)
	if err != nil {
		t.Fatalf("create upstream core: %v", err)
	}
	if err := inst.Start(); err != nil {
		inst.Close()
		t.Fatalf("start upstream core: %v", err)
	}
	t.Cleanup(func() { _ = inst.Close() })
}

func originPort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, originBody)
	})}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })
	return ln.Addr().(*net.TCPAddr).Port
}

func writeClientConfig(t *testing.T, dir string, listenPort int, server map[string]any) string {
	t.Helper()
	cfg := map[string]any{
		"global": map[string]any{
			"listenPort": strconv.Itoa(listenPort),
			"allowLAN":   false,
			"logLevel":   "warning",
			"routeCN":    false,
			"routeAds":   false,
		},
		"servers":  []any{server},
		"activeId": server["id"],
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "sunray.json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func runSunRay(t *testing.T, cfgPath string, port int, expectFail bool) {
	t.Helper()
	runSunRayTarget(t, cfgPath, port, targetURL, expectFail)
}

func runSunRayTarget(t *testing.T, cfgPath string, port int, target string, expectFail bool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, sunrayBin, "-config", cfgPath, "-test-mode")
	var out syncBuffer
	cmd.Stderr = &out
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start sun-ray: %v", err)
	}
	ready := make(chan struct{}, 1)
	readDone := make(chan struct{})
	go func() {
		defer close(readDone)
		sc := bufio.NewScanner(stdout)
		for sc.Scan() {
			line := sc.Text()
			_, _ = out.Write([]byte(line + "\n"))
			if strings.HasPrefix(line, "SUNRAY_READY ") {
				select {
				case ready <- struct{}{}:
				default:
				}
			}
		}
	}()

	exited := make(chan struct{})
	var waitErr error
	go func() {
		<-readDone
		waitErr = cmd.Wait()
		close(exited)
	}()
	t.Cleanup(func() {
		_ = cmd.Process.Signal(os.Interrupt)
		select {
		case <-exited:
		case <-time.After(10 * time.Second):
			_ = cmd.Process.Kill()
			<-exited
		}
		if t.Failed() {
			t.Logf("sun-ray output:\n%s", out.String())
		}
	})

	select {
	case <-ready:
	case <-exited:
		t.Fatalf("sun-ray exited before ready: %v\n%s", waitErr, out.String())
	case <-ctx.Done():
		t.Fatalf("timed out waiting for SUNRAY_READY\n%s", out.String())
	}

	curl := exec.CommandContext(ctx, "curl", "-sS", "--max-time", "20",
		"--socks5-hostname", fmt.Sprintf("127.0.0.1:%d", port), target)
	body, err := curl.CombinedOutput()
	if expectFail {
		if err == nil {
			t.Fatalf("the request succeeded although it should be blocked: %s", body)
		}
		return
	}
	if err != nil {
		t.Fatalf("curl: %v\n%s\nsun-ray:\n%s", err, body, out.String())
	}
	if string(body) != originBody {
		t.Fatalf("body = %q, want %q", body, originBody)
	}
}

func writeCertificate(t *testing.T, dir string) (certFile, keyFile string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "localhost"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
		DNSNames:              []string{"localhost"},
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1")},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	certFile = filepath.Join(dir, "cert.pem")
	keyFile = filepath.Join(dir, "key.pem")
	if err := os.WriteFile(certFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyFile, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}), 0o600); err != nil {
		t.Fatal(err)
	}
	return certFile, keyFile
}

func wireGuardKeyPair(t *testing.T) (private, public string) {
	t.Helper()
	key, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(key.Bytes()),
		base64.StdEncoding.EncodeToString(key.PublicKey().Bytes())
}

func randomUUID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

func freeTCPPort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

func freeUDPPort(t *testing.T) int {
	t.Helper()
	conn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	return conn.LocalAddr().(*net.UDPAddr).Port
}

type syncBuffer struct {
	mu sync.Mutex
	b  strings.Builder
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.String()
}
