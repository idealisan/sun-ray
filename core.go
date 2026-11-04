package main

// core.go embeds xray-core as a library: the GUI process and the proxy core
// are one single binary, talking through function calls — no subprocess, no
// command line. Start/stop call core.Instance directly, logs come through
// log.RegisterHandler, traffic counters through the stats manager.

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
	_ "github.com/xtls/xray-core/main/distro/all" // register every protocol and feature
	"github.com/xtls/xray-core/infra/conf/serial"
)

// guiConfig is everything the UI edits.
type guiConfig struct {
	SocksPort string // local SOCKS5 port, e.g. "10808"
	HTTPPort  string // local HTTP port, e.g. "10809"
	AllowLAN  bool   // listen on 0.0.0.0 instead of 127.0.0.1

	Protocol string // vless, vmess, trojan, shadowsocks, freedom
	Address  string // server address
	Port     string // server port
	Secret   string // UUID or password
	Method   string // shadowsocks cipher
	UseTLS   bool
	SNI      string
}

func defaultConfig() guiConfig {
	return guiConfig{
		SocksPort: "10808", HTTPPort: "10809",
		Protocol: "vless", Port: "443", Method: "aes-256-gcm",
	}
}

var protocols = []string{"vless", "vmess", "trojan", "shadowsocks", "freedom"}

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

	cfg := map[string]any{
		"log":      map[string]any{"loglevel": "info"},
		"outbounds": []any{proxy, map[string]any{"protocol": "freedom", "tag": "direct"}},
		"stats":    map[string]any{},
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
				"protocol": "socks",
				"settings": map[string]any{"auth": "noauth", "udp": true},
			},
			map[string]any{
				"tag": "http-in", "listen": listen, "port": httpPort,
				"protocol": "http",
				"settings": map[string]any{"allowTransparent": false},
			},
		},
	}
	return json.MarshalIndent(cfg, "", "  ")
}

func (c *guiConfig) buildOutbound() (map[string]any, error) {
	o := map[string]any{"tag": "proxy"}
	if c.Protocol == "freedom" {
		o["protocol"] = "freedom"
		o["settings"] = map[string]any{}
		return o, nil
	}
	serverPort, err := strconv.Atoi(c.Port)
	if err != nil || serverPort < 1 || serverPort > 65535 {
		return nil, fmt.Errorf("服务器端口无效: %q", c.Port)
	}
	if strings.TrimSpace(c.Address) == "" {
		return nil, errors.New("服务器地址不能为空")
	}
	if strings.TrimSpace(c.Secret) == "" {
		return nil, errors.New("UUID / 密码不能为空")
	}
	switch c.Protocol {
	case "vless":
		o["protocol"] = "vless"
		o["settings"] = map[string]any{"vnext": []any{map[string]any{
			"address": c.Address, "port": serverPort,
			"users": []any{map[string]any{"id": c.Secret, "encryption": "none", "level": 8}},
		}}}
	case "vmess":
		o["protocol"] = "vmess"
		o["settings"] = map[string]any{"vnext": []any{map[string]any{
			"address": c.Address, "port": serverPort,
			"users": []any{map[string]any{"id": c.Secret, "security": "auto", "alterId": 0, "level": 8}},
		}}}
	case "trojan":
		o["protocol"] = "trojan"
		o["settings"] = map[string]any{"servers": []any{map[string]any{
			"address": c.Address, "port": serverPort, "password": c.Secret, "level": 8,
		}}}
	case "shadowsocks":
		o["protocol"] = "shadowsocks"
		o["settings"] = map[string]any{"servers": []any{map[string]any{
			"address": c.Address, "port": serverPort,
			"method": c.Method, "password": c.Secret, "level": 8,
		}}}
	default:
		return nil, fmt.Errorf("不支持的协议: %q", c.Protocol)
	}
	if c.UseTLS {
		sni := c.SNI
		if sni == "" {
			sni = c.Address
		}
		o["streamSettings"] = map[string]any{
			"network": "tcp", "security": "tls",
			"tlsSettings": map[string]any{"serverName": sni, "allowInsecure": false},
		}
	}
	return o, nil
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
// xray's log lines on arbitrary goroutines; it should hop to the UI thread.
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

	// xray's own log app registers its handler during Start; ours goes in
	// after it, so log lines land in the GUI.
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
