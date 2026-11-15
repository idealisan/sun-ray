//go:build !nogui && !webui

package main

// subscription.go is the GUI's subscription orchestration: picking which
// subscriptions to refresh, doing it off the UI thread, and hopping back to
// the window to apply the nodes. The fetching/parsing/merging logic lives in
// subscription_shared.go so the webui uses exactly the same code.

import "fmt"

func (a *app) updateSelectedSubscriptions() {
	ids := a.selectedSubscriptionIDs()
	if len(ids) == 0 {
		for i := range a.cfg.Subscriptions {
			ids = append(ids, a.cfg.Subscriptions[i].ID)
		}
	}
	a.updateSubscriptions(ids)
}

func (a *app) updateSubscriptions(ids []string) {
	if len(a.subUpdating) > 0 {
		return
	}
	subs := make([]subscriptionConfig, 0, len(ids))
	for _, id := range ids {
		if s, ok := a.subscriptionByID(id); ok {
			subs = append(subs, *s)
		}
	}
	if len(subs) == 0 {
		return
	}
	a.subUpdating = make(map[string]bool, len(subs))
	for _, s := range subs {
		a.subUpdating[s.ID] = true
	}
	go func() {
		for _, s := range subs {
			nodes, err := fetchSubscriptionNodes(s)
			a.updateUI(func() {
				if cur, ok := a.subscriptionByID(s.ID); ok {
					if err != nil {
						cur.LastError = err.Error()
						a.addLog("[订阅] " + cur.displayName() + " 更新失败: " + err.Error())
					} else {
						a.applySubscriptionNodes(cur, nodes)
						a.addLog(fmt.Sprintf("[订阅] %s 已更新：%d 个节点", cur.displayName(), len(nodes)))
					}
				}
				delete(a.subUpdating, s.ID)
				a.saver.maybeSave(a.cfg, true)
			})
		}
	}()
}

func (a *app) updateUI(fn func()) {
	if a.win == nil {
		fn()
		return
	}
	a.win.Update(fn)
}

func (a *app) applySubscriptionNodes(sub *subscriptionConfig, nodes []serverConfig) {
	applySubscriptionNodesTo(&a.cfg, sub, nodes)
	a.updateTrayTip()
}
