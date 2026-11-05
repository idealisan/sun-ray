//go:build !nogui

package main

// tun_helper_app.go is the unprivileged side of TUN mode: it starts the
// helper, speaks the small control protocol with it, and keeps its state
// so a settings change can reuse a helper that is already running.

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type tunHelper struct {
	conn     net.Conn
	reader   *bufio.Reader
	done     chan struct{}
	settings tunHelperSettings
}

// startTunHelper launches the privileged helper and waits until its TUN
// interface is up. iface is the physical interface the in-process core
// binds its outbounds to.
func startTunHelper(g *globalConfig, iface string, logf func(string)) (*tunHelper, error) {
	tunName := tunNameFor(g)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	defer listener.Close()
	control := listener.Addr().String()
	token := randomToken()

	helperJSON, err := tunHelperConfig(g, g.ListenPort, tunName, control, token)
	if err != nil {
		return nil, err
	}
	wrapper, err := json.Marshal(tunHelperWrapper{
		Xray: helperJSON, Control: control, Token: token, TunName: tunName,
	})
	if err != nil {
		return nil, err
	}

	dir, err := os.MkdirTemp("", "sunray-tun-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	configPath := filepath.Join(dir, "helper.json")
	errorPath := filepath.Join(dir, "helper.err")
	if err := os.WriteFile(configPath, wrapper, 0o600); err != nil {
		return nil, err
	}
	if err := os.WriteFile(errorPath, nil, 0o600); err != nil {
		return nil, err
	}

	exe, err := os.Executable()
	if err != nil {
		return nil, err
	}
	if err := launchTunHelper(exe, configPath, errorPath); err != nil {
		return nil, helperStartError(err, errorPath)
	}

	if tcp, ok := listener.(*net.TCPListener); ok {
		_ = tcp.SetDeadline(time.Now().Add(30 * time.Second))
	}
	conn, err := listener.Accept()
	if err != nil {
		return nil, helperStartError(err, errorPath)
	}

	reader := bufio.NewReader(conn)
	hello, err := reader.ReadString('\n')
	if err != nil {
		_ = conn.Close()
		return nil, helperStartError(err, errorPath)
	}
	if strings.TrimSpace(hello) != "HELLO "+token {
		_ = conn.Close()
		return nil, fmt.Errorf("TUN 辅助进程的令牌不匹配")
	}

	h := &tunHelper{
		conn:     conn,
		reader:   reader,
		done:     make(chan struct{}),
		settings: tunSettingsFor(g, iface, tunName),
	}

	_ = conn.SetReadDeadline(time.Now().Add(30 * time.Second))
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			_ = conn.Close()
			return nil, helperStartError(err, errorPath)
		}
		line = strings.TrimSpace(line)
		switch {
		case line == "READY":
			_ = conn.SetReadDeadline(time.Time{})
			go h.readLoop(logf)
			return h, nil
		case strings.HasPrefix(line, "LOG "):
			logf(strings.TrimPrefix(line, "LOG "))
		case strings.HasPrefix(line, "ERROR "):
			_ = conn.Close()
			return nil, fmt.Errorf("TUN 辅助进程: %s", strings.TrimPrefix(line, "ERROR "))
		}
	}
}

func (h *tunHelper) readLoop(logf func(string)) {
	defer close(h.done)
	for {
		line, err := h.reader.ReadString('\n')
		if err != nil {
			logf("[sun-ray] TUN 辅助进程已退出")
			return
		}
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "LOG "):
			logf(strings.TrimPrefix(line, "LOG "))
		case strings.HasPrefix(line, "ERROR "):
			logf("[sun-ray] TUN 辅助进程: " + strings.TrimPrefix(line, "ERROR "))
		}
	}
}

// stop asks the helper to remove the TUN interface and waits briefly for
// it to exit.
func (h *tunHelper) stop() {
	_, _ = fmt.Fprintln(h.conn, "STOP")
	_ = h.conn.Close()
	select {
	case <-h.done:
	case <-time.After(5 * time.Second):
	}
}

func helperStartError(err error, errorPath string) error {
	if err == errElevationCancelled {
		return err
	}
	if data, readErr := os.ReadFile(errorPath); readErr == nil {
		if detail := strings.TrimSpace(string(data)); detail != "" {
			return fmt.Errorf("%w: %s", err, detail)
		}
	}
	return err
}
