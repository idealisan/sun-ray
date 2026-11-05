//go:build !nogui

package main

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode"
)

const (
	subscriptionBodyLimit = 10 << 20
	subscriptionTimeout   = 30 * time.Second
)

func (a *app) updateSelectedSubscriptions() {
	ids := a.selectedSubscriptionIDs()
	if len(ids) == 0 {
		for i := range a.cfg.Subscriptions {
			ids = append(ids, a.cfg.Subscriptions[i].ID)
		}
	}
	a.updateSubscriptions(ids)
}

func (a *app) updateSubscriptions(ids []string) {
	if len(a.subUpdating) > 0 {
		return
	}
	subs := make([]subscriptionConfig, 0, len(ids))
	for _, id := range ids {
		if s, ok := a.subscriptionByID(id); ok {
			subs = append(subs, *s)
		}
	}
	if len(subs) == 0 {
		return
	}
	a.subUpdating = make(map[string]bool, len(subs))
	for _, s := range subs {
		a.subUpdating[s.ID] = true
	}
	go func() {
		for _, s := range subs {
			nodes, err := fetchSubscriptionNodes(s)
			a.updateUI(func() {
				if cur, ok := a.subscriptionByID(s.ID); ok {
					if err != nil {
						cur.LastError = err.Error()
						a.addLog("[订阅] " + cur.displayName() + " 更新失败: " + err.Error())
					} else {
						a.applySubscriptionNodes(cur, nodes)
						a.addLog(fmt.Sprintf("[订阅] %s 已更新：%d 个节点", cur.displayName(), len(nodes)))
					}
				}
				delete(a.subUpdating, s.ID)
				a.saver.maybeSave(a.cfg, true)
			})
		}
	}()
}

func (a *app) updateUI(fn func()) {
	if a.win == nil {
		fn()
		return
	}
	a.win.Update(fn)
}

func (a *app) applySubscriptionNodes(sub *subscriptionConfig, nodes []serverConfig) {
	old := make(map[string]serverConfig)
	for _, s := range a.cfg.Servers {
		if s.SubscriptionID == sub.ID {
			old[nodeKey(s)] = s
		}
	}

	imported := make([]serverConfig, 0, len(nodes))
	used := make(map[string]bool, len(nodes))
	for _, n := range nodes {
		key := nodeKey(n)
		if prev, ok := old[key]; ok && !used[key] {
			n.ID = prev.ID
			used[key] = true
		} else {
			n.ID = newServerID()
		}
		n.SubscriptionID = sub.ID
		imported = append(imported, n)
	}

	kept := a.cfg.Servers[:0]
	for _, s := range a.cfg.Servers {
		if s.SubscriptionID != sub.ID {
			kept = append(kept, s)
		}
	}
	a.cfg.Servers = append(kept, imported...)

	live := make(map[string]bool, len(a.cfg.Servers))
	for _, s := range a.cfg.Servers {
		live[s.ID] = true
	}
	for id := range a.cfg.Results {
		if !live[id] {
			delete(a.cfg.Results, id)
		}
	}
	if _, ok := a.serverByID(a.cfg.ActiveID); !ok {
		if len(a.cfg.Servers) > 0 {
			a.cfg.ActiveID = a.cfg.Servers[0].ID
		} else {
			s := defaultServerConfig()
			a.cfg.Servers = []serverConfig{s}
			a.cfg.ActiveID = s.ID
		}
	}

	sub.ServerCount = len(imported)
	sub.UpdatedAt = time.Now().Unix()
	sub.LastError = ""
	a.updateTrayTip()
}

func nodeKey(s serverConfig) string {
	return strings.Join([]string{
		s.Protocol, s.Address, s.Port, s.Secret, s.Method,
		s.Network, s.Path, s.Host, s.Service,
		s.Security, s.SNI, s.PublicKey, s.ShortID, s.Flow, s.VMessSecurity,
	}, "\x00")
}

