//go:build !windows && !linux && !darwin

package discovery

import (
	"errors"
	"net"
)

func configureSocket(*net.UDPConn, *net.Interface, net.IP) error {
	return errors.New("interface-scoped mDNS is supported on Windows, Linux and macOS")
}

func scopedPacketConn(conn *net.UDPConn, _ *net.Interface) net.PacketConn { return conn }
