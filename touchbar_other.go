//go:build !darwin

package main

// Non-macOS builds have no Touch Bar: the whole feature, including its
// settings checkbox, is compiled out so the shared GUI code needs no
// platform branching of its own.

import "github.com/egoist/mygo/ui"

func touchBarCheckbox(*ui.Context, *globalConfig) {}
func touchBarSetEnabled(bool)                     {}
func touchBarSetSpeeds(string, string)            {}
func touchBarShutdown()                           {}
