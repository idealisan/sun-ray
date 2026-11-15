//go:build webui

package main

// Command sun-ray-webui serves the Sun Ray interface as a web page embedded
// in the binary. It is a third, separate build variant: Linux-only in the
// releases (amd64/arm64/armv7), but it builds and runs on any Go platform so
// the page can be inspected during development.
//
// The layout follows the GUI (main.go's view and its tabs): the GUI is
// authoritative. See AGENTS.md.
//
// Usage:
//   sun-ray-webui                       serve on 127.0.0.1:3785
//   sun-ray-webui -host HOST -port N    listen elsewhere
//   sun-ray-webui -config FILE          use another config file
//   sun-ray-webui -test-mode            skip the single-instance lock (dev)
//   sun-ray-webui -version              print the version and exit

import (
	_ "embed"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"
)

// webUIPage is the single self-contained HTML page (inline CSS and vanilla
// JavaScript, no framework, no external requests). It is embedded only in
// this variant: the GUI and CLI builds do not compile this file, so the page
// bytes and the HTTP server never enter their binaries.
//
//go:embed webui/index.html
var webUIPage []byte

func main() {
	// The same executable is the privileged helper when the GUI launches it
	// with -tun-helper; keep that mode working before any web setup.
	if runTunHelperIfRequested() {
		return
	}

	var (
		host       = flag.String("host", "127.0.0.1", "address to listen on (default: IPv4 loopback only)")
		port       = flag.Int("port", 3785, "port to listen on")
		configFile = flag.String("config", "", "config file to run (default: sunray.json in the home directory)")
		version    = flag.Bool("version", false, "print the version and exit")
		testMode   = flag.Bool("test-mode", false, "development/test mode: skip the single-instance lock")
	)
	flag.Parse()

	if *version {
		fmt.Println("Sun Ray " + versionString() + " (webui)")
		return
	}

	if *configFile != "" {
		configFileOverride = *configFile
	}

	// One core per config file, sharing the lock with the GUI/CLI so two
	// instances do not write the same file. -test-mode skips it so a dev
	// build can run beside the installed app.
	if !*testMode && !acquireInstanceLock() {
		log.Fatal("已有 Sun Ray 实例在运行")
	}

	ctrl := newWebController()
	defer ctrl.stopCore()

	addr := net.JoinHostPort(*host, strconv.Itoa(*port))
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatalf("监听 %s 失败: %v", addr, err)
	}

	mux := http.NewServeMux()
	ctrl.routes(mux, webUIPage)
	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}

	go func() {
		if err := srv.Serve(listener); err != nil && err != http.ErrServerClosed {
			log.Printf("webui 服务停止: %v", err)
		}
	}()
	log.Printf("Sun Ray %s (webui) 已在 http://%s 提供服务，配置文件 %s", versionString(), addr, configPath())

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	<-sig
	log.Println("正在停止...")
	_ = srv.Close()
}
