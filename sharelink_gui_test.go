//go:build !nogui

package main

import (
	"encoding/base64"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestImportShareTextPlainLinks(t *testing.T) {
	a := &app{cfg: defaultAppConfig(), cursor: -1}
	before := len(a.cfg.Servers)
	text := strings.Join([]string{
		"vless://uuid-1@vless.example.com:443#VLESS",
		"trojan://pw@trojan.example.com:443#Trojan",
		"this is not a link",
		"",
		"# a comment",
	}, "\n")
	added, skipped, err := a.importShareText(text)
	if err != nil {
		t.Fatal(err)
	}
	if added != 2 || skipped != 1 {
		t.Errorf("added=%d skipped=%d, want 2/1", added, skipped)
	}
	if len(a.cfg.Servers) != before+2 {
		t.Errorf("servers: want %d, got %d", before+2, len(a.cfg.Servers))
	}
	last := a.cfg.Servers[len(a.cfg.Servers)-1]
	if last.ID == "" || last.SubscriptionID != "" {
		t.Errorf("imported node should get a fresh id and no subscription: %+v", last)
	}
}

func TestImportShareTextBase64Subscription(t *testing.T) {
	a := &app{cfg: defaultAppConfig(), cursor: -1}
	body := "vless://uuid@vless.example.com:443#VLESS\nvmess://" + base64.StdEncoding.EncodeToString([]byte(`{"v":"2","ps":"VM","add":"vmess.example.com","port":"443","id":"uuid-vm","net":"tcp"}`))
	text := base64.StdEncoding.EncodeToString([]byte(body))
	added, skipped, err := a.importShareText(text)
	if err != nil {
		t.Fatal(err)
	}
	if added != 2 || skipped != 0 {
		t.Errorf("added=%d skipped=%d, want 2/0", added, skipped)
	}
}

func TestImportShareTextAppendsDuplicates(t *testing.T) {
	a := &app{cfg: defaultAppConfig(), cursor: -1}
	link := "vless://uuid@vless.example.com:443#VLESS"
	before := len(a.cfg.Servers)
	for i := 0; i < 2; i++ {
		if _, _, err := a.importShareText(link); err != nil {
			t.Fatal(err)
		}
	}
	if len(a.cfg.Servers) != before+2 {
		t.Errorf("duplicates should be appended: want %d, got %d", before+2, len(a.cfg.Servers))
	}
}

func TestImportShareTextEmpty(t *testing.T) {
	a := &app{cfg: defaultAppConfig(), cursor: -1}
	if _, _, err := a.importShareText(""); err == nil {
		t.Error("empty clipboard should fail")
	}
	if _, _, err := a.importShareText("random text without links"); err == nil {
		t.Error("text without links should fail")
	}
}

func TestImportResetsFilter(t *testing.T) {
	a := &app{cfg: defaultAppConfig(), cursor: -1}
	a.serverFilter = "sub-xyz"
	a.serverFilterName = "某订阅"
	if _, _, err := a.importShareText("vless://uuid@vless.example.com:443#VLESS"); err != nil {
		t.Fatal(err)
	}
	if a.serverFilter != serverFilterAll || a.serverFilterName != "所有" {
		t.Errorf("filter should reset to all: %q/%q", a.serverFilter, a.serverFilterName)
	}
}

func TestServerListHasImportAndCopyMenu(t *testing.T) {
	a := &app{cfg: defaultAppConfig(), tab: tabServer, cursor: -1}
	a.cfg.Servers[0].Name = "test-server"
	a.saver.remember(a.cfg)
	tt := ui.NewTester(a.view, 880, 620)
	if !tt.HasText("导入") {
		t.Errorf("missing the import button; texts: %v", tt.Texts())
	}
	if err := tt.RightClick("test-server"); err != nil {
		t.Fatalf("right click: %v", err)
	}
	if !containsMenuItem(tt.Menu(), "复制为 URL") {
		t.Errorf("missing 复制为 URL in the context menu: %v", tt.Menu())
	}
}

func containsMenuItem(items []string, want string) bool {
	for _, s := range items {
		if s == want {
			return true
		}
	}
	return false
}
