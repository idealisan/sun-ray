package main

// sharelink.go encodes a server as a share URL and decodes the app's own
// sunray:// form. Protocols with a community-standard share link use that
// format, so the copied URL works in other clients too. Protocols without
// one use sunray:// + Base64URL(JSON) so a copy can still be pasted back
// into this app.

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
)

const sunrayFormat = "sunray/1"

// sunrayOnly are the protocols with no community-standard share link.
var sunrayOnly = map[string]bool{
	"wireguard": true, "masque": true, "freedom": true,
	"blackhole": true, "dns": true, "loopback": true,
}

// shareLink encodes one server as a share URL.
func shareLink(s serverConfig) (string, error) {
	if sunrayOnly[s.Protocol] {
		return encodeSunrayLink(s)
	}
	switch s.Protocol {
	case "vless", "vmess", "trojan", "shadowsocks", "hysteria", "socks", "http":
		return encodeStandardLink(s)
	default:
		return "", fmt.Errorf("协议 %q 不支持导出为链接", s.Protocol)
	}
}

// linkHost builds a host:port, bracketing IPv6 addresses.
func linkHost(s serverConfig) string {
	host := s.Address
	if strings.Contains(host, ":") && !strings.HasPrefix(host, "[") {
		host = "[" + host + "]"
	}
	return net.JoinHostPort(host, s.Port)
}

// linkFragment escapes a server name for the URL fragment. QueryEscape
// encodes a space as "+", which the fragment readers (u.Fragment and
// PathUnescape) keep literally, so spaces use %20 instead.
func linkFragment(name string) string {
	return strings.ReplaceAll(url.QueryEscape(name), "+", "%20")
}

// addParam adds a query parameter when the value is not empty.
func addParam(q url.Values, key, value string) {
	if value != "" {
		q.Set(key, value)
	}
}

// boolParam writes "1" for true, matching subscriptionBool.
func boolParam(q url.Values, key string, on bool) {
	if on {
		q.Set(key, "1")
	}
}

func encodeStandardLink(s serverConfig) (string, error) {
	if strings.TrimSpace(s.Address) == "" {
		return "", errors.New("服务器地址为空")
	}
	if strings.TrimSpace(s.Port) == "" {
		return "", errors.New("服务器端口为空")
	}
	q := url.Values{}
	addParam(q, "type", s.Network)
	addParam(q, "security", s.Security)
	addParam(q, "sni", s.SNI)
	addParam(q, "alpn", s.ALPN)
	addParam(q, "fp", s.Fingerprint)
	boolParam(q, "allowInsecure", s.AllowInsecure)
	addParam(q, "path", s.Path)
	addParam(q, "host", s.Host)
	addParam(q, "serviceName", s.Service)
	addParam(q, "flow", s.Flow)
	addParam(q, "encryption", s.VlessEncrypt)
	addParam(q, "pbk", s.PublicKey)
	addParam(q, "sid", s.ShortID)
	addParam(q, "spx", s.SpiderX)

	fragment := linkFragment(s.Name)
	var raw string
	switch s.Protocol {
	case "vless":
		if s.Secret == "" {
			return "", errors.New("VLESS 缺少 UUID")
		}
		raw = fmt.Sprintf("vless://%s@%s", url.User(s.Secret), linkHost(s))
	case "trojan":
		if s.Secret == "" {
			return "", errors.New("Trojan 缺少密码")
		}
		raw = fmt.Sprintf("trojan://%s@%s", url.User(s.Secret), linkHost(s))
	case "hysteria":
		if s.Secret == "" {
			return "", errors.New("Hysteria2 缺少密码")
		}
		raw = fmt.Sprintf("hysteria2://%s@%s", url.User(s.Secret), linkHost(s))
	case "socks":
		user := url.User(s.User)
		if s.Pass != "" {
			user = url.UserPassword(s.User, s.Pass)
		}
		raw = fmt.Sprintf("socks://%s@%s", user, linkHost(s))
	case "http":
		user := url.User(s.User)
		if s.Pass != "" {
			user = url.UserPassword(s.User, s.Pass)
		}
		raw = fmt.Sprintf("http://%s@%s", user, linkHost(s))
	case "shadowsocks":
		if s.Method == "" || s.Secret == "" {
			return "", errors.New("Shadowsocks 缺少加密方法或密码")
		}
		userinfo := base64.RawURLEncoding.EncodeToString([]byte(s.Method + ":" + s.Secret))
		raw = fmt.Sprintf("ss://%s@%s", userinfo, linkHost(s))
	case "vmess":
		return encodeVMessLink(s)
	}
	if enc := q.Encode(); enc != "" {
		raw += "?" + enc
	}
	if fragment != "" {
		raw += "#" + fragment
	}
	return raw, nil
}

