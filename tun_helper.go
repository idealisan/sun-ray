package main

// tun_helper.go is the privileged side of TUN mode. The app runs an
// in-process core behind a loopback mixed inbound; a helper process, the
// same binary started with -tun-helper, owns the TUN interface and forwards
// its packets to that inbound over SOCKS. The helper never reads the server
// secrets: its config contains only the TUN inbound and a SOCKS outbound to
// 127.0.0.1.

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"

	xlog "github.com/xtls/xray-core/common/log"
)

const tunHelperArg = "-tun-helper"

// errElevationCancelled means the user dismissed the administrator prompt.
var errElevationCancelled = errors.New("elevation cancelled")

// tunHelperWrapper is the file the app hands to the helper. The helper
// deletes it as soon as it has read it.
type tunHelperWrapper struct {
	Xray    json.RawMessage `json:"xray"`
	Control string          `json:"control"`
	Token   string          `json:"token"`
	TunName string          `json:"tunName"`
}

// tunHelperSettings describes the TUN side, so a settings change can keep
// the helper alive and restart only the in-process core.
type tunHelperSettings struct {
	TunName    string
	MTU        int
	Gateway    string
	IPv6       bool
	ListenPort string
	Iface      string
}

func tunSettingsFor(g *globalConfig, iface, currentName string) tunHelperSettings {
	name := strings.TrimSpace(g.TunName)
	if name == "" {
		name = currentName
	}
	if name == "" {
		name = tunNameFor(g)
	}
	mtu := int(g.TunMTU)
	if mtu <= 0 {
		mtu = defaultTunMTU
	}
	gateway := strings.TrimSpace(g.TunGateway)
	if gateway == "" {
		gateway = defaultTunGateway
	}
	return tunHelperSettings{
		TunName:    name,
		MTU:        mtu,
		Gateway:    gateway,
		IPv6:       g.TunIPv6,
		ListenPort: g.ListenPort,
		Iface:      iface,
	}
}

func sameTun(a, b tunHelperSettings) bool { return a == b }

// runTunHelperIfRequested handles the hidden helper mode before any UI or
// flag parsing. It exits the process when it was invoked as the helper.
func runTunHelperIfRequested() bool {
	if len(os.Args) < 2 || os.Args[1] != tunHelperArg {
		return false
	}
	if err := runTunHelper(os.Args[2:]); err != nil {
		os.Exit(1)
	}
	os.Exit(0)
	return true
}

func runTunHelper(args []string) (err error) {
	fs := flag.NewFlagSet("tun-helper", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	configPath := fs.String("config", "", "the wrapper config")
	errorPath := fs.String("errorfile", "", "where to report startup errors")
	if err := fs.Parse(args); err != nil {
		writeHelperError(*errorPath, err)
		return err
	}
	if *configPath == "" {
		err := errors.New("tun-helper: -config is required")
		writeHelperError(*errorPath, err)
		return err
	}

	data, err := os.ReadFile(*configPath)
	if err != nil {
		writeHelperError(*errorPath, err)
		return err
	}
	// The file holds the control token; do not leave it behind.
	_ = os.Remove(*configPath)

	var wrapper tunHelperWrapper
	if err := json.Unmarshal(data, &wrapper); err != nil {
		writeHelperError(*errorPath, err)
		return err
	}
	if len(wrapper.Xray) == 0 || wrapper.Control == "" || wrapper.Token == "" {
		err := errors.New("tun-helper: incomplete wrapper config")
		writeHelperError(*errorPath, err)
		return err
	}

	conn, err := net.Dial("tcp", wrapper.Control)
	if err != nil {
		writeHelperError(*errorPath, err)
		return err
	}
	defer conn.Close()

	send := &helperSender{conn: conn}
	if err := send.send("HELLO %s", wrapper.Token); err != nil {
		writeHelperError(*errorPath, err)
		return err
	}

	defer func() {
		if r := recover(); r != nil {
			_ = send.send("ERROR %v", r)
			err = fmt.Errorf("%v", r)
			writeHelperError(*errorPath, err)
		}
	}()

	inst, err := startInstance(wrapper.Xray)
	if err != nil {
		_ = send.send("ERROR %v", err)
		writeHelperError(*errorPath, err)
		return err
	}
	defer inst.Close()

	xlog.RegisterHandler(&guiHandler{fn: func(line string) {
		line = strings.ReplaceAll(strings.TrimRight(line, "\n"), "\n", " ")
		_ = send.send("LOG %s", line)
	}})
	if err := send.send("READY"); err != nil {
		return err
	}

	stop := make(chan struct{})
	go func() {
		defer close(stop)
		r := bufio.NewReader(conn)
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				return
			}
			if strings.TrimSpace(line) == "STOP" {
				return
			}
		}
	}()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sig)
	select {
	case <-stop:
	case <-sig:
	}
	return nil
}

type helperSender struct {
	conn net.Conn
	mu   sync.Mutex
}

func (s *helperSender) send(format string, args ...any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := fmt.Fprintf(s.conn, format+"\n", args...)
	return err
}

func writeHelperError(path string, err error) {
	if path == "" || err == nil {
		return
	}
	f, openErr := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if openErr != nil {
		return
	}
	defer f.Close()
	_, _ = fmt.Fprintln(f, err)
}

// tunHelperConfig renders the helper's own xray config: a TUN inbound and
// a SOCKS outbound to the app's loopback mixed inbound. It contains no
// server address, UUID, password or other secret.
func tunHelperConfig(g *globalConfig, proxyPort, tunName, control, token string) ([]byte, error) {
	port, err := strconv.Atoi(proxyPort)
	if err != nil || port < 1 || port > 65535 {
		return nil, fmt.Errorf("监听端口无效: %q", proxyPort)
	}
	if strings.TrimSpace(control) == "" || strings.TrimSpace(token) == "" {
		return nil, errors.New("TUN 辅助进程缺少控制地址或令牌")
	}
	local := *g
	local.TunName = tunName
	local.TunEnabled = true
	local.TunHelper = false
	local.TunBypassInterface = ""

	level := g.LogLevel
	if level == "" {
		level = "warning"
	}
	cfg := map[string]any{
		"log":      map[string]any{"loglevel": level},
		"inbounds": []any{tunInbound(&local)},
		"outbounds": []any{map[string]any{
			"tag": "socks-out", "protocol": "socks",
			"settings": map[string]any{"servers": []any{map[string]any{
				"address": "127.0.0.1", "port": port,
			}}},
		}},
	}
	return json.MarshalIndent(cfg, "", "  ")
}

func randomToken() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return strconv.FormatInt(int64(os.Getpid()), 16)
	}
	return hex.EncodeToString(b[:])
}
