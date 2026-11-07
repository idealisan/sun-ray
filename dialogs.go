//go:build !nogui

package main

// dialogs.go shows the modal alerts the app puts up: the config on disk
// that failed to load, and a write that failed. Both are asked in the
// window's own frame, so they follow the app's look.

import (
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// The kinds of alert the app shows.
const (
	alertNone = iota
	alertConfigBroken
	alertSaveFailed
)

// showConfigAlert asks what to do about a config file that could not be
// loaded. Until the user answers, the file stays as it is: the saver is
// blocked, so nothing can replace it. "Create a new config" is the only
// answer that lets the app write again; "Quit" leaves everything alone.
func (a *app) showConfigAlert(c *ui.Context) {
	if a.alertKind != alertConfigBroken {
		return
	}
	switch ui.AlertDialog(c, &a.alertOpen, "配置文件校验失败，无法加载",
		a.configBroken+"\n\n可以退出，或者创建全新配置（将覆盖原文件）。", "退出", "创建全新配置") {
	case 0: // 退出
		a.quitting = true
		mygo.App.Quit()
	case 1: // 创建全新配置
		a.alertKind = alertNone
		a.alertOpen = false
		a.configBroken = ""
		// The user asked for a new config, so writing over the broken
		// file is what they want.
		a.saver.blocked = false
		a.saver.lastJSON = ""
		a.saver.maybeSave(a.cfg, true)
	case ui.AlertDismissed:
		// Escape must not decide: the file stays as it is, and the
		// question stands.
		a.alertOpen = true
	}
}

// showSaveFailed tells the user that the config could not be written. There
// is nothing to do about it, so the alert has one button.
func (a *app) showSaveFailed(c *ui.Context) {
	if a.alertKind != alertSaveFailed {
		return
	}
	if ui.AlertDialog(c, &a.alertOpen, "配置文件写入失败", a.saveFailed, "确定") == 0 {
		a.alertKind = alertNone
		a.alertOpen = false
		a.saveFailed = ""
	}
}

// reportSaveFailed is called when a write fails. It shows the warning once
// the window is up.
func (a *app) reportSaveFailed(err error) {
	if a.win == nil {
		return
	}
	a.win.Update(func() {
		if a.alertKind != alertNone {
			return // another alert is up; it matters more
		}
		a.saveFailed = err.Error()
		a.alertKind = alertSaveFailed
		a.alertOpen = true
	})
}
