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