func fetchSubscriptionNodes(sub subscriptionConfig) ([]serverConfig, error) {
	rawURL := strings.TrimSpace(sub.URL)
	u, err := url.Parse(rawURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, errors.New("订阅地址无效")
	}
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "SunRay/"+appVersion)
	req.Header.Set("Accept", "*/*")
	client := &http.Client{Timeout: subscriptionTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("HTTP %s", resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, subscriptionBodyLimit+1))
	if err != nil {
		return nil, err
	}
	if len(body) > subscriptionBodyLimit {
		return nil, errors.New("订阅内容超过 10 MB")
	}
	return parseSubscriptionBody(body)
}

func parseSubscriptionBody(body []byte) ([]serverConfig, error) {
	text := decodeSubscriptionText(body)
	if strings.TrimSpace(text) == "" {
		return nil, errors.New("订阅内容为空")
	}
	var nodes []serverConfig
	var parseErrors []string
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "//") {
			continue
		}
		if !strings.Contains(line, "://") {
			continue
		}
		s, err := parseShareLink(line)
		if err != nil {
			parseErrors = append(parseErrors, err.Error())
			continue
		}
		nodes = append(nodes, s)
	}
	if len(nodes) == 0 {
		if len(parseErrors) > 0 {
			return nil, fmt.Errorf("没有可用的节点：%s", strings.Join(parseErrors, "；"))
		}
		return nil, errors.New("订阅内容里没有节点")
	}
	return nodes, nil
}

func decodeSubscriptionText(body []byte) string {
	text := strings.TrimSpace(string(body))
	text = strings.TrimSpace(strings.TrimPrefix(text, "\ufeff"))
	if strings.Contains(text, "://") {
		return text
	}
	if decoded, err := decodeBase64Text(text); err == nil && strings.Contains(decoded, "://") {
		return decoded
	}
	return text
}

func decodeBase64Text(text string) (string, error) {
	compact := strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return -1
		}
		return r
	}, text)
	for _, enc := range []*base64.Encoding{
		base64.StdEncoding,
		base64.RawStdEncoding,
		base64.URLEncoding,
		base64.RawURLEncoding,
	} {
		if decoded, err := enc.DecodeString(compact); err == nil {
			return string(decoded), nil
		}
	}
	return "", errors.New("base64 解码失败")
}

func parseShareLink(line string) (serverConfig, error) {
	switch {
	case strings.HasPrefix(strings.ToLower(line), "vless://"):
		return parseVLESS(line)
	case strings.HasPrefix(strings.ToLower(line), "vmess://"):
		return parseVMess(line)
	case strings.HasPrefix(strings.ToLower(line), "trojan://"):
		return parseTrojan(line)
	case strings.HasPrefix(strings.ToLower(line), "ss://"):
		return parseShadowsocks(line)
	case strings.HasPrefix(strings.ToLower(line), "hysteria2://"),
		strings.HasPrefix(strings.ToLower(line), "hy2://"):
		return parseHysteria2(line)
	case strings.HasPrefix(strings.ToLower(line), "hysteria://"):
		return parseHysteria(line)
	case strings.HasPrefix(strings.ToLower(line), "socks://"),
		strings.HasPrefix(strings.ToLower(line), "socks5://"):
		return parseSocksHTTP(line, "socks")
	case strings.HasPrefix(strings.ToLower(line), "http://"):
		return parseSocksHTTP(line, "http")
	default:
		return serverConfig{}, errors.New("不支持的节点类型")
	}
}

