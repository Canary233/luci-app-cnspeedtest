//go:build linux

package main

import (
	"net"
	"syscall"
)

func configureBindIface(dialer *net.Dialer) {
	if bindIface == "" {
		return
	}
	dialer.Control = func(network, address string, conn syscall.RawConn) error {
		var sockErr error
		err := conn.Control(func(fd uintptr) {
			sockErr = syscall.SetsockoptString(int(fd), syscall.SOL_SOCKET, syscall.SO_BINDTODEVICE, bindIface)
		})
		if err != nil {
			return err
		}
		return sockErr
	}
}

func bindIfaceSupported() bool {
	return true
}
