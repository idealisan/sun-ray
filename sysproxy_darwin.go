package main

// The macOS implementation drives networksetup, which writes the
// network service preferences.

import (
	"net"
	"os/exec"
	"strings"
)

// applyDarwinProxy configures every enabled network service.
func applySystemProxyPlatform(action, addr, pacURL string) error {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return err
	}
	services := darwinNetworkServices()
	if len(services) == 0 {
		return nil
	}
	for _, service := range services {
		switch action {
		case proxyModeManual:
			runQuiet("networksetup", "-setwebproxy", service, host, port)
			runQuiet("networksetup", "-setsecureproxy", service, host, port)
			bypass := append([]string{"-setproxybypassdomains", service}, macProxyBypass()...)
			runQuiet("networksetup", bypass...)
		case proxyModePAC:
			runQuiet("networksetup", "-setautoproxyurl", service, pacURL)
			runQuiet("networksetup", "-setautoproxystate", service, "on")
		}
	}
	return nil
}

// clearSystemProxy turns the proxy off on every network service.
func clearSystemProxy() error {
	for _, service := range darwinNetworkServices() {
		runQuiet("networksetup", "-setwebproxystate", service, "off")
		runQuiet("networksetup", "-setsecureproxystate", service, "off")
		runQuiet("networksetup", "-setautoproxystate", service, "off")
	}
	return nil
}

// darwinNetworkServices lists the enabled network services.
func darwinNetworkServices() []string {
	out, err := exec.Command("networksetup", "-listallnetworkservices").Output()
	if err != nil {
		return nil
	}
	var services []string
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "*") || strings.HasPrefix(line, "An asterisk") {
			continue
		}
		services = append(services, line)
	}
	return services
}

// systemProxyIsManual reports whether the system applies the local proxy
// the settings ask for.
func systemProxyIsManual(host, port string) bool {
	values := scutilProxy()
	return values["HTTPEnable"] == "1" && values["HTTPProxy"] == host && values["HTTPPort"] == port &&
		values["HTTPSEnable"] == "1" && values["HTTPSProxy"] == host && values["HTTPSPort"] == port
}

// systemProxyIsPAC reports whether the system applies the PAC URL the app
// serves.
func systemProxyIsPAC(url string) bool {
	values := scutilProxy()
	if values["ProxyAutoConfigEnable"] != "1" {
		return false
	}
	return samePACURL(values["ProxyAutoConfigURLString"], url)
}

// samePACURL compares two PAC URLs without their query, which carries a
// token that changes with every application.
func samePACURL(a, b string) bool {
	if i := strings.IndexByte(a, '?'); i >= 0 {
		a = a[:i]
	}
	if i := strings.IndexByte(b, '?'); i >= 0 {
		b = b[:i]
	}
	return a != "" && a == b
}

// scutilProxy reads the proxy the system applies to the current network.
func scutilProxy() map[string]string {
	values := map[string]string{}
	out, err := exec.Command("scutil", "--proxy").Output()
	if err != nil {
		return values
	}
	for _, line := range strings.Split(string(out), "\n") {
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		key, value = strings.TrimSpace(key), strings.TrimSpace(value)
		if key != "" {
			values[key] = value
		}
	}
	return values
}

// runQuiet runs a command and ignores what it says.
func runQuiet(name string, args ...string) {
	_ = exec.Command(name, args...).Run()
}
