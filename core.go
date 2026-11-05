package main

// core.go embeds xray-core as a library: the GUI process and the proxy core
// are one single binary, talking through function calls — no subprocess, no
// command line. Start/stop call core.Instance directly, logs come through
// log.RegisterHandler, traffic counters through the stats manager.
// Protocol-specific config building lives in protocols.go.

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/xtls/xray-core/common/log"
	"github.com/xtls/xray-core/core"
	"github.com/xtls/xray-core/features/stats"
	"github.com/xtls/xray-core/infra/conf/serial"
	_ "github.com/xtls/xray-core/main/distro/all" // register every protocol and feature
)

// globalConfig is shared by every server: inbound, logging and routing.
type globalConfig struct {
	ListenPort string `json:"listenPort"` // one port serves SOCKS and HTTP (mixed inbound)
	AllowLAN   bool   `json:"allowLAN"`   // listen on 0.0.0.0 instead of 127.0.0.1

	LogLevel string `json:"logLevel"`
	RouteCN  bool   `json:"routeCN"`  // direct traffic to mainland China sites/IPs
	RouteAds bool   `json:"routeAds"` // block ad domains
}

// serverConfig is one entry of the server list; protocolSpec binds into it.
type serverConfig struct {
	ID   string `json:"id"`
	Name string `json:"name"`

	Protocol string `json:"protocol"` // key into protocolRegistry
	Address  string `json:"address"`  // server address
	Port     string `json:"port"`     // server port
	Secret   string `json:"secret"`   // UUID / password / auth / secretKey
	Method   string `json:"method"`   // shadowsocks cipher
	User     string `json:"user"`     // socks / http outbound user
	Pass     string `json:"pass"`     // socks / http outbound password
	PeerKey  string `json:"peerKey"`  // wireguard peer public key
	Flow     string `json:"flow"`     // vless flow, xtls-rprx-vision or empty

	VlessEncrypt          string `json:"vlessEncrypt"`          // vless encryption, default none
	VMessSecurity         string `json:"vmessSecurity"`         // vmess user security, default auto
	FreedomDomainStrategy string `json:"freedomDomainStrategy"` // freedom domainStrategy
	InboundTag            string `json:"inboundTag"`            // loopback inboundTag
	WGPreSharedKey        string `json:"wgPreSharedKey"`
	WGKeepAlive           string `json:"wgKeepAlive"`
	WGMTU                 string `json:"wgMtu"`
	WGLocalAddr           string `json:"wgLocalAddr"`
	RemoteDNS             string `json:"remoteDNS"` // masque, comma separated

	Network string `json:"network"` // transport: tcp, ws, grpc, httpupgrade
	Path    string `json:"path"`    // ws / httpupgrade path
	Host    string `json:"host"`    // ws / httpupgrade Host header
	Service string `json:"service"` // grpc serviceName

	Security      string `json:"security"` // none, tls, reality
	SNI           string `json:"sni"`
	ALPN          string `json:"alpn"`        // comma separated, e.g. "h2,http/1.1"
	Fingerprint   string `json:"fingerprint"` // uTLS fingerprint: chrome, firefox, ...
	AllowInsecure bool   `json:"allowInsecure"`
	PublicKey     string `json:"publicKey"` // reality
	ShortID       string `json:"shortID"`   // reality
	SpiderX       string `json:"spiderX"`   // reality
}

// appConfig is the whole persisted state: global settings plus the server
// list and the id of the server in use.
type appConfig struct {
	Global   globalConfig            `json:"global"`
	Servers  []serverConfig          `json:"servers"`
	ActiveID string                  `json:"activeId"`
	Results  map[string]serverResult `json:"results,omitempty"`
}

// serverResult is a stored measurement of one server, kept so the list
// still shows which server is faster after a restart.
type serverResult struct {
	LatencyMs int     `json:"latencyMs"`
	SpeedBps  float64 `json:"speedBps"`
	TestedAt  int64   `json:"testedAt"` // unix seconds
}

func defaultGlobalConfig() globalConfig {
	return globalConfig{ListenPort: "10808", LogLevel: "info"}
}

func defaultServerConfig() serverConfig {
	return serverConfig{
		ID: newServerID(), Protocol: "vless", Port: "443", Method: "aes-256-gcm",
		VMessSecurity: "auto",
		Network:       "tcp", Security: "none", Fingerprint: "chrome", SpiderX: "/",
	}
}

func defaultAppConfig() appConfig {
	s := defaultServerConfig()
	return appConfig{
		Global:   defaultGlobalConfig(),
		Servers:  []serverConfig{s},
		ActiveID: s.ID,
		Results:  map[string]serverResult{},
	}
}

// newServerID returns a random id, stable across renames and reorders.
func newServerID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 16)
	}
	return hex.EncodeToString(b[:])
}

// displayName is the label the server list shows for this entry.
func (s *serverConfig) displayName() string {
	if s.Name != "" {
		return s.Name
	}
	if s.Address != "" {
		return s.Protocol + " · " + s.Address
	}
	return "未命名 " + s.Protocol
}

