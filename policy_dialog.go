//go:build !nogui

package main

// policy_dialog.go is the test window behind the policy page's Test
// button: it says which rule would take a domain or an address, and where.

import (
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

func (a *app) openPolicyTest() {
	input := ""
	var result policyMatch
	errText := ""
	tested := false

	// Created from another goroutine so the window opens after this build
	// pass, like the server editor.
	go func() {
		var win *mygo.Window
		win = mygo.NewWindow(mygo.WindowOptions{
			Title:    "测试策略",
			Width:    480,
			Height:   260,
			MinWidth: 400, MinHeight: 220,
			Content: ui.View(func(c *ui.Context) {
				t := c.Theme()
				ui.Column(c).Fill().Padding(12, 16).Gap(10).Children(func() {
					ui.Text(c, "输入域名或 IP，看看哪条规则会处理它").TextColor(t.TextMuted)
					ui.Row(c).Gap(8).Children(func() {
						field := ui.TextInput(c, &input).
							Placeholder("example.com 或 10.0.0.0/8").
							Grow(1).
							AutoFocus()
						if ui.PrimaryButton(c.Key("poltestrun"), "测试").Clicked() || field.Submitted() {
							m, err := a.matchPolicy(input)
							tested = true
							if err != nil {
								result, errText = policyMatch{}, err.Error()
							} else {
								result, errText = m, ""
							}
						}
					})
					if !tested {
						return
					}
					if errText != "" {
						ui.Text(c, errText).TextColor(t.Danger)
						return
					}
					ui.Text(c, result.Source).Bold()
					ui.Row(c).Gap(4).Children(func() {
						ui.Text(c, "动作：").TextColor(t.TextMuted)
						ui.Text(c, policyActionLabel(result.Action)).Bold().TextColor(policyActionColor(t, result.Action))
					})
					if result.Note != "" {
						ui.Text(c, result.Note).TextColor(t.TextMuted)
					}
				})
			}),
		})
		_ = win.SetIcon(appIconPNG)
	}()
}

// policyActionColor tells the three actions apart in the test window.
func policyActionColor(t *ui.Theme, action string) ui.Color {
	switch action {
	case policyActionDirect:
		return t.Accent
	case policyActionBlock:
		return t.Danger
	default:
		return t.Text
	}
}
