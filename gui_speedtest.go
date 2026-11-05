//go:build !nogui

package main

import (
	"fmt"
	"net"
	"sync/atomic"
	"time"

	"golang.org/x/net/proxy"
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
	ids := a.selectedIDs()
	if len(ids) == 0 {
		ids = make([]string, 0, len(a.cfg.Servers))
		for i := range a.cfg.Servers {
			ids = append(ids, a.cfg.Servers[i].ID)
		}
	}
	a.runTestsFor(ids)
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
	log.RegisterHandler(&guiHandler{fn: a.addLog})

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
