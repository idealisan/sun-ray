package main

// protocols.go is a strategy-pattern registry: every outbound protocol
// describes itself once — which fields it needs in the UI and how to build
// its xray settings JSON. Adding a protocol means adding one entry here;
// the UI and the config builder stay untouched.

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// fieldSpec describes one UI field bound to a guiConfig member.
type fieldSpec struct {
	label       string
	placeholder string
	bindString  func(c *guiConfig) *string
	bindBool    func(c *guiConfig) *bool
	options     []string // non-nil renders a select/radios
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
	tlsOnly     bool // QUIC-based: security (TLS) applies, transport does not
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
var quicSecurities = []string{"none", "tls"}
var flowOptions = []string{"无", "xtls-rprx-vision"}
var vmessSecurities = []string{"auto", "aes-128-gcm", "chacha20-poly1305", "none", "zero"}
var domainStrategies = []string{"AsIs", "UseIP", "UseIPv4", "UseIPv6"}

// uTLS fingerprints xray-core supports (see transport/internet/tls config).
var fingerprints = []string{"chrome", "firefox", "safari", "ios", "android", "edge", "360", "qq", "random", "randomized"}

// needsAddress reports whether the protocol connects to a remote server.
func needsAddress(p string) bool {
	switch p {
	case "freedom", "blackhole", "dns", "loopback":
		return false
	}
	return true
}

// needsSecret reports whether the protocol takes a UUID / password / key.
func needsSecret(p string) bool {
	switch p {
	case "vless", "vmess", "trojan", "shadowsocks", "hysteria", "masque":
		return true
	}
	return false
}

// streamCapable reports whether streamSettings (transport + security) apply.
func streamCapable(p string) bool {
	switch p {
	case "vless", "vmess", "trojan", "shadowsocks":
		return true
	}
	return false
}

var protocolRegistry = map[string]protocolSpec{
	"vless": {
		name: "vless", needsAddr: true, needsSecret: true, secretLabel: "UUID", stream: true,
		fields: []fieldSpec{
			selectField("Flow", func(c *guiConfig) *string { return &c.Flow }, flowOptions),
			textField("加密（留空为 none）", "none", func(c *guiConfig) *string { return &c.VlessEncrypt }),
		},
		build: func(c *guiConfig, port int) (map[string]any, error) {
			enc := c.VlessEncrypt
			if enc == "" {
				enc = "none"
			}
			user := map[string]any{"id": c.Secret, "encryption": enc, "level": 8}
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
		fields: []fieldSpec{
			selectField("加密方式", func(c *guiConfig) *string { return &c.VMessSecurity }, vmessSecurities),
		},
		build: func(c *guiConfig, port int) (map[string]any, error) {
			sec := c.VMessSecurity
			if sec == "" {
				sec = "auto"
			}
			return map[string]any{"vnext": []any{map[string]any{
				"address": c.Address, "port": port,
				"users": []any{map[string]any{"id": c.Secret, "security": sec, "alterId": 0, "level": 8}},
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
		name: "socks", needsAddr: true,
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
		name: "http", needsAddr: true,
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

// UDP-native or special protocols; registered in init to keep the map literal flat.
func init() {
	protocolRegistry["wireguard"] = protocolSpec{
		name: "wireguard", needsAddr: true, needsSecret: true, secretLabel: "本机私钥",
		fields: []fieldSpec{
			textField("对端公钥", "", func(c *guiConfig) *string { return &c.PeerKey }),
			textField("预共享密钥（可选）", "", func(c *guiConfig) *string { return &c.WGPreSharedKey }),
			textField("本机地址（逗号分隔）", "172.16.0.2/32", func(c *guiConfig) *string { return &c.WGLocalAddr }),
			textField("KeepAlive 秒（可选）", "0", func(c *guiConfig) *string { return &c.WGKeepAlive }),
			textField("MTU", "1420", func(c *guiConfig) *string { return &c.WGMTU }),
		},
		build: func(c *guiConfig, port int) (map[string]any, error) {
			if strings.TrimSpace(c.PeerKey) == "" {
				return nil, errors.New("WireGuard 对端公钥不能为空")
			}
			local := c.WGLocalAddr
			if local == "" {
				local = "172.16.0.2/32"
			}
			peer := map[string]any{
				"publicKey": c.PeerKey,
				"endpoint":  fmt.Sprintf("%s:%d", c.Address, port),
			}
			if c.WGPreSharedKey != "" {
				peer["preSharedKey"] = c.WGPreSharedKey
			}
			if c.WGKeepAlive != "" {
				if ka, err := strconv.Atoi(c.WGKeepAlive); err == nil && ka > 0 {
					peer["keepAlive"] = ka
				}
			}
			mtu := 1420
			if c.WGMTU != "" {
				if m, err := strconv.Atoi(c.WGMTU); err == nil && m > 0 {
					mtu = m
				}
			}
			return map[string]any{
				"secretKey": c.Secret,
				"address":   strings.Split(local, ","),
				"peers":     []any{peer},
				"mtu":       mtu,
			}, nil
		},
	}

	protocolRegistry["hysteria"] = protocolSpec{
		name: "hysteria", needsAddr: true, needsSecret: true, secretLabel: "认证密钥", tlsOnly: true,
		build: func(c *guiConfig, port int) (map[string]any, error) {
			// hysteria2: auth travels in the hysteria transport settings, not here.
			return map[string]any{"version": 2, "address": c.Address, "port": port}, nil
		},
	}
	protocolRegistry["masque"] = protocolSpec{
		name: "masque", needsAddr: true, tlsOnly: true,
		fields: []fieldSpec{
			textField("远程 DNS（逗号分隔，可选）", "8.8.8.8,1.1.1.1", func(c *guiConfig) *string { return &c.RemoteDNS }),
		},
		build: func(c *guiConfig, port int) (map[string]any, error) {
			s := map[string]any{"address": c.Address, "port": port}
			if c.RemoteDNS != "" {
				s["remoteDNS"] = strings.Split(c.RemoteDNS, ",")
			}
			return s, nil
		},
	}

	protocolRegistry["freedom"] = protocolSpec{
		name: "freedom",
		fields: []fieldSpec{
			selectField("域名解析策略", func(c *guiConfig) *string { return &c.FreedomDomainStrategy }, domainStrategies),
		},
		build: func(c *guiConfig, port int) (map[string]any, error) {
			s := map[string]any{}
			if c.FreedomDomainStrategy != "" {
				s["domainStrategy"] = c.FreedomDomainStrategy
			}
			return s, nil
		},
	}
	protocolRegistry["blackhole"] = protocolSpec{
		name: "blackhole", build: func(c *guiConfig, port int) (map[string]any, error) { return map[string]any{}, nil },
	}
	protocolRegistry["dns"] = protocolSpec{
		name: "dns", build: func(c *guiConfig, port int) (map[string]any, error) { return map[string]any{}, nil },
	}
	protocolRegistry["loopback"] = protocolSpec{
		name: "loopback",
		fields: []fieldSpec{
			textField("入站标签", "mixed-in", func(c *guiConfig) *string { return &c.InboundTag }),
		},
		build: func(c *guiConfig, port int) (map[string]any, error) {
			tag := c.InboundTag
			if tag == "" {
				tag = "mixed-in"
			}
			return map[string]any{"inboundTag": tag}, nil
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
		s["security"] = "tls"
		s["tlsSettings"] = c.buildTLSSettings()
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

// buildStreamQUIC renders streamSettings for QUIC-native protocols:
// hysteria and masque ride their own transport with auth in the
// transport settings, plus TLS on top.
func (c *guiConfig) buildStreamQUIC() map[string]any {
	if c.Security != "tls" {
		return nil
	}
	s := map[string]any{"security": "tls", "tlsSettings": c.buildTLSSettings()}
	switch c.Protocol {
	case "hysteria":
		s["network"] = "hysteria"
		s["hysteriaSettings"] = map[string]any{"version": 2, "auth": c.Secret}
	case "masque":
		s["network"] = "masque"
		ms := map[string]any{}
		if c.Secret != "" {
			ms["pass"] = c.Secret
		}
		s["masqueSettings"] = ms
	}
	return s
}

// buildTLSSettings renders tlsSettings shared by the TLS and REALITY paths.
func (c *guiConfig) buildTLSSettings() map[string]any {
	sni := c.SNI
	if sni == "" {
		sni = c.Address
	}
	tls := map[string]any{"serverName": sni}
	if c.ALPN != "" {
		tls["alpn"] = strings.Split(c.ALPN, ",")
	}
	if c.Fingerprint != "" {
		tls["fingerprint"] = c.Fingerprint
	}
	return tls
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
	if s.tlsOnly && c.Security != "tls" {
		return nil, errors.New(c.Protocol + " 基于 QUIC，需要在安全中选择 TLS")
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
	if s.tlsOnly {
		if st := c.buildStreamQUIC(); st != nil {
			o["streamSettings"] = st
		}
	}
	return o, nil
}
