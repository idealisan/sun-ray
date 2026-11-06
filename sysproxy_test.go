package main

import (
	"strings"
	"testing"
)

func TestPACTemplate(t *testing.T) {
	pac := strings.Replace(pacTemplate, "%s", "127.0.0.1:10808", 1)
	for _, want := range []string{
		"FindProxyForURL", "PROXY 127.0.0.1:10808", "DIRECT",
		`"*.local"`, `"*.lan"`, `"*.home.arpa"`,
		`isInNet(host, "10.0.0.0", "255.0.0.0")`,
		`isInNet(host, "100.64.0.0", "255.192.0.0")`,
		`isInNet(host, "172.16.0.0", "255.240.0.0")`,
		`isInNet(host, "192.168.0.0", "255.255.0.0")`,
		`isInNet(host, "198.18.0.0", "255.254.0.0")`,
	} {
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