// buildJSON renders the server fields and the shared global settings into a
// full xray JSON config, with stats and policy enabled so the UI can read
// traffic counters.
func (c *serverConfig) buildJSON(g *globalConfig) ([]byte, error) {
	listenPort, err := strconv.Atoi(g.ListenPort)
	if err != nil || listenPort < 1 || listenPort > 65535 {
		return nil, fmt.Errorf("监听端口无效: %q", g.ListenPort)
	}
	listen := "127.0.0.1"
	if g.AllowLAN {
		listen = "0.0.0.0"
	}

	proxy, err := c.buildOutbound()
	if err != nil {
		return nil, err
	}

	// Simple routing: private ranges always go direct; presets add more.
	// Private ranges are spelled out as CIDRs so no geoip.dat asset is needed.
	rules := []any{
		map[string]any{"type": "field", "ip": []string{
			"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16", "127.0.0.0/8",
			"169.254.0.0/16", "::1/128", "fc00::/7", "fe80::/10",
		}, "outboundTag": "direct"},
	}
	outbounds := []any{proxy, map[string]any{"protocol": "freedom", "tag": "direct"}}
	if g.RouteAds {
		rules = append(rules, map[string]any{"type": "field", "domain": []string{"geosite:category-ads-all"}, "outboundTag": "block"})
		outbounds = append(outbounds, map[string]any{"protocol": "blackhole", "tag": "block"})
	}
	if g.RouteCN {
		rules = append(rules,
			map[string]any{"type": "field", "domain": []string{"geosite:cn"}, "outboundTag": "direct"},
			map[string]any{"type": "field", "ip": []string{"geoip:cn"}, "outboundTag": "direct"},
		)
	}

	// Sniffing is always on: without it the domain routing rules
	// (geosite) have nothing to match on. routeOnly keeps the sniffed
	// domain for routing alone and never rewrites the destination, and
	// metadataOnly avoids waiting for the whole handshake, so the
	// connection keeps its original address and the added delay stays
	// negligible.
	sniffing := map[string]any{
		"enabled":      true,
		"destOverride": []string{"http", "tls", "quic"},
		"metadataOnly": true,
		"routeOnly":    true,
	}

	cfg := map[string]any{
		"log":       map[string]any{"loglevel": g.LogLevel},
		"routing":   map[string]any{"domainStrategy": "IPIfNonMatch", "rules": rules},
		"outbounds": outbounds,
		"stats":     map[string]any{},
		"policy": map[string]any{
			"levels": map[string]any{"0": map[string]any{
				"statsUserUplink": true, "statsUserDownlink": true,
			}},
			"system": map[string]any{
				"statsInboundUplink": true, "statsInboundDownlink": true,
				"statsOutboundUplink": true, "statsOutboundDownlink": true,
			},
		},
		"inbounds": []any{
			// Mixed inbound: one port auto-detects SOCKS and HTTP clients.
			map[string]any{
				"tag": "mixed-in", "listen": listen, "port": listenPort,
				"protocol": "mixed", "sniffing": sniffing,
				"settings": map[string]any{"auth": "noauth", "udp": true},
			},
		},
	}
	return json.MarshalIndent(cfg, "", "  ")
} // guiHandler turns xray log messages into lines for the UI.
type guiHandler struct{ fn func(string) }

func (h *guiHandler) Handle(msg log.Message) {
	if gm, ok := msg.(*log.GeneralMessage); ok && gm.Severity < log.Severity_Info {
		return // drop debug spam, keep info and above
	}
	h.fn(msg.String())
}

// xrayCore owns the in-process Instance.
type xrayCore struct {
	mu       sync.Mutex
	instance *core.Instance
}

func (x *xrayCore) Running() bool {
	x.mu.Lock()
	defer x.mu.Unlock()
	return x.instance != nil
}

// Start builds and starts a core instance in this process. onLog receives
// xray log lines on arbitrary goroutines; it should hop to the UI thread.
func (x *xrayCore) Start(cfgJSON []byte, onLog func(string)) error {
	x.mu.Lock()
	if x.instance != nil {
		x.mu.Unlock()
		return errors.New("已在运行中")
	}
	x.mu.Unlock()

	inst, err := startInstance(cfgJSON)
	if err != nil {
		return err
	}
	x.mu.Lock()
	x.instance = inst
	x.mu.Unlock()

	// xray registers its own log handler during Start; ours goes in after
	// it, so log lines land in the GUI.
	log.RegisterHandler(&guiHandler{fn: onLog})
	return nil
}

// startInstance builds and starts a core instance in this process. The
// caller owns it and closes it when done; the user instance and the
// temporary instance used by the speed test both go through here.
func startInstance(cfgJSON []byte) (*core.Instance, error) {
	pbConf, err := serial.DecodeJSONConfig(bytes.NewReader(cfgJSON))
	if err != nil {
		return nil, fmt.Errorf("解析配置: %w", err)
	}
	coreConf, err := pbConf.Build()
	if err != nil {
		return nil, fmt.Errorf("构建配置: %w", err)
	}
	inst, err := core.New(coreConf)
	if err != nil {
		return nil, fmt.Errorf("创建内核: %w", err)
	}
	if err := inst.Start(); err != nil {
		inst.Close()
		return nil, fmt.Errorf("启动内核: %w", err)
	}
	return inst, nil
}

// Stop closes the instance. Function call, not a kill.
func (x *xrayCore) Stop() error {
	x.mu.Lock()
	inst := x.instance
	x.instance = nil
	x.mu.Unlock()
	if inst == nil {
		return nil
	}
	return inst.Close()
}

// Traffic sums the in-process stats counters: total uplink and downlink bytes.
func (x *xrayCore) Traffic() (up, down int64) {
	x.mu.Lock()
	inst := x.instance
	x.mu.Unlock()
	if inst == nil {
		return 0, 0
	}
	m, ok := inst.GetFeature(stats.ManagerType()).(stats.Manager)
	if !ok {
		return 0, 0
	}
	m.VisitCounters(func(name string, c stats.Counter) bool {
		switch {
		case strings.HasSuffix(name, ">>>traffic>>>uplink"):
			up += c.Value()
		case strings.HasSuffix(name, ">>>traffic>>>downlink"):
			down += c.Value()
		}
		return true
	})
	return up, down
}
