package main

// speedtest.go measures latency and download speed for a server through a
// temporary in-process xray instance bound to a random loopback port, so
// the measurement never touches the port the user configures and never
// listens on anything but 127.0.0.1.

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/net/proxy"

	"github.com/xtls/xray-core/common/log"
)

const (
	latencySamples = 3 // take the median of three probes
	latencyTimeout = 8 * time.Second
	latencyTarget  = "cp.cloudflare.com:80"
	speedDuration  = 10 * time.Second
	speedParallel  = 3
	speedURL       = "https://speed.cloudflare.com/__down?bytes=52428800" // 50 MB
)

// testState is the per-server measurement result shown in the list.
type testState struct {
	running bool
	failed  bool
	latency time.Duration
	speed   float64 // bytes per second
	err     string
}

// freeLoopbackPort returns a port that was free a moment ago on the
// loopback interface, so a temporary core can bind it.
func freeLoopbackPort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}

// socksDialer builds a SOCKS5 dialer for the temporary core.
func socksDialer(socksAddr string) (proxy.ContextDialer, error) {
	d, err := proxy.SOCKS5("tcp", socksAddr, nil, proxy.Direct)
	if err != nil {
		return nil, err
	}
	cd, ok := d.(proxy.ContextDialer)
	if !ok {
		return nil, fmt.Errorf("socks dialer is not context aware")
	}
	return cd, nil
}

// measureLatency returns the median time until the first byte of the
// response arrives, as seen by the client through the proxy. Every
// protocol can be measured this way, including QUIC based ones.
func measureLatency(socksAddr string) (time.Duration, error) {
	dialer, err := socksDialer(socksAddr)
	if err != nil {
		return 0, err
	}
	var samples []time.Duration
	for i := 0; i < latencySamples; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), latencyTimeout)
		start := time.Now()
		conn, err := dialer.DialContext(ctx, "tcp", latencyTarget)
		if err != nil {
			cancel()
			return 0, err
		}
		conn.SetDeadline(time.Now().Add(latencyTimeout))
		_, werr := io.WriteString(conn, "GET /generate_204 HTTP/1.1\r\nHost: cp.cloudflare.com\r\nUser-Agent: sun-ray\r\nConnection: close\r\n\r\n")
		if werr != nil {
			conn.Close()
			cancel()
			return 0, werr
		}
		var one [1]byte
		_, rerr := io.ReadFull(conn, one[:])
		conn.Close()
		cancel()
		if rerr != nil {
			return 0, rerr
		}
		samples = append(samples, time.Since(start))
	}
	return medianDuration(samples), nil
}

// medianDuration returns the middle value of the samples.
func medianDuration(samples []time.Duration) time.Duration {
	if len(samples) == 0 {
		return 0
	}
	sorted := append([]time.Duration(nil), samples...)
	for i := 1; i < len(sorted); i++ {
		for j := i; j > 0 && sorted[j] < sorted[j-1]; j-- {
			sorted[j], sorted[j-1] = sorted[j-1], sorted[j]
		}
	}
	return sorted[len(sorted)/2]
}

// counter counts bytes written through it.
type counter struct{ n atomic.Int64 }

func (c *counter) Write(p []byte) (int, error) {
	c.n.Add(int64(len(p)))
	return len(p), nil
}

// measureSpeed downloads through the proxy until the time is up and
// returns the average bytes per second over all connections.
func measureSpeed(socksAddr string) (float64, error) {
	dialer, err := socksDialer(socksAddr)
	if err != nil {
		return 0, err
	}
	transport := &http.Transport{
		DialContext:         dialer.DialContext,
		DisableKeepAlives:   true,
		TLSHandshakeTimeout: 10 * time.Second,
	}
	client := &http.Client{Transport: transport}
	ctx, cancel := context.WithTimeout(context.Background(), speedDuration)
	defer cancel()

	var total counter
	var wg sync.WaitGroup
	var failed atomic.Bool
	for i := 0; i < speedParallel; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req, err := http.NewRequestWithContext(ctx, "GET", speedURL, nil)
			if err != nil {
				failed.Store(true)
				return
			}
			resp, err := client.Do(req)
			if err != nil {
				if ctx.Err() == nil {
					failed.Store(true)
				}
				return
			}
			defer resp.Body.Close()
			io.Copy(&total, resp.Body)
		}()
	}
	wg.Wait()
	if total.n.Load() == 0 {
		if failed.Load() {
			return 0, fmt.Errorf("测速请求失败")
		}
		return 0, fmt.Errorf("没有收到数据")
	}
	return float64(total.n.Load()) / speedDuration.Seconds(), nil
}

// formatLatency renders a duration for the list column.
func formatLatency(d time.Duration) string {
	if d <= 0 {
		return "-"
	}
	if d < time.Millisecond {
		return fmt.Sprintf("%dµs", d.Microseconds())
	}
	return fmt.Sprintf("%dms", d.Milliseconds())
}

// formatSpeed renders a transfer rate for the list column.
func formatSpeed(bytesPerSec float64) string {
	if bytesPerSec <= 0 {
		return "-"
	}
	units := []string{"B/s", "KB/s", "MB/s", "GB/s"}
	value := bytesPerSec
	for _, unit := range units {
		if value < 1024 {
			return fmt.Sprintf("%.1f %s", value, unit)
		}
		value /= 1024
	}
	return fmt.Sprintf("%.1f TB/s", value/1024)
}

// tempPortString renders a port for the temporary core config.
func tempPortString(port int) string {
	return strconv.Itoa(port)
}

// testStateOf returns the measurement state of a server, if any.
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
	g := &globalConfig{ListenPort: tempPortString(port), Sniffing: false, LogLevel: "info"}
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
