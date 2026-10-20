package discovery

import (
	"encoding/binary"
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
		// The default Linux IP_MULTICAST_ALL=1 receives groups joined by other
		// sockets/interfaces. Restrict this socket to its selected membership.
		if optionErr == nil {
			optionErr = syscall.SetsockoptInt(int(fd), syscall.IPPROTO_IP, 49, 0)
		}
		if optionErr == nil {
			optionErr = syscall.SetsockoptInt(int(fd), syscall.IPPROTO_IP, syscall.IP_PKTINFO, 1)
		}
	})
	if err != nil {
		return err
	}
	return optionErr
}

type interfaceConn struct {
	*net.UDPConn
	index int
}

func scopedPacketConn(conn *net.UDPConn, iface *net.Interface) net.PacketConn {
	return &interfaceConn{UDPConn: conn, index: iface.Index}
}

func (c *interfaceConn) ReadFrom(b []byte) (int, net.Addr, error) {
	oob := make([]byte, 128)
	for {
		n, oobn, flags, source, err := c.ReadMsgUDP(b, oob)
		if err != nil {
			return 0, nil, err
		}
		if flags&(syscall.MSG_TRUNC|syscall.MSG_CTRUNC) != 0 {
			continue
		}
		messages, err := syscall.ParseSocketControlMessage(oob[:oobn])
		if err == nil && packetInfoMatches(messages, c.index) {
			return n, source, nil
		}
	}
}

func packetInfoMatches(messages []syscall.SocketControlMessage, index int) bool {
	for _, m := range messages {
		if m.Header.Level == syscall.IPPROTO_IP && m.Header.Type == syscall.IP_PKTINFO && len(m.Data) >= 12 {
			return int(binary.NativeEndian.Uint32(m.Data[:4])) == index
		}
	}
	return false
}