// encodeVMessLink uses the v2rayN "ver 2" Base64(JSON) form, the format
// parseVMess reads back.
func encodeVMessLink(s serverConfig) (string, error) {
	if s.Secret == "" {
		return "", errors.New("VMess 缺少 UUID")
	}
	m := map[string]any{
		"v": "2", "ps": s.Name, "add": s.Address, "port": s.Port, "id": s.Secret,
		"aid": s.VMessAlterID, "scy": s.VMessSecurity, "net": s.Network,
		"host": s.Host, "path": s.Path, "tls": s.Security, "sni": s.SNI,
		"alpn": s.ALPN, "fp": s.Fingerprint, "pbk": s.PublicKey,
		"sid": s.ShortID, "spx": s.SpiderX, "serviceName": s.Service,
	}
	if s.AllowInsecure {
		m["allowInsecure"] = true
	}
	data, err := json.Marshal(m)
	if err != nil {
		return "", err
	}
	return "vmess://" + base64.StdEncoding.EncodeToString(data), nil
}

// encodeSunrayLink carries the whole server as Base64URL(JSON), dropping the
// fields that belong to this install only. The format tag lets the parser
// reject payloads that are not a sun-ray link.
func encodeSunrayLink(s serverConfig) (string, error) {
	data, err := json.Marshal(s)
	if err != nil {
		return "", err
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		return "", err
	}
	delete(m, "id")
	delete(m, "subscriptionId")
	m["format"] = sunrayFormat
	payload, err := json.Marshal(m)
	if err != nil {
		return "", err
	}
	return "sunray://" + base64.RawURLEncoding.EncodeToString(payload), nil
}

// parseSunrayLink decodes a sunray:// link back into a server. IDs are left
// empty so the importer assigns fresh ones.
func parseSunrayLink(raw string) (serverConfig, error) {
	payload := strings.TrimSpace(raw[len("sunray://"):])
	decoded, err := base64.RawURLEncoding.DecodeString(payload)
	if err != nil {
		return serverConfig{}, errors.New("sunray 链接内容解码失败")
	}
	var m map[string]any
	if err := json.Unmarshal(decoded, &m); err != nil {
		return serverConfig{}, errors.New("sunray 链接内容无效")
	}
	if format, _ := m["format"].(string); format != sunrayFormat {
		return serverConfig{}, errors.New("sunray 链接版本不支持")
	}
	delete(m, "format")
	data, err := json.Marshal(m)
	if err != nil {
		return serverConfig{}, err
	}
	var s serverConfig
	if err := json.Unmarshal(data, &s); err != nil {
		return serverConfig{}, err
	}
	if s.Protocol == "" {
		return serverConfig{}, errors.New("sunray 链接缺少协议")
	}
	s.ID = ""
	s.SubscriptionID = ""
	return s, nil
}

// shareLinksForServers encodes servers as one share URL per line. A server
// that cannot be exported is reported in the error and skipped, so a mixed
// selection still yields a usable clipboard.
func shareLinksForServers(servers []serverConfig) (string, []string) {
	var links, failures []string
	for _, s := range servers {
		link, err := shareLink(s)
		if err != nil {
			failures = append(failures, s.displayName()+": "+err.Error())
			continue
		}
		links = append(links, link)
	}
	return strings.Join(links, "\n"), failures
}
