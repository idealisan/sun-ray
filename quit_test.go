//go:build !nogui

package main

import (
	"testing"

	"github.com/egoist/mygo"
)

// A quit the user asked for (Cmd+Q, the Dock's Quit, AppleScript) only puts
// the window away: the app keeps running in the status bar, so the proxy
// stays up. A logout, restart or shutdown quits for good, or macOS would
// name the app as the one that prevented it, and so does the tray's own
// Quit, which App.Quit asks for.
func TestQuitRequested(t *testing.T) {
	tests := []struct {
		name      string
		reason    mygo.QuitReason
		tray      bool
		prevented bool
	}{
		{"a user quit keeps the app in the status bar", mygo.QuitReasonUser, true, true},
		{"a logout or shutdown quits for good", mygo.QuitReasonSystemPowerOff, true, false},
		{"the tray's Quit quits for good", mygo.QuitReasonApp, true, false},
		{"without a tray a user quit quits", mygo.QuitReasonUser, false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := &app{cfg: defaultAppConfig(), cursor: -1}
			if tt.tray {
				a.tray = &mygo.Tray{}
			}
			e := &mygo.QuitEvent{Reason: tt.reason}
			a.onQuitRequested(e)
			if e.DefaultPrevented() != tt.prevented {
				t.Errorf("prevented = %v, want %v", e.DefaultPrevented(), tt.prevented)
			}
		})
	}
}
