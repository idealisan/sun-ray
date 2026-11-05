package main

// pacserver.go serves the proxy auto-config file over HTTP. Browsers on
// Windows do not take a file:// PAC, they need a real URL, so the app
// runs a tiny HTTP server for it while the PAC mode is on.

import (
	"fmt"
	"net"
	"net/http"
	"strconv"
	"sync/atomic"
	"time"
)

// pacPorts are tried for the PAC server, the first free one wins. A
// fixed port keeps the URL in the system settings stable.
var pacPorts = []int{10809, 10810, 10811, 10812}

// pacServer is the local HTTP server that serves the PAC file.
type pacServer struct {
	listener net.Listener
	server   *http.Server
	url      string
	content  atomic.Value // string
}

// startPACServer starts the PAC server for the given local proxy
// address, for example 127.0.0.1:10808.
func startPACServer(proxyAddr string) (*pacServer, error) {
	var listener net.Listener
	var err error
	for _, port := range pacPorts {
		listener, err = net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
		if err == nil {
			break
		}
	}
	if listener == nil {
		listener, err = net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			return nil, err
		}
	}

	p := &pacServer{listener: listener}
	p.setProxy(proxyAddr)
	mux := http.NewServeMux()
	mux.HandleFunc("/pac", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-ns-proxy-autoconfig")
		w.Header().Set("Cache-Control", "no-store")
		content, _ := p.content.Load().(string)
		_, _ = w.Write([]byte(content))
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/pac", http.StatusFound)
	})
	p.server = &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	go p.server.Serve(listener)
	addr := listener.Addr().(*net.TCPAddr)
	p.url = fmt.Sprintf("http://127.0.0.1:%d/pac", addr.Port)
	return p, nil
}

// setProxy points the served PAC at another local proxy address.
func (p *pacServer) setProxy(proxyAddr string) {
	host, port, err := net.SplitHostPort(proxyAddr)
	if err != nil {
		return
	}
	p.content.Store(fmt.Sprintf(pacTemplate, host+":"+port))
}

// stop shuts the PAC server down.
func (p *pacServer) stop() {
	if p == nil {
		return
	}
	if p.server != nil {
		_ = p.server.Close()
	}
	if p.listener != nil {
		_ = p.listener.Close()
	}
}
