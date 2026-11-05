package main

import (
	"strings"
	"testing"
)

func TestPACTemplate(t *testing.T) {
	pac := strings.Replace(pacTemplate, "%s", "127.0.0.1:10808", 2)
	for _, want := range []string{"FindProxyForURL", "PROXY 127.0.0.1:10808", "SOCKS5 127.0.0.1:10808", "DIRECT"} {
		if !strings.Contains(pac, want) {
			t.Errorf("PAC file missing %q; got %s", want, pac)
		}
	}
	if strings.Contains(pac, "%s") {
		t.Error("PAC file still has a placeholder")
	}
}

func TestProxyModeLabel(t *testing.T) {
	if proxyModeLabel(proxyModeOff) != "不修改" {
		t.Errorf("off label = %q", proxyModeLabel(proxyModeOff))
	}
	if proxyModeLabel(proxyModeManual) != "系统代理" {
		t.Errorf("manual label = %q", proxyModeLabel(proxyModeManual))
	}
	if proxyModeLabel(proxyModePAC) != "PAC 模式" {
		t.Errorf("pac label = %q", proxyModeLabel(proxyModePAC))
	}
}
