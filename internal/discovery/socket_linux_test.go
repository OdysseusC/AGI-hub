package discovery

import (
	"encoding/binary"
	"syscall"
	"testing"
)

func TestPacketInfoRequiresSelectedInterface(t *testing.T) {
	info := make([]byte, 12)
	binary.NativeEndian.PutUint32(info, 2)
	messages := []syscall.SocketControlMessage{{
		Header: syscall.Cmsghdr{Level: syscall.IPPROTO_IP, Type: syscall.IP_PKTINFO},
		Data:   info,
	}}
	if !packetInfoMatches(messages, 2) {
		t.Fatal("selected interface rejected")
	}
	if packetInfoMatches(messages, 3) || packetInfoMatches(nil, 2) {
		t.Fatal("foreign or missing interface accepted")
	}
	messages[0].Data = info[:3]
	if packetInfoMatches(messages, 2) {
		t.Fatal("short packet info accepted")
	}
}
