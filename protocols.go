package main

// protocols.go is a strategy-pattern registry: every outbound protocol
// describes itself once — which fields it needs in the UI and how to build
// its xray settings JSON. Adding a protocol means adding one entry here;
// the UI and the config builder stay untouched.

import (
	"fmt"
	"strings"
)

// fieldSpec describes one UI field bound to a guiConfig member.
type fieldSpec struct {
	label       string
	placeholder string
	bindString  func(c *guiConfig) *string
	bindBool    func(c *guiConfig) *bool
	options     []string // non-nil renders a select
}

func textField(label, placeholder string, bind func(c *guiConfig) *string) fieldSpec {
	return fieldSpec{label: label, placeholder: placeholder, bindString: bind}
}

func selectField(label string, bind func(c *guiConfig) *string, options []string) fieldSpec {
	return fieldSpec{label: label, bindString: bind, options: options}
}

func checkField(label string, bind func(c *guiConfig) *bool) fieldSpec {
	return fieldSpec{label: label, bindBool: bind}
}

// protocolSpec is the per-protocol strategy: what the UI shows and how the
// outbound settings JSON is built.
type protocolSpec struct {
	name        string
	needsAddr   bool // show address + port fields
	needsSecret bool
	secretLabel string
	stream      bool // transport + security (streamSettings) apply
	fields      []fieldSpec
	build       func(c *guiConfig, port int) (map[string]any, error)
}

// every protocol listed here must exist in the xray-core build; kept in
// sync with the outbound loader in xray-core/infra/conf/xray.go.
var protocolList = []string{
	"vless", "vmess", "trojan", "shadowsocks", "socks", "http",
	"wireguard", "hysteria", "masque", "freedom", "blackhole", "dns", "loopback",
}

var logLevels = []string{"debug", "info", "warning", "error"}
var networks = []string{"tcp", "ws", "grpc", "httpupgrade"}
var securities = []string{"none", "tls", "reality"}
var flowOptions = []string{"无", "xtls-rprx-vision"}

// simpleSettings is the common shape for protocols with no parameters.
func simpleSettings(protocol string) func(c *guiConfig, port int) (map[string]any, error) {
	return func(c *guiConfig, port int) (map[string]any, error) {
		return map[string]any{}, nil
	}
}

var protocolRegistry = map[string]protocolSpec{
	"vless": {
		name: "vless", needsAddr: true, needsSecret: true, secretLabel: "UUID", stream: true,
		fields: []fieldSpec{
			selectField("Flow", func(c *guiConfig) *string { return &c.Flow }, flowOptions),
		},
		build: func(c *guiConfig, port int) (map[string]any, error) {
			user := map[string]any{"id": c.Secret, "encryption": "none", "level": 8}
			if c.Flow == "xtls-rprx-vision" {
				user["flow"] = c.Flow
			}
			return map[string]any{"vnext": []any{map[string]any{
				"address": c.Address, "port": port, "users": []any{user},
			}}}, nil
		},
	},
	"vmess": {
		name: "vmess", needsAddr: true, needsSecret: true, secretLabel: "UUID", stream: true,
		build: func(c *guiConfig, port int) (map[string]any, error) {
			return map[string]any{"vnext": []any{map[string]any{
				"address": c.Address, "port": port,
				"users": []any{map[string]any{"id": c.Secret, "security": "auto", "alterId": 0, "level": 8}},
			}}}, nil
		},
	},
	"trojan": {
		name: "trojan", needsAddr: true, needsSecret: true, secretLabel: "密码", stream: true,
		build: func(c *guiConfig, port int) (map[string]any, error) {
			return map[string]any{"servers": []any{map[string]any{
				"address": c.Address, "port": port, "password": c.Secret, "level": 8,
			}}}, nil
		},
	},
	"shadowsocks": {
		name: "shadowsocks", needsAddr: true, needsSecret: true, secretLabel: "密码", stream: true,
		fields: []fieldSpec{
			textField("加密方法", "aes-256-gcm", func(c *guiConfig) *string { return &c.Method }),
		},
		build: func(c *guiConfig, port int) (map[string]any, error) {
			return map[string]any{"servers": []any{map[string]any{
				"address": c.Address, "port": port,
				"method": c.Method, "password": c.Secret, "level": 8,
			}}}, nil
		},
	},
	"socks": {
		name: "socks", needsAddr: true, stream: true,
		fields: []fieldSpec{
			textField("用户名（可选）", "", func(c *guiConfig) *string { return &c.User }),
			textField("密码（可选）", "", func(c *guiConfig) *string { return &c.Pass }),
		},
		build: func(c *guiConfig, port int) (map[string]any, error) {
			server := map[string]any{"address": c.Address, "port": port}
			if c.User != "" {
				server["users"] = []any{map[string]any{"user": c.User, "pass": c.Pass, "level": 8}}
			}
			return map[string]any{"servers": []any{server}}, nil
		},
	},
	"http": {
		name: "http", needsAddr: true, stream: true,
		fields: []fieldSpec{
			textField("用户名（可选）", "", func(c *guiConfig) *string { return &c.User }),
			textField("密码（可选）", "", func(c *guiConfig) *string { return &c.Pass }),
		},
		build: func(c *guiConfig, port int) (map[string]any, error) {
			server := map[string]any{"address": c.Address, "port": port}
			if c.User != "" {
				server["users"] = []any{map[string]any{"user": c.User, "pass": c.Pass, "level": 8}}
			}
			return map[string]any{"servers": []any{server}}, nil
		},
	},
}

