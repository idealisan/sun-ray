//go:build !windows

package main

// tun_helper_route.go finds the interface the default route uses, so the
// in-process core can bind its own connections there and stay out of the
// helper's TUN.

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
)

func defaultRouteInterface() (string, error) {
	switch runtime.GOOS {
	case "darwin":
		return darwinDefaultInterface()
	case "linux":
		return linuxDefaultInterface()
	}
	return "", fmt.Errorf("TUN 模式在 %s 上还不支持", runtime.GOOS)
}

func darwinDefaultInterface() (string, error) {
	out, err := exec.Command("/sbin/route", "-n", "get", "default").Output()
	if err != nil {
		return "", err
	}
	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "interface:") {
			name := strings.TrimSpace(strings.TrimPrefix(line, "interface:"))
			if name != "" {
				return name, nil
			}
		}
	}
	return "", errors.New("找不到默认路由的网卡")
}

func linuxDefaultInterface() (string, error) {
	f, err := os.Open("/proc/net/route")
	if err != nil {
		return "", err
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	first := true
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if first {
			first = false
			continue
		}
		if len(fields) < 8 || fields[1] != "00000000" || fields[7] != "00000000" {
			continue
		}
		flags, err := strconv.ParseUint(fields[3], 16, 32)
		if err != nil || flags&0x1 == 0 {
			continue
		}
		return fields[0], nil
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}
	return "", errors.New("找不到默认路由的网卡")
}