func parseVLESS(raw string) (serverConfig, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" {
		return serverConfig{}, errors.New("VLESS 链接无效")
	}
	s := defaultServerConfig()
	s.ID = ""
	s.Protocol = "vless"
	s.Name = strings.TrimSpace(u.Fragment)
	s.Address = u.Hostname()
	if s.Port, err = subscriptionPort(u.Port(), "443"); err != nil {
		return serverConfig{}, err
	}
	if u.User != nil {
		s.Secret = u.User.Username()
	}
	if s.Secret == "" {
		return serverConfig{}, errors.New("VLESS 缺少 UUID")
	}
	q := u.Query()
	if s.Network, err = subscriptionNetwork(firstNonEmpty(q.Get("type"), q.Get("net"))); err != nil {
		return serverConfig{}, err
	}
	s.Security = subscriptionSecurity(q.Get("security"), "none")
	s.SNI = firstNonEmpty(q.Get("sni"), q.Get("serverName"), q.Get("peer"))
	s.ALPN = q.Get("alpn")
	s.Fingerprint = firstNonEmpty(q.Get("fp"), q.Get("fingerprint"), s.Fingerprint)
	s.AllowInsecure = subscriptionBool(q, "allowInsecure", "insecure", "skip-cert-verify")
	s.PublicKey = firstNonEmpty(q.Get("pbk"), q.Get("publicKey"))
	s.ShortID = firstNonEmpty(q.Get("sid"), q.Get("shortId"))
	s.SpiderX = firstNonEmpty(q.Get("spx"), q.Get("spiderX"), s.SpiderX)
	s.Flow = normalizeFlow(q.Get("flow"))
	s.VlessEncrypt = firstNonEmpty(q.Get("encryption"), "none")
	s.Path = q.Get("path")
	s.Host = q.Get("host")
	s.Service = firstNonEmpty(q.Get("serviceName"), q.Get("service"))
	if s.Network == "grpc" && s.Service == "" {
		s.Service = s.Path
	}
	return s, nil
}

func parseVMess(raw string) (serverConfig, error) {
	payload := raw[len("vmess://"):]
	text, err := decodeBase64Text(payload)
	if err != nil {
		return serverConfig{}, errors.New("VMess 内容解码失败")
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(text), &m); err != nil {
		return serverConfig{}, errors.New("VMess JSON 无效")
	}
	s := defaultServerConfig()
	s.ID = ""
	s.Protocol = "vmess"
	s.Name = jsonString(m, "ps")
	s.Address = jsonString(m, "add")
	if s.Address == "" {
		return serverConfig{}, errors.New("VMess 缺少地址")
	}
	if s.Port, err = subscriptionPort(jsonString(m, "port"), "443"); err != nil {
		return serverConfig{}, err
	}
	s.Secret = jsonString(m, "id")
	if s.Secret == "" {
		return serverConfig{}, errors.New("VMess 缺少 UUID")
	}
	s.VMessSecurity = firstNonEmpty(jsonString(m, "scy"), "auto")
	if s.Network, err = subscriptionNetwork(jsonString(m, "net")); err != nil {
		return serverConfig{}, err
	}
	switch strings.ToLower(jsonString(m, "tls")) {
	case "tls":
		s.Security = "tls"
	case "reality":
		s.Security = "reality"
	default:
		s.Security = "none"
	}
	s.SNI = firstNonEmpty(jsonString(m, "sni"), jsonString(m, "serverName"))
	s.ALPN = jsonString(m, "alpn")
	s.Fingerprint = firstNonEmpty(jsonString(m, "fp"), s.Fingerprint)
	s.AllowInsecure = jsonBool(m, "allowInsecure") || jsonBool(m, "skip-cert-verify")
	s.Path = jsonString(m, "path")
	s.Host = jsonString(m, "host")
	s.Service = firstNonEmpty(jsonString(m, "serviceName"), jsonString(m, "service"))
	if s.Network == "grpc" && s.Service == "" {
		s.Service = s.Path
	}
	s.PublicKey = firstNonEmpty(jsonString(m, "pbk"), jsonString(m, "publicKey"))
	s.ShortID = firstNonEmpty(jsonString(m, "sid"), jsonString(m, "shortId"))
	s.SpiderX = firstNonEmpty(jsonString(m, "spx"), jsonString(m, "spiderX"), s.SpiderX)
	return s, nil
}

