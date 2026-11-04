package main

// core.go embeds xray-core as a library: the GUI process and the proxy core
// are one single binary, talking through function calls — no subprocess, no
// command line. Start/stop call core.Instance directly, logs come through
// log.RegisterHandler, traffic counters through the stats manager.
// Protocol-specific config building lives in protocols.go.

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"

	"github.com/xtls/xray-core/common/log"
	"github.com/xtls/xray-core/core"
	"github.com/xtls/xray-core/features/stats"
	"github.com/xtls/xray-core/infra/conf/serial"
	_ "github.com/xtls/xray-core/main/distro/all" // register every protocol and feature
)

// guiConfig is everything the UI edits; protocolSpec binds into it.
type guiConfig struct {
	LogLevel string
	Sniffing bool // sniff inbound connection domains, needed for domain routing

	SocksPort string // local SOCKS5 port, e.g. "10808"
	HTTPPort  string // local HTTP port, e.g. "10809"
	AllowLAN  bool   // listen on 0.0.0.0 instead of 127.0.0.1

	RouteCN  bool // direct traffic to mainland China sites/IPs
	RouteAds bool // block ad domains

	Protocol string // key into protocolRegistry
	Address  string // server address
	Port     string // server port
	Secret   string // UUID / password / auth / secretKey
	Method   string // shadowsocks cipher
	User     string // socks / http outbound user
	Pass     string // socks / http outbound password
	PeerKey  string // wireguard peer public key
	Flow     string // vless flow, "" or xtls-rprx-vision

	Network string // transport: tcp, ws, grpc, httpupgrade
	Path    string // ws / httpupgrade path
	Host    string // ws / httpupgrade Host header
	Service string // grpc serviceName

	Security      string // none, tls, reality
	SNI           string
	ALPN          string // comma separated, e.g. "h2,http/1.1"
	Fingerprint   string // uTLS fingerprint: chrome, firefox, ...
	AllowInsecure bool
	PublicKey     string // reality
	ShortID       string // reality
	SpiderX       string // reality
}

func defaultConfig() guiConfig {
	return guiConfig{
		LogLevel: "info", Sniffing: true,
		SocksPort: "10808", HTTPPort: "10809",
		Protocol: "vless", Port: "443", Method: "aes-256-gcm",
		Network: "tcp", Security: "none", Fingerprint: "chrome", SpiderX: "/",
	}
}

// buildJSON renders the GUI fields into a full xray JSON config, with stats
// and policy enabled so the UI can read traffic counters.
func (c *guiConfig) buildJSON() ([]byte, error) {
	socksPort, err := strconv.Atoi(c.SocksPort)
	if err != nil || socksPort < 1 || socksPort > 65535 {
		return nil, fmt.Errorf("SOCKS 端口无效: %q", c.SocksPort)
	}
	httpPort, err := strconv.Atoi(c.HTTPPort)
	if err != nil || httpPort < 1 || httpPort > 65535 {
		return nil, fmt.Errorf("HTTP 端口无效: %q", c.HTTPPort)
	}
	listen := "127.0.0.1"
	if c.AllowLAN {
		listen = "0.0.0.0"
	}

	proxy, err := c.buildOutbound()
	if err != nil {
		return nil, err
	}

	// Simple routing: private ranges always go direct; presets add more.
	rules := []any{
		map[string]any{"type": "field", "ip": []string{"geoip:private"}, "outboundTag": "direct"},
	}
	outbounds := []any{proxy, map[string]any{"protocol": "freedom", "tag": "direct"}}
	if c.RouteAds {
		rules = append(rules, map[string]any{"type": "field", "domain": []string{"geosite:category-ads-all"}, "outboundTag": "block"})
		outbounds = append(outbounds, map[string]any{"protocol": "blackhole", "tag": "block"})
	}
	if c.RouteCN {
		rules = append(rules,
			map[string]any{"type": "field", "domain": []string{"geosite:cn"}, "outboundTag": "direct"},
			map[string]any{"type": "field", "ip": []string{"geoip:cn"}, "outboundTag": "direct"},
		)
	}

	sniffing := map[string]any{"enabled": false, "destOverride": []string{"http", "tls"}}
	if c.Sniffing {
		sniffing["enabled"] = true
	}

	cfg := map[string]any{
		"log":       map[string]any{"loglevel": c.LogLevel},
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
			map[string]any{
				"tag": "socks-in", "listen": listen, "port": socksPort,
				"protocol": "socks", "sniffing": sniffing,
				"settings": map[string]any{"auth": "noauth", "udp": true},
			},
			map[string]any{
				"tag": "http-in", "listen": listen, "port": httpPort,
				"protocol": "http", "sniffing": sniffing,
				"settings": map[string]any{"allowTransparent": false},
			},
		},
	}
	return json.MarshalIndent(cfg, "", "  ")
}

// guiHandler turns xray log messages into lines for the UI.
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

	pbConf, err := serial.DecodeJSONConfig(bytes.NewReader(cfgJSON))
	if err != nil {
		return fmt.Errorf("解析配置: %w", err)
	}
	coreConf, err := pbConf.Build()
	if err != nil {
		return fmt.Errorf("构建配置: %w", err)
	}
	inst, err := core.New(coreConf)
	if err != nil {
		return fmt.Errorf("创建内核: %w", err)
	}
	if err := inst.Start(); err != nil {
		inst.Close()
		return fmt.Errorf("启动内核: %w", err)
	}
	x.mu.Lock()
	x.instance = inst
	x.mu.Unlock()

	// xray registers its own log handler during Start; ours goes in after
	// it, so log lines land in the GUI.
	log.RegisterHandler(&guiHandler{fn: onLog})
	return nil
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
