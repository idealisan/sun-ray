//go:build webui

package main

// webui_controller.go is the webui's backend: it owns the config, the
// in-process core and the log buffer, and serves a small JSON API for the
// embedded page. It reuses the ungated business logic (core.go,
// persistence.go, protocols.go, policy.go, sharelink.go, speedtest.go and
// the shared subscription/policy helpers) but never touches the GUI's app,
// view or mygo. The GUI remains authoritative for layout and behavior.

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"time"

	xlog "github.com/xtls/xray-core/common/log"
)

// webMaxLogLines mirrors the GUI's maxLogLines: the log is a debugging aid,
// so only the newest lines are kept.
const webMaxLogLines = 200

// webController is the state behind the page. mu guards cfg, logs,
// configBroken and lastJSON; opMu serializes start/stop/restart.
type webController struct {
	mu           sync.Mutex
	opMu         sync.Mutex
	cfg          appConfig
	core         xrayCore
	saver        configSaver
	logs         []string
	logChars     int
	configBroken string
	lastCoreJSON []byte

	// meta is built once: the field metadata and option lists the page uses
	// to render forms, derived from the same registries the GUI uses.
	meta webMeta
}

func newWebController() *webController {
	c := &webController{meta: buildWebMeta()}
	c.saver.onError = func(err error) { c.addLog("[sun-ray] 保存配置失败: " + err.Error()) }

	cfg, err := loadConfig()
	if err != nil {
		// Never write over a file that failed to load (AGENTS.md). Start from
		// defaults with the saver blocked until the user chooses to reset.
		c.cfg = defaultAppConfig()
		c.configBroken = err.Error()
		c.saver.blocked = true
		c.addLog("[sun-ray] 配置加载失败，未写回原文件：" + err.Error())
	} else {
		c.cfg = cfg
	}
	c.saver.remember(c.cfg)
	c.addLog("Sun Ray " + versionString() + " (webui)")

	if c.configBroken == "" && c.cfg.Global.AutoStart {
		if err := c.startCore(); err != nil {
			c.addLog("[sun-ray] 自动启动失败: " + err.Error())
		}
	}
	return c
}

// addLog is the xray log callback. It runs on arbitrary core goroutines, so
// it only touches the guarded buffer — there is no window to update here
// (the GUI's addLog calls a.win.Update; the webui has no such thing).
func (c *webController) addLog(line string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	text := "[当前] " + strings.TrimRight(line, "\n")
	c.logs = append(c.logs, text)
	c.logChars += len(text) + 1
	for len(c.logs) > webMaxLogLines {
		c.logChars -= len(c.logs[0]) + 1
		c.logs = c.logs[1:]
	}
}

func (c *webController) stopCore() {
	c.opMu.Lock()
	defer c.opMu.Unlock()
	_ = c.core.Stop()
}

func (c *webController) startCore() error {
	c.opMu.Lock()
	defer c.opMu.Unlock()
	return c.startCoreLocked()
}

func (c *webController) startCoreLocked() error {
	if c.core.Running() {
		return nil
	}
	c.mu.Lock()
	cfg := c.cfg
	broken := c.configBroken
	c.mu.Unlock()
	if broken != "" {
		return errors.New("配置加载失败，请先「创建全新配置」")
	}
	s := activeServerOf(cfg)
	if s == nil {
		return errors.New("没有服务器")
	}
	// The webui does not launch the privileged TUN helper or touch the
	// system proxy (those are GUI-only). TUN settings are still stored so a
	// config authored here works in the GUI; building the core with TUN on
	// needs root and is left to the deployment, like the headless build.
	data, err := s.buildJSON(&cfg.Global)
	if err != nil {
		return err
	}
	if err := c.core.Start(data, c.addLog); err != nil {
		return err
	}
	c.mu.Lock()
	c.lastCoreJSON = data
	c.mu.Unlock()
	return nil
}

func (c *webController) stopCoreLocked() error {
	return c.core.Stop()
}

func (c *webController) restartCoreLocked() error {
	_ = c.core.Stop()
	return c.startCoreLocked()
}

