package discovery

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	ServiceName     = "_agihub._tcp.local."
	ProtocolVersion = "1"
	maxPacketSize   = 4096
	maxRecords      = 128
	recordTTL       = 120
	typeA           = 1
	typePTR         = 12
	typeTXT         = 16
	typeAAAA        = 28
	typeSRV         = 33
	typeANY         = 255
)

var errPacket = errors.New("invalid discovery DNS packet")

type question struct {
	name string
	typ  uint16
}

type record struct {
	name   string
	typ    uint16
	ttl    uint32
	flush  bool
	target string
	port   uint16
	txt    map[string]string
	ip     net.IP
}

type packet struct {
	response  bool
	questions []question
	records   []record
}

func validText(s string, limit int) bool {
	if s == "" || len(s) > limit || !utf8.ValidString(s) {
		return false
	}
	for _, r := range s {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}

func validID(s string) bool {
	if len(s) == 0 || len(s) > 128 {
		return false
	}
	for _, c := range s {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
			return false
		}
	}
	return true
}

func usableIP(ip net.IP) bool {
	return ip != nil && !ip.IsLoopback() && !ip.IsUnspecified() && !ip.IsMulticast() && (ip.IsGlobalUnicast() || ip.IsLinkLocalUnicast())
}

func validateAdvertisement(a Advertisement) (net.IP, uint16, error) {
	if !validID(a.HubID) || !validText(a.Name, 128) || a.Protocol != ProtocolVersion {
		return nil, 0, errors.New("discovery requires a valid Hub ID, name and supported protocol")
	}
	host, port, err := net.SplitHostPort(a.Address)
	if err != nil {
		return nil, 0, errors.New("discovery address must be a literal IP and port")
	}
	// The selected interface supplies the zone for link-local IPv6 addresses.
	if i := strings.LastIndexByte(host, '%'); i >= 0 {
		host = host[:i]
	}
	ip := net.ParseIP(host)
	n, err := strconv.ParseUint(port, 10, 16)
	if !usableIP(ip) || err != nil || n == 0 {
		return nil, 0, errors.New("discovery address must be a non-loopback unicast IP and valid port")
	}
	return ip, uint16(n), nil
}

func names(a Advertisement) (string, string) {
	sum := sha256.Sum256([]byte(a.HubID))
	label := fmt.Sprintf("hub-%x", sum[:16])
	return label + "." + ServiceName, label + ".local."
}

func appendName(b []byte, name string) []byte {
	for _, label := range strings.Split(strings.TrimSuffix(name, "."), ".") {
		b = append(b, byte(len(label)))
		b = append(b, label...)
	}
	return append(b, 0)
}

func append16(b []byte, n uint16) []byte { return append(b, byte(n>>8), byte(n)) }
func append32(b []byte, n uint32) []byte {
	return append(b, byte(n>>24), byte(n>>16), byte(n>>8), byte(n))
}

func appendRecord(b []byte, name string, typ uint16, flush bool, ttl uint32, data []byte) []byte {
	b = appendName(b, name)
	b = append16(b, typ)
	class := uint16(1)
	if flush {
		class |= 0x8000
	}
	b = append16(b, class)
	b = append32(b, ttl)
	b = append16(b, uint16(len(data)))
	return append(b, data...)
}

func encodeAnnouncement(a Advertisement, ttl uint32) ([]byte, error) {
	ip, port, err := validateAdvertisement(a)
	if err != nil {
		return nil, err
	}
	instance, host := names(a)
	b := []byte{0, 0, 0x84, 0, 0, 0, 0, 4, 0, 0, 0, 0}
	b = appendRecord(b, ServiceName, typePTR, false, ttl, appendName(nil, instance))
	srv := append16(make([]byte, 4), port)
	b = appendRecord(b, instance, typeSRV, true, ttl, appendName(srv, host))
	var txt []byte
	for _, s := range []string{"hub_id=" + a.HubID, "name=" + a.Name, "protocol=" + a.Protocol} {
		txt = append(txt, byte(len(s)))
		txt = append(txt, s...)
	}
	b = appendRecord(b, instance, typeTXT, true, ttl, txt)
	if ip4 := ip.To4(); ip4 != nil {
		b = appendRecord(b, host, typeA, true, ttl, ip4)
	} else {
		b = appendRecord(b, host, typeAAAA, true, ttl, ip.To16())
	}
	return b, nil
}

func encodeQuery() []byte {
	b := []byte{0, 0, 0, 0, 0, 1, 0, 0, 0, 0, 0, 0}
	b = appendName(b, ServiceName)
	b = append16(b, typePTR)
	return append16(b, 1)
}

