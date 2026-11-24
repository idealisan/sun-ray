//go:build windows

package main

import "github.com/egoist/mygo/ui"

// envProxyCheckbox only exists on Windows, the only platform where the app
// can actually set the user's shell environment. Elsewhere the checkbox is
// compiled out, leaving just the "copy commands" button.
func envProxyCheckbox(c *ui.Context, g *globalConfig) {
	ui.Checkbox(c, &g.EnvProxy, "设置代理环境变量（终端可用）")
}