// routes registers every endpoint on the mux.
func (c *webController) routes(mux *http.ServeMux, page []byte) {
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write(page)
	})
	mux.HandleFunc("/api/state", c.handleState)
	mux.HandleFunc("/api/meta", c.handleMeta)
	mux.HandleFunc("/api/start", c.handleStart)
	mux.HandleFunc("/api/stop", c.handleStop)
	mux.HandleFunc("/api/config", c.handleConfig)
	mux.HandleFunc("/api/config/reset", c.handleConfigReset)
	mux.HandleFunc("/api/logs/clear", c.handleLogsClear)
	mux.HandleFunc("/api/servers/import", c.handleServersImport)
	mux.HandleFunc("/api/servers/export", c.handleServersExport)
	mux.HandleFunc("/api/subscriptions/update", c.handleSubscriptionsUpdate)
	mux.HandleFunc("/api/policy/test", c.handlePolicyTest)
	mux.HandleFunc("/api/test", c.handleTest)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(v)
}

func writeOK(w http.ResponseWriter, err error, extra map[string]any) {
	out := map[string]any{"ok": err == nil}
	if err != nil {
		out["error"] = err.Error()
	}
	for k, v := range extra {
		out[k] = v
	}
	writeJSON(w, out)
}

func requirePost(w http.ResponseWriter, r *http.Request) bool {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return false
	}
	return true
}

// webState is what the page polls. config carries secrets, so the default
// listener is loopback-only (see README).
type webState struct {
	Version      string    `json:"version"`
	Config       appConfig `json:"config"`
	Running      bool      `json:"running"`
	ActiveID     string    `json:"activeId"`
	Up           int64     `json:"up"`
	Down         int64     `json:"down"`
	Logs         []string  `json:"logs"`
	ConfigBroken string    `json:"configBroken"`
}

func (c *webController) handleState(w http.ResponseWriter, r *http.Request) {
	c.mu.Lock()
	cfg := c.cfg
	logs := append([]string(nil), c.logs...)
	broken := c.configBroken
	c.mu.Unlock()
	up, down := c.core.Traffic()
	writeJSON(w, webState{
		Version:      versionString(),
		Config:       cfg,
		Running:      c.core.Running(),
		ActiveID:     cfg.ActiveID,
		Up:           up,
		Down:         down,
		Logs:         logs,
		ConfigBroken: broken,
	})
}

func (c *webController) handleMeta(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, c.meta)
}

func (c *webController) handleStart(w http.ResponseWriter, r *http.Request) {
	if !requirePost(w, r) {
		return
	}
	writeOK(w, c.startCore(), nil)
}

func (c *webController) handleStop(w http.ResponseWriter, r *http.Request) {
	if !requirePost(w, r) {
		return
	}
	c.opMu.Lock()
	err := c.stopCoreLocked()
	c.opMu.Unlock()
	writeOK(w, err, nil)
}

// handleConfig replaces the whole config (the page sends the appConfig it
// built), validates it, persists it and restarts a running core.
func (c *webController) handleConfig(w http.ResponseWriter, r *http.Request) {
	if !requirePost(w, r) {
		return
	}
	var cfg appConfig
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<20)).Decode(&cfg); err != nil {
		writeOK(w, fmt.Errorf("配置无效: %v", err), nil)
		return
	}
	if cfg.Servers == nil {
		writeOK(w, errors.New("配置缺少服务器列表"), nil)
		return
	}
	for i := range cfg.Servers {
		if cfg.Servers[i].ID == "" {
			cfg.Servers[i].ID = newServerID()
		}
	}
	for i := range cfg.Subscriptions {
		if cfg.Subscriptions[i].ID == "" {
			cfg.Subscriptions[i].ID = newServerID()
		}
	}
	if cfg.Results == nil {
		cfg.Results = map[string]serverResult{}
	}
	if cfg.Global.TunMTU <= 0 {
		cfg.Global.TunMTU = defaultTunMTU
	}
	if cfg.Global.DNSPreset == "" {
		cfg.Global.DNSPreset = dnsPresetSystem
	}
	for _, p := range cfg.Global.Policies {
		if _, err := normalizePolicy(p); err != nil {
			writeOK(w, fmt.Errorf("策略校验失败: %v", err), nil)
			return
		}
	}

	c.opMu.Lock()
	c.mu.Lock()
	c.cfg = cfg
	c.configBroken = ""
	c.saver.blocked = false
	c.mu.Unlock()
	c.saver.maybeSave(cfg, true)
	c.saver.remember(cfg)
	err := c.restartCoreIfRunningLocked()
	c.opMu.Unlock()
	if err != nil {
		c.addLog("[sun-ray] 应用配置后重启失败: " + err.Error())
	}
	writeOK(w, err, nil)
}

func (c *webController) restartCoreIfRunningLocked() error {
	if !c.core.Running() {
		return nil
	}
	return c.restartCoreLocked()
}

