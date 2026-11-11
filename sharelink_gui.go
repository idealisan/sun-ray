//go:build !nogui

package main

// sharelink_gui.go holds the parts of share-link import and export that
// touch the GUI app state. The encoders themselves live in sharelink.go so
// the headless build can share them.

import (
	"errors"
	"fmt"
	"strings"

	"github.com/egoist/mygo"
)

var errEmptyImport = errors.New("剪贴板里没有可用的节点链接")

// importShareText parses plain share links or a Base64 subscription body and
// appends every valid node as a new, uncategorized server. It reports how
// many were added and how many lines failed, so the UI can show a result.
func (a *app) importShareText(text string) (int, int, error) {
	decoded := decodeSubscriptionText([]byte(text))
	if strings.TrimSpace(decoded) == "" {
		return 0, 0, errEmptyImport
	}
	var added, skipped int
	for _, line := range strings.Split(decoded, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "//") {
			continue
		}
		if !strings.Contains(line, "://") {
			skipped++
			continue
		}
		s, err := parseShareLink(line)
		if err != nil {
			skipped++
			continue
		}
		s.ID = newServerID()
		s.SubscriptionID = ""
		a.cfg.Servers = append(a.cfg.Servers, s)
		added++
	}
	if added == 0 {
		return 0, skipped, errEmptyImport
	}
	// A filter other than "all" could hide the imported rows; show them.
	a.serverFilter = serverFilterAll
	a.serverFilterName = "所有"
	a.chosen.Clear()
	a.cursor = -1
	return added, skipped, nil
}

// copyShareLinks writes the given servers' share URLs to the clipboard, one
// per line. The caller must hold a real window, since the clipboard needs
// the app's main thread.
func (a *app) copyShareLinks(servers []serverConfig) (int, []string) {
	links, failures := shareLinksForServers(servers)
	if links == "" {
		return 0, failures
	}
	mygo.Clipboard.WriteText(links)
	return len(servers) - len(failures), failures
}

// copySelectedURLs exports the right-clicked row, or the whole selection
// when that row is part of it, matching the test and delete menu items.
func (a *app) copySelectedURLs(id string) {
	ids := a.testTargetsFor(id)
	servers := make([]serverConfig, 0, len(ids))
	for _, sid := range ids {
		if s, ok := a.serverByID(sid); ok {
			servers = append(servers, *s)
		}
	}
	copied, failures := a.copyShareLinks(servers)
	if copied > 0 {
		a.addLog(fmt.Sprintf("[sun-ray] 已复制 %d 个节点的分享链接到剪贴板", copied))
	}
	for _, f := range failures {
		a.addLog("[sun-ray] 无法导出：" + f)
	}
}

// importFromClipboard reads share links from the system clipboard and adds
// them as new servers, then reports what happened.
func (a *app) importFromClipboard() {
	text := mygo.Clipboard.ReadText()
	added, skipped, err := a.importShareText(text)
	if err != nil {
		a.addLog("[sun-ray] 导入失败：" + err.Error())
		return
	}
	a.addLog(fmt.Sprintf("[sun-ray] 已从剪贴板导入 %d 个节点，跳过 %d 行", added, skipped))
}