// readName accepts only backwards compression references, bounds the expanded
// name and pointer count, and never follows network-controlled offsets blindly.
func readName(b []byte, start int) (string, int, error) {
	pos, next, length := start, -1, 0
	var labels []string
	for steps := 0; steps < 128; steps++ {
		if pos < 12 || pos >= len(b) {
			return "", 0, errPacket
		}
		n := int(b[pos])
		if n&0xc0 == 0xc0 {
			if pos+1 >= len(b) {
				return "", 0, errPacket
			}
			ptr := (n&0x3f)<<8 | int(b[pos+1])
			if ptr < 12 || ptr >= pos {
				return "", 0, errPacket
			}
			if next < 0 {
				next = pos + 2
			}
			pos = ptr
			continue
		}
		if n&0xc0 != 0 || pos+1+n > len(b) {
			return "", 0, errPacket
		}
		pos++
		if n == 0 {
			if next < 0 {
				next = pos
			}
			return strings.ToLower(strings.Join(labels, ".")) + ".", next, nil
		}
		length += n + 1
		if length > 254 {
			return "", 0, errPacket
		}
		label := b[pos : pos+n]
		for _, c := range label {
			if c < 33 || c > 126 || c == '.' || c == '\\' {
				return "", 0, errPacket
			}
		}
		labels = append(labels, string(label))
		pos += n
	}
	return "", 0, errPacket
}

func parsePacket(b []byte) (packet, error) {
	var p packet
	if len(b) < 12 || len(b) > maxPacketSize {
		return p, errPacket
	}
	flags := binary.BigEndian.Uint16(b[2:4])
	// mDNS uses transaction ID zero, opcode zero, and no truncation/rcode.
	if binary.BigEndian.Uint16(b[:2]) != 0 || flags&0x7b0f != 0 {
		return p, errPacket
	}
	p.response = flags&0x8000 != 0
	qd := int(binary.BigEndian.Uint16(b[4:6]))
	rr := int(binary.BigEndian.Uint16(b[6:8])) + int(binary.BigEndian.Uint16(b[8:10])) + int(binary.BigEndian.Uint16(b[10:12]))
	if qd > maxRecords || rr > maxRecords {
		return p, errPacket
	}
	pos := 12
	for i := 0; i < qd; i++ {
		name, next, err := readName(b, pos)
		if err != nil || next+4 > len(b) {
			return packet{}, errPacket
		}
		typ, class := binary.BigEndian.Uint16(b[next:]), binary.BigEndian.Uint16(b[next+2:])
		if class&0x7fff == 1 || class&0x7fff == 255 {
			p.questions = append(p.questions, question{name: name, typ: typ})
		}
		pos = next + 4
	}
	for i := 0; i < rr; i++ {
		name, next, err := readName(b, pos)
		if err != nil || next+10 > len(b) {
			return packet{}, errPacket
		}
		r := record{name: name, typ: binary.BigEndian.Uint16(b[next:]), ttl: binary.BigEndian.Uint32(b[next+4:])}
		class := binary.BigEndian.Uint16(b[next+2:])
		r.flush = class&0x8000 != 0
		begin := next + 10
		end := begin + int(binary.BigEndian.Uint16(b[next+8:]))
		if end > len(b) {
			return packet{}, errPacket
		}
		pos = end
		if class&0x7fff != 1 {
			continue
		}
		switch r.typ {
		case typePTR:
			r.target, next, err = readName(b, begin)
			if err != nil || next != end {
				return packet{}, errPacket
			}
		case typeSRV:
			if begin+6 >= end {
				return packet{}, errPacket
			}
			r.port = binary.BigEndian.Uint16(b[begin+4:])
			r.target, next, err = readName(b, begin+6)
			if err != nil || next != end {
				return packet{}, errPacket
			}
		case typeTXT:
			r.txt = make(map[string]string)
			for j := begin; j < end; {
				n := int(b[j])
				j++
				if n == 0 || j+n > end {
					return packet{}, errPacket
				}
				key, value, ok := strings.Cut(string(b[j:j+n]), "=")
				key = strings.ToLower(key)
				if !ok || key == "" || len(r.txt) >= 16 {
					return packet{}, errPacket
				}
				if _, duplicate := r.txt[key]; duplicate {
					return packet{}, errPacket
				}
				r.txt[key] = value
				j += n
			}
		case typeA, typeAAAA:
			n := 4
			if r.typ == typeAAAA {
				n = 16
			}
			if end-begin != n {
				return packet{}, errPacket
			}
			r.ip = append(net.IP(nil), b[begin:end]...)
		default:
			continue
		}
		p.records = append(p.records, r)
	}
	if pos != len(b) {
		return packet{}, errPacket
	}
	return p, nil
}
