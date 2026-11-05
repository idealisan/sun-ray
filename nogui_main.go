//go:build nogui

package main

// Command sun-ray-nogui runs the same core with the same config
// file, without a user interface: for servers and containers where
// nobody logs in. It reads the sunray.json the GUI writes, so a
// configuration made in the GUI can be copied over and run here, with
// the geo assets already inside the binary.
//
// Usage:
//   sun-ray-nogui                 run the core with the config in the home
//   sun-ray-nogui -config FILE   run it with another config file
//   sun-ray-nogui -test          measure the server, then exit
//   sun-ray-nogui -test-mode     print SUNRAY_READY and wait, for integration tests
//   sun-ray-nogui -version       print the version and exit

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"

	xlog "github.com/xtls/xray-core/common/log"
)

func main() {
	var (
		configFile  = flag.String("config", "", "the config file to run (default: sunray.json in the home directory)")
		showVersion = flag.Bool("version", false, "print the version and exit")
		test        = flag.Bool("test", false, "measure the server latency and speed, then exit")
		testMode    = flag.Bool("test-mode", false, "print SUNRAY_READY and wait, for integration tests")
	)
	flag.Parse()

	if *showVersion {
		fmt.Println("Sun Ray " + versionString())
		return
	}

	path := *configFile
	if path == "" {
		path = configPath()
	}
	var cfg appConfig
	if *testMode {
		loaded, err := loadConfigFile(path)
		if err != nil {
			log.Fatal(err)
		}
		cfg = loaded
	} else {
		cfg = loadConfigFrom(path)
	}
	s := activeServerOf(cfg)
	if s == nil {
		log.Fatalf("no server in %s", path)
	}

	if *test {
		runHeadlessTest(cfg, s)
		return
	}

	if cfg.Global.TunEnabled {
		if msg := tunPrivilegeError(); msg != "" {
			log.Println("提示：" + msg)
		}
	}
	data, err := s.buildJSON(&cfg.Global)
	if err != nil {
		log.Fatal(err)
	}
	inst, err := startInstance(data)
	if err != nil {
		log.Fatal(err)
	}
	defer inst.Close()

	// The core logs to the handler, which prints to the standard output.
	xlog.RegisterHandler(&guiHandler{fn: printLine})
	if *testMode {
		host := "127.0.0.1"
		if cfg.Global.AllowLAN {
			host = "0.0.0.0"
		}
		fmt.Printf("SUNRAY_READY %s:%s\n", host, cfg.Global.ListenPort)
	} else {
		fmt.Printf("Sun Ray %s, no interface, server: %s (%s), local port %s\n",
			versionString(), s.displayName(), s.Protocol, cfg.Global.ListenPort)
	}

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	<-sig
	fmt.Println("stopping...")
}

// printLine writes one log line to the standard output.
func printLine(line string) {
	fmt.Println(strings.TrimRight(line, "\n"))
}

// runHeadlessTest measures the server through a temporary core and prints
// the result, the way the GUI does.
func runHeadlessTest(cfg appConfig, s *serverConfig) {
	port, err := freeLoopbackPort()
	if err != nil {
		log.Fatal(err)
	}
	g := cfg.Global
	g.ListenPort = strconv.Itoa(port)
	data, err := s.buildJSON(&g)
	if err != nil {
		log.Fatal(err)
	}
	inst, err := startInstance(data)
	if err != nil {
		log.Fatal(err)
	}
	defer inst.Close()

	addr := fmt.Sprintf("127.0.0.1:%d", port)
	latency, err := measureLatency(addr)
	if err != nil {
		log.Fatalf("the latency test failed: %v", err)
	}
	fmt.Printf("latency: %s\n", formatLatency(latency))

	speed, err := measureSpeed(addr)
	if err != nil {
		log.Fatalf("the speed test failed: %v", err)
	}
	fmt.Printf("speed: %s\n", formatSpeed(speed))
}
