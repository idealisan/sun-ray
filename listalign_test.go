//go:build !nogui

package main

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

// Data cells must line up with their headers: right-aligned columns end at
// the same inset, the centered "当前" column is centered, and left columns
// start together. The cell Box spans the column via FillWidth, so the
// alignment comes from AlignItems(cols[col].Align).
func TestServerCellsMatchHeaderAlignment(t *testing.T) {
	a := &app{cfg: defaultAppConfig(), tab: tabServer, cursor: -1}
	a.cfg.Servers[0].Name = "S1"
	a.cfg.Servers[0].Port = "443"
	a.saver.remember(a.cfg)
	tt := ui.NewTester(a.view, 880, 620)

	header, ok := tt.Find("端口")
	if !ok {
		t.Fatal("port header missing")
	}
	data, ok := tt.Find("443")
	if !ok {
		t.Fatal("port data missing")
	}
	// Both should end at the same right edge (within the cell padding).
	gap := (header.X + header.W) - (data.X + data.W)
	if gap < 0 || gap > 16 {
		t.Errorf("port data is not right-aligned with its header: header right=%.1f data right=%.1f",
			header.X+header.W, data.X+data.W)
	}
}

func TestSubscriptionCellsMatchHeaderAlignment(t *testing.T) {
	a := &app{cfg: defaultAppConfig(), tab: tabSubscription, cursor: -1}
	a.cfg.Subscriptions = []subscriptionConfig{{ID: "sub-1", Name: "Sub", ServerCount: 3}}
	a.saver.remember(a.cfg)
	tt := ui.NewTester(a.view, 880, 620)

	header, ok := tt.Find("节点")
	if !ok {
		t.Fatal("node-count header missing")
	}
	data, ok := tt.Find("3")
	if !ok {
		t.Fatal("node-count data missing")
	}
	gap := (header.X + header.W) - (data.X + data.W)
	if gap < 0 || gap > 16 {
		t.Errorf("node count is not right-aligned with its header: header right=%.1f data right=%.1f",
			header.X+header.W, data.X+data.W)
	}
}
