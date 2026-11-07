//go:build !windows

package main

import (
	"runtime"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
)

// The copy button is always available, while the checkbox only works on
// Windows: on macOS and Linux it is disabled.
func TestEnvProxyUI(t *testing.T) {
	a := &app{cfg: defaultAppConfig(), cursor: -1}
	a.saver.remember(a.cfg)
	tt := ui.NewTester(a.view, 880, 620)
	for _, want := range []string{"设置代理环境变量（终端可用）", "复制环境变量命令"} {
		if !tt.HasText(want) {
			t.Errorf("missing %q; texts: %v", want, tt.Texts())
		}
	}
	if runtime.GOOS != "windows" {
		before := a.cfg.Global.EnvProxy
		if err := tt.Click("设置代理环境变量（终端可用）"); err == nil {
			if a.cfg.Global.EnvProxy != before {
				t.Error("the disabled checkbox should not change the setting")
			}
		}
	}
	// The button itself is clickable; the clipboard is only written when a
	// real window exists, which the headless tester does not provide.
	if err := tt.Click("复制环境变量命令"); err != nil {
		t.Errorf("the copy button should be clickable: %v", err)
	}
}

// On Unix the copied commands are shell exports with the configured port.
func TestEnvProxyCommandsUnix(t *testing.T) {
	got := envProxyCommands("127.0.0.1:12345")
	for _, want := range []string{
		"export HTTP_PROXY=http://127.0.0.1:12345",
		"export HTTPS_PROXY=http://127.0.0.1:12345",
		"export ALL_PROXY=socks5://127.0.0.1:12345",
		"NO_PROXY=localhost,127.0.0.1,::1",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("commands missing %q: %s", want, got)
		}
	}
}
