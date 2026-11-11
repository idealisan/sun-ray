//go:build !nogui

package main

import (
	"testing"

	"github.com/egoist/mygo"
)

// A quit the system asked for (Cmd+Q, the Dock's Quit) only puts the window
// away: the app keeps running in the status bar, so the proxy stays up.
func TestSystemQuitKeepsRunning(t *testing.T) {
	a := &app{cfg: defaultAppConfig(), cursor: -1}
	a.tray = &mygo.Tray{} // a tray exists, so there is a way back

	e := &mygo.QuitEvent{}
	a.onQuitRequested(e)
	if !e.DefaultPrevented() {
		t.Error("the quit should have been prevented")
	}
	if a.quitting {
		t.Error("the app should not be quitting")
	}
	if a.quitRequested {
		t.Error("this was not a real quit")
	}
}

// The tray's own Quit ends the app for good.
func TestRealQuitProceeds(t *testing.T) {
	a := &app{cfg: defaultAppConfig(), cursor: -1}
	a.tray = &mygo.Tray{}
	a.quitRequested = true

	e := &mygo.QuitEvent{}
	a.onQuitRequested(e)
	if e.DefaultPrevented() {
		t.Error("a real quit should not be prevented")
	}
	if !a.quitting {
		t.Error("the app should be quitting")
	}
}

// Without a tray there would be no way back, so the window close quits.
func TestQuitWithoutTrayQuits(t *testing.T) {
	a := &app{cfg: defaultAppConfig(), cursor: -1}
	a.tray = nil

	e := &mygo.QuitEvent{}
	a.onQuitRequested(e)
	if !e.DefaultPrevented() {
		t.Error("the quit should have been prevented")
	}
	if !a.quitRequested {
		t.Error("without a tray the app should quit for good")
	}
}
