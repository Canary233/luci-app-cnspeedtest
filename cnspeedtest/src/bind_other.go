//go:build !linux

package main

import "net"

func configureBindIface(dialer *net.Dialer) {
}

func bindIfaceSupported() bool {
	return false
}
