package discovery

import (
	"net"
	"syscall"
)

func configureSocket(conn *net.UDPConn, iface *net.Interface, ip net.IP) error {
	raw, err := conn.SyscallConn()
	if err != nil {
		return err
	}
	var optionErr error
	err = raw.Control(func(fd uintptr) {
		var source [4]byte
		copy(source[:], ip.To4())
		optionErr = syscall.SetsockoptInet4Addr(int(fd), syscall.IPPROTO_IP, syscall.IP_MULTICAST_IF, source)
		if optionErr == nil {
			optionErr = syscall.SetsockoptInt(int(fd), syscall.IPPROTO_IP, syscall.IP_MULTICAST_TTL, 255)
		}
		// IP_BOUND_IF scopes both incoming and outgoing traffic.
		if optionErr == nil {
			optionErr = syscall.SetsockoptInt(int(fd), syscall.IPPROTO_IP, 25, iface.Index)
		}
	})
	if err != nil {
		return err
	}
	return optionErr
}

func scopedPacketConn(conn *net.UDPConn, _ *net.Interface) net.PacketConn { return conn }
