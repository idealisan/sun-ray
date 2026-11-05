//go:build windows

package main

import (
	"fmt"
	"net"

	"golang.org/x/sys/windows"
)

func defaultRouteInterface() (string, error) {
	var index uint32
	err := windows.GetBestInterfaceEx(&windows.SockaddrInet4{
		Addr: [4]byte{8, 8, 8, 8}, Port: 53,
	}, &index)
	if err != nil {
		return "", err
	}
	iface, err := net.InterfaceByIndex(int(index))
	if err != nil {
		return "", err
	}
	if iface.Name == "" {
		return "", fmt.Errorf("默认路由的网卡没有名字")
	}
	return iface.Name, nil
}