func parseTrojan(raw string) (serverConfig, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" {
		return serverConfig{}, errors.New("Trojan 链接无效")
	}
	s := defaultServerConfig()
	s.ID = ""
	s.Protocol = "trojan"
	s.Name = strings.TrimSpace(u.Fragment)
	s.Address = u.Hostname()
	if s.Port, err = subscriptionPort(u.Port(), "443"); err != nil {
		return serverConfig{}, err
	}
	if u.User != nil {
		s.Secret = u.User.Username()
	}
	if s.Secret == "" {
		return serverConfig{}, errors.New("Trojan 缺少密码")
	}
	q := u.Query()
	if s.Network, err = subscriptionNetwork(firstNonEmpty(q.Get("type"), q.Get("net"))); err != nil {
		return serverConfig{}, err
	}
	s.Security = subscriptionSecurity(q.Get("security"), "tls")
	s.SNI = firstNonEmpty(q.Get("sni"), q.Get("peer"))
	s.ALPN = q.Get("alpn")
	s.Fingerprint = firstNonEmpty(q.Get("fp"), q.Get("fingerprint"), s.Fingerprint)
	s.AllowInsecure = subscriptionBool(q, "allowInsecure", "insecure", "skip-cert-verify")
	s.Path = q.Get("path")
	s.Host = q.Get("host")
	s.Service = firstNonEmpty(q.Get("serviceName"), q.Get("service"))
	if s.Network == "grpc" && s.Service == "" {
		s.Service = s.Path
	}
	return s, nil
}

func parseShadowsocks(raw string) (serverConfig, error) {
	body := raw[len("ss://"):]
	name := ""
	if i := strings.Index(body, "#"); i >= 0 {
		name, _ = url.PathUnescape(body[i+1:])
		body = body[:i]
	}
	query := ""
	if i := strings.Index(body, "?"); i >= 0 {
		query = body[i+1:]
		body = body[:i]
	}
	if strings.Contains(query, "plugin=") {
		return serverConfig{}, errors.New("暂不支持 Shadowsocks 插件")
	}

	var userinfo, hostport string
	if i := strings.LastIndex(body, "@"); i >= 0 {
		userinfo, hostport = body[:i], body[i+1:]
	} else {
		decoded, err := decodeBase64Text(body)
		if err != nil {
			return serverConfig{}, errors.New("Shadowsocks 链接无效")
		}
		i := strings.LastIndex(decoded, "@")
		if i < 0 {
			return serverConfig{}, errors.New("Shadowsocks 链接无效")
		}
		userinfo, hostport = decoded[:i], decoded[i+1:]
	}
	method, password, ok := splitShadowsocksUserinfo(userinfo)
	if !ok {
		return serverConfig{}, errors.New("Shadowsocks 缺少加密方法或密码")
	}
	host, portRaw, err := net.SplitHostPort(hostport)
	if err != nil || host == "" {
		return serverConfig{}, errors.New("Shadowsocks 地址无效")
	}
	s := defaultServerConfig()
	s.ID = ""
	s.Protocol = "shadowsocks"
	s.Name = name
	s.Address = host
	s.Method = method
	s.Secret = password
	if s.Port, err = subscriptionPort(portRaw, ""); err != nil {
		return serverConfig{}, err
	}
	return s, nil
}

func splitShadowsocksUserinfo(raw string) (string, string, bool) {
	if decoded, err := decodeBase64Text(raw); err == nil && strings.Contains(decoded, ":") {
		raw = decoded
	} else if unescaped, err := url.PathUnescape(raw); err == nil {
		raw = unescaped
	}
	method, password, ok := strings.Cut(raw, ":")
	return method, password, ok && method != ""
}

func parseHysteria2(raw string) (serverConfig, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" {
		return serverConfig{}, errors.New("Hysteria2 链接无效")
	}
	s := defaultServerConfig()
	s.ID = ""
	s.Protocol = "hysteria"
	s.Name = strings.TrimSpace(u.Fragment)
	s.Address = u.Hostname()
	if s.Port, err = subscriptionPort(u.Port(), "443"); err != nil {
		return serverConfig{}, err
	}
	s.Secret = userSecret(u)
	if s.Secret == "" {
		return serverConfig{}, errors.New("Hysteria2 缺少密码")
	}
	q := u.Query()
	s.Security = "tls"
	s.SNI = firstNonEmpty(q.Get("sni"), q.Get("peer"), u.Hostname())
	s.ALPN = q.Get("alpn")
	s.AllowInsecure = subscriptionBool(q, "insecure", "allowInsecure", "skip-cert-verify")
	return s, nil
}