// UDP-native or special protocols: no streamSettings.
func init() {
	protocolRegistry["wireguard"] = protocolSpec{
		name: "wireguard", needsAddr: true, needsSecret: true, secretLabel: "本机私钥",
		fields: []fieldSpec{
			textField("对端公钥", "", func(c *guiConfig) *string { return &c.PeerKey }),
		},
		build: func(c *guiConfig, port int) (map[string]any, error) {
			if strings.TrimSpace(c.PeerKey) == "" {
				return nil, fmt.Errorf("WireGuard 对端公钥不能为空")
			}
			return map[string]any{
				"secretKey": c.Secret,
				"address":   []string{"172.16.0.2/32"},
				"peers": []any{map[string]any{
					"publicKey": c.PeerKey,
					"endpoint":  fmt.Sprintf("%s:%d", c.Address, port),
				}},
				"mtu": 1420,
			}, nil
		},
	}

	protocolRegistry["hysteria"] = protocolSpec{
		name: "hysteria", needsAddr: true, needsSecret: true, secretLabel: "认证密钥",
		build: func(c *guiConfig, port int) (map[string]any, error) {
			return map[string]any{
				"version": 2, "address": c.Address, "port": port,
				"users": []any{map[string]any{"auth": c.Secret, "level": 8}},
			}, nil
		},
	}

	protocolRegistry["masque"] = protocolSpec{
		name: "masque", needsAddr: true,
		fields: []fieldSpec{
			textField("密码（可选）", "", func(c *guiConfig) *string { return &c.Secret }),
		},
		build: func(c *guiConfig, port int) (map[string]any, error) {
			s := map[string]any{"address": c.Address, "port": port}
			if c.Secret != "" {
				s["users"] = []any{map[string]any{"pass": c.Secret, "level": 8}}
			}
			return s, nil
		},
	}

	protocolRegistry["freedom"] = protocolSpec{
		name: "freedom", build: simpleSettings("freedom"),
	}
	protocolRegistry["blackhole"] = protocolSpec{
		name: "blackhole", build: simpleSettings("blackhole"),
	}
	protocolRegistry["dns"] = protocolSpec{
		name: "dns", build: simpleSettings("dns"),
	}
	protocolRegistry["loopback"] = protocolSpec{
		name: "loopback",
		build: func(c *guiConfig, port int) (map[string]any, error) {
			return map[string]any{"inboundTag": "socks-in"}, nil
		},
	}
}

// spec returns the strategy for a protocol name.
func spec(protocol string) protocolSpec {
	s, ok := protocolRegistry[protocol]
	if !ok {
		panic("unregistered protocol: " + protocol)
	}
	return s
}

// buildStream renders streamSettings for the chosen transport and security;
// only meaningful for stream-capable protocols.
func (c *guiConfig) buildStream() map[string]any {
	if c.Network == "tcp" && c.Security == "none" {
		return nil
	}
	s := map[string]any{"network": c.Network}
	switch c.Network {
	case "ws":
		s["wsSettings"] = map[string]any{"path": c.Path, "headers": map[string]any{"Host": c.Host}}
	case "grpc":
		s["grpcSettings"] = map[string]any{"serviceName": c.Service, "authority": c.Host}
	case "httpupgrade":
		s["httpupgradeSettings"] = map[string]any{"path": c.Path, "host": c.Host}
	}
	switch c.Security {
	case "tls":
		sni := c.SNI
		if sni == "" {
			sni = c.Address
		}
		tls := map[string]any{"serverName": sni, "allowInsecure": c.AllowInsecure}
		if c.ALPN != "" {
			tls["alpn"] = strings.Split(c.ALPN, ",")
		}
		if c.Fingerprint != "" {
			tls["fingerprint"] = c.Fingerprint
		}
		s["security"] = "tls"
		s["tlsSettings"] = tls
	case "reality":
		sni := c.SNI
		if sni == "" {
			sni = c.Address
		}
		s["security"] = "reality"
		s["realitySettings"] = map[string]any{
			"show": true, "serverName": sni, "publicKey": c.PublicKey,
			"shortId": c.ShortID, "spiderX": c.SpiderX, "fingerprint": c.Fingerprint,
		}
	}
	return s
}

// buildOutbound assembles the proxy outbound from the active protocol spec.
func (c *guiConfig) buildOutbound() (map[string]any, error) {
	s := spec(c.Protocol)
	o := map[string]any{"tag": "proxy", "protocol": c.Protocol}

	port := 0
	if s.needsAddr {
		p, err := strconv.Atoi(c.Port)
		if err != nil || p < 1 || p > 65535 {
			return nil, fmt.Errorf("服务器端口无效: %q", c.Port)
		}
		port = p
		if strings.TrimSpace(c.Address) == "" {
			return nil, errors.New("服务器地址不能为空")
		}
	}
	if s.needsSecret && strings.TrimSpace(c.Secret) == "" {
		return nil, errors.New(s.secretLabel + "不能为空")
	}

	settings, err := s.build(c, port)
	if err != nil {
		return nil, err
	}
	o["settings"] = settings

	if s.stream {
		if st := c.buildStream(); st != nil {
			o["streamSettings"] = st
		}
	}
	return o, nil
}