func (c *webController) handleConfigReset(w http.ResponseWriter, r *http.Request) {
	if !requirePost(w, r) {
		return
	}
	cfg := defaultAppConfig()
	c.opMu.Lock()
	c.mu.Lock()
	c.cfg = cfg
	c.configBroken = ""
	c.saver.blocked = false
	c.mu.Unlock()
	c.saver.remember(cfg)
	c.saver.maybeSave(cfg, true)
	c.opMu.Unlock()
	c.addLog("[sun-ray] 已创建全新配置")
	writeOK(w, nil, nil)
}

func (c *webController) handleLogsClear(w http.ResponseWriter, r *http.Request) {
	if !requirePost(w, r) {
		return
	}
	c.mu.Lock()
	c.logs = nil
	c.logChars = 0
	c.mu.Unlock()
	writeOK(w, nil, nil)
}

func (c *webController) handleServersImport(w http.ResponseWriter, r *http.Request) {
	if !requirePost(w, r) {
		return
	}
	var body struct {
		Text string `json:"text"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<20)).Decode(&body); err != nil {
		writeOK(w, fmt.Errorf("内容无效: %v", err), nil)
		return
	}
	var servers []serverConfig
	var errs []string
	for _, line := range strings.Split(body.Text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		s, err := parseShareLink(line)
		if err != nil {
			errs = append(errs, err.Error())
			continue
		}
		s.ID = newServerID()
		servers = append(servers, s)
	}
	if len(servers) == 0 {
		msg := "没有可导入的节点"
		if len(errs) > 0 {
			msg += "：" + strings.Join(errs, "；")
		}
		writeOK(w, errors.New(msg), nil)
		return
	}
	c.opMu.Lock()
	c.mu.Lock()
	c.cfg.Servers = append(c.cfg.Servers, servers...)
	if _, ok := serverByIDOf(c.cfg, c.cfg.ActiveID); !ok && len(c.cfg.Servers) > 0 {
		c.cfg.ActiveID = c.cfg.Servers[0].ID
	}
	cfg := c.cfg
	c.mu.Unlock()
	c.saver.maybeSave(cfg, true)
	c.saver.remember(cfg)
	err := c.restartCoreIfRunningLocked()
	c.opMu.Unlock()
	writeOK(w, err, map[string]any{"imported": len(servers), "errors": errs})
}

func (c *webController) handleServersExport(w http.ResponseWriter, r *http.Request) {
	c.mu.Lock()
	servers := append([]serverConfig(nil), c.cfg.Servers...)
	c.mu.Unlock()
	text, errs := shareLinksForServers(servers)
	writeJSON(w, map[string]any{"text": text, "errors": errs})
}

func (c *webController) handleSubscriptionsUpdate(w http.ResponseWriter, r *http.Request) {
	if !requirePost(w, r) {
		return
	}
	var body struct {
		ID string `json:"id"`
	}
	_ = json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&body)

	c.mu.Lock()
	subs := append([]subscriptionConfig(nil), c.cfg.Subscriptions...)
	c.mu.Unlock()

	results := []map[string]any{}
	changed := false
	for _, sub := range subs {
		if body.ID != "" && sub.ID != body.ID {
			continue
		}
		nodes, err := fetchSubscriptionNodes(sub)
		if err != nil {
			results = append(results, map[string]any{"id": sub.ID, "name": sub.displayName(), "error": err.Error()})
			c.addLog("[订阅] " + sub.displayName() + " 更新失败: " + err.Error())
			continue
		}
		c.opMu.Lock()
		c.mu.Lock()
		for i := range c.cfg.Subscriptions {
			if c.cfg.Subscriptions[i].ID == sub.ID {
				applySubscriptionNodesTo(&c.cfg, &c.cfg.Subscriptions[i], nodes)
				c.cfg.Subscriptions[i].LastError = ""
				break
			}
		}
		cfg := c.cfg
		c.mu.Unlock()
		c.saver.maybeSave(cfg, true)
		c.saver.remember(cfg)
		_ = c.restartCoreIfRunningLocked()
		c.opMu.Unlock()
		changed = true
		results = append(results, map[string]any{"id": sub.ID, "name": sub.displayName(), "count": len(nodes)})
		c.addLog(fmt.Sprintf("[订阅] %s 已更新：%d 个节点", sub.displayName(), len(nodes)))
	}
	_ = changed
	writeJSON(w, map[string]any{"ok": true, "results": results})
}

func (c *webController) handlePolicyTest(w http.ResponseWriter, r *http.Request) {
	if !requirePost(w, r) {
		return
	}
	var body struct {
		Input string `json:"input"`
	}
	_ = json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&body)
	c.mu.Lock()
	g := c.cfg.Global
	c.mu.Unlock()
	m, err := matchPolicyFor(&g, body.Input)
	writeOK(w, err, map[string]any{"source": m.Source, "action": m.Action, "note": m.Note})
}

// handleTest measures one server through a temporary core, the way the GUI's
// measureServer does, using only ungated helpers.
func (c *webController) handleTest(w http.ResponseWriter, r *http.Request) {
	if !requirePost(w, r) {
		return
	}
	var body struct {
		ID string `json:"id"`
	}
	_ = json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&body)
	c.mu.Lock()
	s, ok := serverByIDOf(c.cfg, body.ID)
	var sc serverConfig
	if ok {
		sc = *s
	}
	last := c.lastCoreJSON
	c.mu.Unlock()
	if !ok {
		writeOK(w, errors.New("找不到该服务器"), nil)
		return
	}
	if sc.Address == "" || sc.Port == "" {
		writeOK(w, errors.New("服务器地址或端口为空"), nil)
		return
	}
	port, err := freeLoopbackPort()
	if err != nil {
		writeOK(w, err, nil)
		return
	}
	g := &globalConfig{ListenPort: tempPortString(port), LogLevel: "info"}
	data, err := sc.buildJSON(g)
	if err != nil {
		writeOK(w, err, nil)
		return
	}
	inst, err := startInstance(data)
	if err != nil {
		writeOK(w, err, nil)
		return
	}
	defer inst.Close()
	// The temporary core takes over the xray log handler; give it back to
	// the running core (or to this controller) when we are done.
	defer xlog.RegisterHandler(guiHandlerForConfig(last, c.addLog))

	socksAddr := "127.0.0.1:" + strconv.Itoa(port)
	latency, err := measureLatency(socksAddr)
	if err != nil {
		writeOK(w, err, nil)
		return
	}
	speed, err := measureSpeed(socksAddr)
	if err != nil {
		writeOK(w, err, map[string]any{"latency": formatLatency(latency)})
		return
	}
	// Keep the measurement so the list can show it, like the GUI does.
	c.mu.Lock()
	if c.cfg.Results == nil {
		c.cfg.Results = map[string]serverResult{}
	}
	c.cfg.Results[sc.ID] = serverResult{
		LatencyMs: int(latency.Milliseconds()), SpeedBps: speed, TestedAt: time.Now().Unix(),
	}
	cfg := c.cfg
	c.mu.Unlock()
	c.saver.maybeSave(cfg, true)
	c.saver.remember(cfg)
	writeOK(w, nil, map[string]any{"latency": formatLatency(latency), "speed": formatSpeed(speed)})
}

// ---- metadata: derived from the same registries/constants as the GUI ----

// webField describes one serverConfig field for the page's edit form.
type webField struct {
	Key         string   `json:"key"` // JSON field name in serverConfig
	Label       string   `json:"label"`
	Placeholder string   `json:"placeholder,omitempty"`
	Kind        string   `json:"kind"` // "text" | "select" | "check"
	Options     []string `json:"options,omitempty"`
}

type webProtocolMeta struct {
	NeedsAddr   bool       `json:"needsAddr"`
	NeedsSecret bool       `json:"needsSecret"`
	SecretLabel string     `json:"secretLabel"`
	Stream      bool       `json:"stream"`
	TLSOnly     bool       `json:"tlsOnly"`
	Fields      []webField `json:"fields"`
}

type webMeta struct {
	Protocols        []string                   `json:"protocols"`
	ProtocolSpecs    map[string]webProtocolMeta `json:"protocolSpecs"`
	LogLevels        []string                   `json:"logLevels"`
	DNSPresetValues  []string                   `json:"dnsPresetValues"`
	DNSPresetLabels  []string                   `json:"dnsPresetLabels"`
	DNSCustom        string                     `json:"dnsCustom"`
	ProxyModeValues  []string                   `json:"proxyModeValues"`
	ProxyModeLabels  []string                   `json:"proxyModeLabels"`
	PolicyTypeValues []string                   `json:"policyTypeValues"`
	PolicyTypeLabels []string                   `json:"policyTypeLabels"`
	PolicyActValues  []string                   `json:"policyActionValues"`
	PolicyActLabels  []string                   `json:"policyActionLabels"`
	DomainStrategies []string                   `json:"domainStrategies"`
	Networks         []string                   `json:"networks"`
	Securities       []string                   `json:"securities"`
	FlowOptions      []string                   `json:"flowOptions"`
	VmessSecurities  []string                   `json:"vmessSecurities"`
	Fingerprints     []string                   `json:"fingerprints"`
	ServerFields     []webField                 `json:"serverFields"`
}

// fieldKey finds which serverConfig field a fieldSpec binds, by comparing
// the pointer it returns with the address of each struct field. This keeps
// the page's form keys in lockstep with the GUI's fieldSpecs: no duplicated
// label/key list that could drift.
func fieldKey(f fieldSpec) string {
	var s serverConfig
	v := reflect.ValueOf(&s).Elem()
	t := v.Type()
	var target uintptr
	if f.bindString != nil {
		target = reflect.ValueOf(f.bindString(&s)).Pointer()
	} else if f.bindBool != nil {
		target = reflect.ValueOf(f.bindBool(&s)).Pointer()
	} else {
		return ""
	}
	for i := 0; i < v.NumField(); i++ {
		if v.Field(i).Addr().Pointer() == target {
			tag := t.Field(i).Tag.Get("json")
			if idx := strings.IndexByte(tag, ','); idx >= 0 {
				tag = tag[:idx]
			}
			return tag
		}
	}
	return ""
}

func toWebField(f fieldSpec) webField {
	w := webField{Key: fieldKey(f), Label: f.label, Placeholder: f.placeholder, Options: f.options}
	switch {
	case f.options != nil:
		w.Kind = "select"
	case f.bindBool != nil:
		w.Kind = "check"
	default:
		w.Kind = "text"
	}
	return w
}

func buildWebMeta() webMeta {
	m := webMeta{
		Protocols:        protocolList,
		ProtocolSpecs:    map[string]webProtocolMeta{},
		LogLevels:        logLevels,
		DNSPresetValues:  dnsPresetValues,
		DNSPresetLabels:  dnsPresetLabels,
		DNSCustom:        dnsPresetCustom,
		ProxyModeValues:  proxyModeOptions,
		PolicyTypeValues: policyTypeValues,
		PolicyTypeLabels: policyTypeLabels,
		PolicyActValues:  policyActionValues,
		PolicyActLabels:  policyActionLabels,
		DomainStrategies: domainStrategies,
		Networks:         networks,
		Securities:       securities,
		FlowOptions:      flowOptions,
		VmessSecurities:  vmessSecurities,
		Fingerprints:     fingerprints,
	}
	for _, mode := range proxyModeOptions {
		m.ProxyModeLabels = append(m.ProxyModeLabels, proxyModeLabel(mode))
	}
	for _, p := range protocolList {
		sp := spec(p)
		pm := webProtocolMeta{
			NeedsAddr: sp.needsAddr, NeedsSecret: sp.needsSecret,
			SecretLabel: sp.secretLabel, Stream: sp.stream, TLSOnly: sp.tlsOnly,
		}
		for _, f := range sp.fields {
			pm.Fields = append(pm.Fields, toWebField(f))
		}
		m.ProtocolSpecs[p] = pm
	}
	// The transport/security fields shared by stream protocols. The keys are
	// fixed struct members; labels mirror the GUI's serverForm.
	m.ServerFields = []webField{
		{Key: "name", Label: "名称（可选）", Kind: "text", Placeholder: "例如 日本节点"},
		{Key: "protocol", Label: "协议", Kind: "select", Options: protocolList},
		{Key: "address", Label: "地址", Kind: "text", Placeholder: "example.com"},
		{Key: "port", Label: "端口", Kind: "text", Placeholder: "443"},
		{Key: "network", Label: "传输", Kind: "select", Options: networks},
		{Key: "path", Label: "Path", Kind: "text"},
		{Key: "host", Label: "Host / authority", Kind: "text"},
		{Key: "service", Label: "serviceName", Kind: "text"},
		{Key: "security", Label: "安全", Kind: "select", Options: securities},
		{Key: "sni", Label: "SNI（留空用地址）", Kind: "text"},
		{Key: "alpn", Label: "ALPN（逗号分隔，可选）", Kind: "text"},
		{Key: "fingerprint", Label: "uTLS 指纹", Kind: "select", Options: fingerprints},
		{Key: "publicKey", Label: "Public Key", Kind: "text"},
		{Key: "shortID", Label: "Short ID", Kind: "text"},
		{Key: "spiderX", Label: "SpiderX", Kind: "text"},
		{Key: "allowInsecure", Label: "允许不安全证书", Kind: "check"},
	}
	return m
}