func parseHysteria(raw string) (serverConfig, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" {
		return serverConfig{}, errors.New("Hysteria 链接无效")
	}
	s := defaultServerConfig()
	s.ID = ""
	s.Protocol = "hysteria"
	s.Name = strings.TrimSpace(u.Fragment)
	s.Address = u.Hostname()
	if s.Port, err = subscriptionPort(u.Port(), "443"); err != nil {
		return serverConfig{}, err
	}
	q := u.Query()
	s.Secret = firstNonEmpty(q.Get("auth"), userSecret(u))
	s.Security = "tls"
	s.SNI = firstNonEmpty(q.Get("peer"), q.Get("sni"), u.Hostname())
	s.ALPN = q.Get("alpn")
	s.AllowInsecure = subscriptionBool(q, "insecure", "allowInsecure", "skip-cert-verify")
	return s, nil
}

func parseSocksHTTP(raw, proto string) (serverConfig, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" {
		return serverConfig{}, errors.New("代理链接无效")
	}
	if proto == "http" && u.User == nil {
		return serverConfig{}, errors.New("HTTP 代理需要用户名和密码")
	}
	s := defaultServerConfig()
	s.ID = ""
	s.Protocol = proto
	s.Name = strings.TrimSpace(u.Fragment)
	s.Address = u.Hostname()
	if s.Port, err = subscriptionPort(u.Port(), "1080"); err != nil {
		return serverConfig{}, err
	}
	if u.User != nil {
		s.User = u.User.Username()
		if password, ok := u.User.Password(); ok {
			s.Pass = password
		}
	}
	return s, nil
}

func subscriptionPort(raw, fallback string) (string, error) {
	raw = strings.TrimSpace(strings.Split(raw, ",")[0])
	if raw == "" {
		raw = fallback
	}
	port, err := strconv.Atoi(raw)
	if err != nil || port < 1 || port > 65535 {
		return "", fmt.Errorf("端口无效: %q", raw)
	}
	return strconv.Itoa(port), nil
}

func subscriptionNetwork(raw string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "tcp", "raw":
		return "tcp", nil
	case "ws", "websocket":
		return "ws", nil
	case "grpc", "gun":
		return "grpc", nil
	case "httpupgrade", "http-upgrade":
		return "httpupgrade", nil
	default:
		return "", fmt.Errorf("不支持的传输: %q", raw)
	}
}

func subscriptionSecurity(raw, fallback string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "tls", "xtls":
		return "tls"
	case "reality":
		return "reality"
	case "none":
		return "none"
	case "":
		return fallback
	default:
		return fallback
	}
}

func subscriptionBool(q url.Values, keys ...string) bool {
	for _, key := range keys {
		switch strings.ToLower(strings.TrimSpace(q.Get(key))) {
		case "1", "true", "yes", "on":
			return true
		}
	}
	return false
}

func normalizeFlow(raw string) string {
	if strings.TrimSpace(raw) == "xtls-rprx-vision" {
		return "xtls-rprx-vision"
	}
	return ""
}

func userSecret(u *url.URL) string {
	if u.User == nil {
		return ""
	}
	if password, ok := u.User.Password(); ok {
		return u.User.Username() + ":" + password
	}
	return u.User.Username()
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func jsonString(m map[string]any, key string) string {
	value, ok := m[key]
	if !ok || value == nil {
		return ""
	}
	switch v := value.(type) {
	case string:
		return strings.TrimSpace(v)
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	case bool:
		if v {
			return "true"
		}
		return "false"
	default:
		return strings.TrimSpace(fmt.Sprint(v))
	}
}

func jsonBool(m map[string]any, key string) bool {
	value, ok := m[key]
	if !ok || value == nil {
		return false
	}
	switch v := value.(type) {
	case bool:
		return v
	case string:
		switch strings.ToLower(strings.TrimSpace(v)) {
		case "1", "true", "yes", "on":
			return true
		}
	case float64:
		return v != 0
	}
	return false
}
