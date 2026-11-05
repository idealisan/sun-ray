package main

// tun.go builds the TUN inbound and checks whether this process may
// create one. The TUN interface takes the whole traffic, so it needs
// elevated rights on every platform.

import (
	"fmt"
	"os"
	"runtime"
	"strings"
)

// The defaults of the TUN interface.
const (
	defaultTunName    = "sunray0"
	defaultTunMTU     = 1500
	defaultTunGateway = "172.19.0.1/30"
)

// tunInbound renders the TUN inbound for the given settings. The
// routes send the traffic into the interface, and xray keeps its own
// connections out of it, so the proxy does not loop into itself.
func tunInbound(g *globalConfig) map[string]any {
	name := strings.TrimSpace(g.TunName)
	if name == "" {
		name = defaultTunName
	}
	mtu := int(g.TunMTU)
	if mtu <= 0 {
		mtu = defaultTunMTU
	}
	gateway := strings.TrimSpace(g.TunGateway)
	if gateway == "" {
		gateway = defaultTunGateway
	}
	routes := []string{"0.0.0.0/0"}
	if g.TunIPv6 {
		routes = append(routes, "::/0")
	}
	return map[string]any{
		"tag": "tun-in", "protocol": "tun", "port": 0,
		"settings": map[string]any{
			"name":                   name,
			"desc":                   "Wintun",
			"mtu":                    mtu,
			"gateway":                []string{gateway},
			"autoSystemRoutingTable": routes,
		},
	}
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
