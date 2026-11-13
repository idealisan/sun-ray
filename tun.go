package main

// tun.go builds the TUN inbound and checks whether this process may
// create one. The TUN interface takes the whole traffic, so it needs
// elevated rights on every platform.

import (
	"fmt"
	"net"
	"os"
	"runtime"
	"strconv"
	"strings"
)

// The defaults of the TUN interface.
const (
	defaultTunName    = "sunray0"
	defaultTunMTU     = 1500
	defaultTunGateway = "172.19.0.1/30"
)

// parseTunMTU accepts the text field's value. Out-of-range or malformed
// input is ignored, leaving the previous value in place.
func parseTunMTU(raw string) (float64, bool) {
	mtu, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || mtu < 576 || mtu > 9000 {
		return 0, false
	}
	return float64(mtu), true
}

// tunInbound renders the TUN inbound for the given settings. The
// routes send the traffic into the interface, and xray keeps its own
// connections out of it, so the proxy does not loop into itself.
func tunInbound(g *globalConfig) map[string]any {
	name := tunNameFor(g)
	mtu := int(g.TunMTU)
	if mtu <= 0 {
		mtu = defaultTunMTU
	}
	gateway := strings.TrimSpace(g.TunGateway)
	if gateway == "" {
		gateway = defaultTunGateway
	}
	// Both families, always: a TUN that takes the traffic but leaves IPv6
	// out lets every dual-stack site connect around it. macOS and browsers
	// prefer IPv6, and a blocked site then fails in the TLS handshake
	// instead of going through the proxy.
	routes := []string{"0.0.0.0/0", "::/0"}
	return map[string]any{
		"tag": "tun-in", "protocol": "tun", "port": 0,
		"settings": map[string]any{
			"name":                   name,
			"desc":                   "Wintun",
			"mtu":                    mtu,
			"gateway":                []string{gateway},
			"autoSystemRoutingTable": routes,
			// Keep xray's own connections on the physical interface, so
			// they do not re-enter the TUN. Loopback is skipped by xray.
			"autoOutboundsInterface": "auto",
		},
	}
}

// tunNameFor returns the interface name to create. An explicit name wins;
// otherwise each platform gets a free name of the form it expects.
func tunNameFor(g *globalConfig) string {
	if name := strings.TrimSpace(g.TunName); name != "" {
		return name
	}
	switch runtime.GOOS {
	case "darwin":
		return freeUtunName()
	case "linux", "windows":
		return freeSunrayName()
	}
	return defaultTunName
}

// tunNameHint is the placeholder the settings field shows.
func tunNameHint() string {
	if runtime.GOOS == "darwin" {
		return "utun10"
	}
	return defaultTunName
}

// freeUtunName finds an unused utunN. macOS only accepts that form.
func freeUtunName() string {
	used := interfaceNames()
	for i := 10; i <= 1024; i++ {
		name := "utun" + strconv.Itoa(i)
		if !used[name] {
			return name
		}
	}
	return "utun10"
}

// freeSunrayName finds an unused sunrayN on Linux and Windows.
func freeSunrayName() string {
	used := interfaceNames()
	for i := 0; i <= 9; i++ {
		name := "sunray" + strconv.Itoa(i)
		if !used[name] {
			return name
		}
	}
	return defaultTunName
}

func interfaceNames() map[string]bool {
	used := map[string]bool{}
	ifaces, err := net.Interfaces()
	if err != nil {
		return used
	}
	for _, iface := range ifaces {
		used[iface.Name] = true
	}
	return used
}

// tunPrivilegeError returns why the TUN interface cannot be created, or
// an empty string when this process may create one.
func tunPrivilegeError() string {
	switch runtime.GOOS {
	case "linux":
		f, err := os.OpenFile("/dev/net/tun", os.O_RDWR, 0)
		if err == nil {
			f.Close()
			return ""
		}
		if os.IsPermission(err) {
			return "TUN 模式需要 root 权限（或 CAP_NET_ADMIN），请用 sudo 运行"
		}
		return "无法打开 /dev/net/tun：" + err.Error()
	case "darwin", "freebsd":
		if os.Geteuid() != 0 {
			return "TUN 模式需要 root 权限，请用 sudo 运行"
		}
		return ""
	case "windows":
		if !isElevated() {
			return "TUN 模式需要管理员权限，请以管理员身份运行"
		}
		return ""
	}
	return fmt.Sprintf("TUN 模式在 %s 上还不支持", runtime.GOOS)
}
