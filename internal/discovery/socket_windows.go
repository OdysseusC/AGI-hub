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
		socket := syscall.Handle(fd)
		var source [4]byte
		copy(source[:], ip.To4())
		optionErr = syscall.SetsockoptInet4Addr(socket, syscall.IPPROTO_IP, syscall.IP_MULTICAST_IF, source)
		if optionErr == nil {
			optionErr = syscall.SetsockoptInt(socket, syscall.IPPROTO_IP, syscall.IP_MULTICAST_TTL, 255)
		}
		// IP_IFLIST restricts reception, unlike IP_MULTICAST_IF, which only
		// selects the outgoing interface. Available on Windows 10 1803+.
		if optionErr == nil {
			optionErr = syscall.SetsockoptInt(socket, syscall.IPPROTO_IP, 28, 1)
		}
		if optionErr == nil {
			optionErr = syscall.SetsockoptInt(socket, syscall.IPPROTO_IP, 29, iface.Index)
		}
	})
	if err != nil {
		return err
	}
	return optionErr
}

func scopedPacketConn(conn *net.UDPConn, _ *net.Interface) net.PacketConn { return conn }
