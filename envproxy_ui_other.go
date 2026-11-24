//go:build !windows

package main

import "github.com/egoist/mygo/ui"

// envProxyCheckbox is a no-op off Windows: see envproxy_ui_windows.go.
func envProxyCheckbox(*ui.Context, *globalConfig) {}
