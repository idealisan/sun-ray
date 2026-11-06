//go:build !nogui

package main

import (
	"fmt"
	"github.com/xtls/xray-core/common/log"
	"time"
)

// testState is the per-server measurement result shown in the list.

func (a *app) testStateOf(id string) *testState {
	if a.tests == nil {
		return nil
	}
	return a.tests[id]
}

// runTests measures the selected servers, or all of them when nothing is
// selected. The servers are tested one after another, so they do not
// compete for bandwidth.
func (a *app) runTests() {
	a.runTestsFor(a.testTargets())
}

// testTargets is the selection the test button acts on, narrowed to the rows
// the current filter shows: rows scrolled out of the viewport still count,
// rows the filter hides do not. When nothing is chosen, the whole filtered
// list is tested.
func (a *app) testTargets() []string {
	if a.chosen.Len() > 0 {
		ids := make([]string, 0, a.chosen.Len())
		for _, row := range a.serverRows {
			if id := a.cfg.Servers[row].ID; a.chosen.Has(id) {
				ids = append(ids, id)
			}
		}
		if len(ids) > 0 {
			return ids
		}
	}
	ids := make([]string, 0, len(a.serverRows))
	for _, row := range a.serverRows {
		ids = append(ids, a.cfg.Servers[row].ID)
	}
	return ids
}

// testTargetsFor is what the context menu of the row id acts on: the whole
// selection when the row is part of it, otherwise just that row. It mirrors
// deleteTargets.
func (a *app) testTargetsFor(id string) []string {
	if a.chosen.Has(id) {
		return a.testTargets()
	}
	return []string{id}
}

// runTestsFor measures the given servers, one after another, so they do
// not compete for bandwidth.
func (a *app) runTestsFor(ids []string) {
	if a.testing {
		return
	}
	if len(ids) == 0 {
		return
	}
	// Copy the servers to measure: the list may change while we run.
	servers := make([]serverConfig, 0, len(ids))
	for _, id := range ids {
		if s, ok := a.serverByID(id); ok {
			servers = append(servers, *s)
		}
	}
	a.testing = true
	go func() {
		defer func() { a.win.Update(func() { a.testing = false }) }()
		for _, s := range servers {
			a.win.Update(func() {
				if a.tests == nil {
					a.tests = map[string]*testState{}
				}
				a.tests[s.ID] = &testState{running: true}
			})
			latency, speed, err := a.measureServer(s)
			a.win.Update(func() {
				if a.tests == nil {
					a.tests = map[string]*testState{}
				}
				st := &testState{latency: latency, speed: speed}
				if err != nil {
					st.failed = true
					st.err = err.Error()
				} else {
					// Keep the measurement, so the list still shows it after a
					// restart and the servers can be compared.
					if a.cfg.Results == nil {
						a.cfg.Results = map[string]serverResult{}
					}
					a.cfg.Results[s.ID] = serverResult{
						LatencyMs: int(latency.Milliseconds()),
						SpeedBps:  speed,
						TestedAt:  time.Now().Unix(),
					}
				}
				a.tests[s.ID] = st
			})
		}
	}()
}

// measureServer starts a temporary core for one server on a random
// loopback port and measures latency and speed through it.
func (a *app) measureServer(s serverConfig) (time.Duration, float64, error) {
	if s.Address == "" || s.Port == "" {
		return 0, 0, fmt.Errorf("服务器地址或端口为空")
	}
	port, err := freeLoopbackPort()
	if err != nil {
		return 0, 0, err
	}
	g := &globalConfig{ListenPort: tempPortString(port), LogLevel: "info"}
	if h := a.currentTunHelper(); h != nil {
		// TUN owns the default route; keep the measurement out of it.
		g.TunBypassInterface = h.settings.Iface
	}
	data, err := s.buildJSON(g)
	if err != nil {
		return 0, 0, err
	}
	a.tempCore.Store(true)
	defer a.tempCore.Store(false)
	inst, err := startInstance(data)
	if err != nil {
		return 0, 0, err
	}
	defer inst.Close()
	// The temporary core took over the log handler; give it back to the GUI.
	log.RegisterHandler(&guiHandler{fn: a.addLog, level: severityForLogLevel(a.cfg.Global.LogLevel)})

	socksAddr := fmt.Sprintf("127.0.0.1:%d", port)
	latency, err := measureLatency(socksAddr)
	if err != nil {
		return 0, 0, err
	}
	speed, err := measureSpeed(socksAddr)
	if err != nil {
		return latency, 0, err
	}
	return latency, speed, nil
}
