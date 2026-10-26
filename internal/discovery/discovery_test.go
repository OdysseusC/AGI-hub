package discovery

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"net"
	"sync"
	"testing"
	"time"
)

func testAdvertisement() Advertisement {
	return Advertisement{HubID: "hub-test", Name: "测试 Hub", Address: "192.168.1.20:7443", Protocol: ProtocolVersion}
}

func TestAdvertisementRoundTripAndMetadata(t *testing.T) {
	for _, address := range []string{"192.168.1.20:7443", "[fd00::123]:7443"} {
		a := testAdvertisement()
		a.Address = address
		wire, err := encodeAnnouncement(a, 120)
		if err != nil {
			t.Fatal(err)
		}
		p, err := parsePacket(wire)
		if err != nil {
			t.Fatal(err)
		}
		if !p.response || len(p.records) != 4 {
			t.Fatalf("wrong announcement: %+v", p)
		}
		c := newCache("another-hub", "eth-test")
		now := time.Unix(1000, 0)
		c.apply(p, now)
		got := c.candidates(now)
		if len(got) != 1 || got[0].Advertisement != a || got[0].Interface != "eth-test" || !got[0].ExpiresAt.Equal(now.Add(120*time.Second)) {
			t.Fatalf("wrong candidate: %+v", got)
		}
		if bytes.Contains(wire, []byte("session")) || bytes.Contains(wire, []byte("token")) || bytes.Contains(wire, []byte("endpoint")) {
			t.Fatal("announcement exposed unrelated metadata")
		}
	}
}

func TestCompressionAndMalformedPackets(t *testing.T) {
	// A real compressed PTR response: pointer to the service owner at offset 12.
	wire := []byte{0, 0, 0x84, 0, 0, 0, 0, 1, 0, 0, 0, 0}
	wire = append(wire, 7)
	wire = append(wire, []byte("_agihub")...)
	wire = append(wire, 4)
	wire = append(wire, []byte("_tcp")...)
	wire = append(wire, 5)
	wire = append(wire, []byte("local")...)
	wire = append(wire, 0, 0, 12, 0, 1, 0, 0, 0, 120, 0, 6, 3, 'h', 'u', 'b', 0xc0, 12)
	p, err := parsePacket(wire)
	if err != nil || len(p.records) != 1 || p.records[0].target != "hub."+ServiceName {
		t.Fatalf("compressed PTR failed: %+v %v", p, err)
	}
	loop := append([]byte(nil), wire[:12]...)
	loop = append(loop, 0xc0, 12)
	for name, packet := range map[string][]byte{
		"empty": nil, "oversized": make([]byte, maxPacketSize+1),
		"pointer loop": loop, "short header": wire[:11],
		"short rdata":     wire[:len(wire)-1],
		"pointer outside": append(append([]byte(nil), wire[:12]...), 0xff, 0xff),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := parsePacket(packet); err == nil {
				t.Fatal("malformed packet accepted")
			}
		})
	}
	for i := 0; i < len(wire); i++ {
		if _, err := parsePacket(wire[:i]); err == nil {
			t.Fatalf("truncation at %d accepted", i)
		}
	}
}

func TestCacheSplitRecordsExpiryGoodbyeAndFiltering(t *testing.T) {
	now := time.Unix(1000, 0)
	a := testAdvertisement()
	wire, _ := encodeAnnouncement(a, 120)
	p, _ := parsePacket(wire)
	c := newCache("self", "eth-test")
	for _, rr := range p.records {
		c.apply(packet{response: true, records: []record{rr}}, now)
	}
	if got := c.candidates(now); len(got) != 1 {
		t.Fatalf("split records not assembled: %+v", got)
	}
	if got := c.candidates(now.Add(120 * time.Second)); len(got) != 0 {
		t.Fatal("expired candidate retained")
	}
	c.apply(p, now)
	goodbye, _ := encodeAnnouncement(a, 0)
	gp, _ := parsePacket(goodbye)
	c.apply(gp, now.Add(time.Second))
	if got := c.candidates(now.Add(2 * time.Second)); len(got) != 0 {
		t.Fatal("goodbye retained beyond grace period")
	}
	self := newCache(a.HubID, "eth-test")
	self.apply(p, now)
	if len(self.candidates(now)) != 0 {
		t.Fatal("self advertisement shown")
	}
	for i := range p.records {
		if p.records[i].typ == typeTXT {
			p.records[i].txt["protocol"] = "unsupported"
		}
	}
	c.apply(p, now)
	if len(c.candidates(now)) != 0 {
		t.Fatal("unsupported protocol shown")
	}
}

func TestInvalidOptionsDoNotOpenSocket(t *testing.T) {
	opened := 0
	deps := testDeps(newFakeConn())
	deps.listen = func(*net.Interface, net.IP) (net.PacketConn, error) {
		opened++
		return nil, errors.New("should not open")
	}
	for _, o := range []Options{
		{}, {Interface: "eth-test"},
		{Interface: "eth-test", Announcement: Advertisement{HubID: "hub", Name: "Test", Address: "127.0.0.1:7443", Protocol: ProtocolVersion}},
		{Interface: "eth-test", Announcement: Advertisement{HubID: "hub", Name: "Test", Address: "192.168.9.2:7443", Protocol: ProtocolVersion}},
	} {
		if _, err := start(context.Background(), o, deps); err == nil {
			t.Fatal("invalid options accepted")
		}
	}
	if opened != 0 {
		t.Fatalf("opened %d sockets before validation", opened)
	}
}

func TestExplicitLifecycleAndContextCancellation(t *testing.T) {
	conn := newFakeConn()
	deps := testDeps(conn)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s, err := start(ctx, Options{Interface: "eth-test", Announcement: testAdvertisement()}, deps)
	if err != nil {
		t.Fatal(err)
	}
	if conn.writeCount() != 2 {
		t.Fatalf("want announcement and query, got %d writes", conn.writeCount())
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	count := conn.writeCount()
	if err := s.Close(); err != nil || conn.writeCount() != count {
		t.Fatal("close must be idempotent")
	}
	last := conn.lastWrite()
	p, err := parsePacket(last)
	if err != nil || !p.response {
		t.Fatalf("goodbye missing: %v", err)
	}
	for _, rr := range p.records {
		if rr.ttl != 0 {
			t.Fatal("close sent a nonzero TTL")
		}
	}
	select {
	case <-s.Done():
	default:
		t.Fatal("close returned before service stopped")
	}
	conn2 := newFakeConn()
	s2, err := start(ctx, Options{Interface: "eth-test", Announcement: testAdvertisement()}, testDeps(conn2))
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	select {
	case <-s2.Done():
	case <-time.After(time.Second):
		t.Fatal("cancel did not stop service")
	}
}

func TestQueryAndCandidateDelivery(t *testing.T) {
	conn := newFakeConn()
	changes := make(chan []Candidate, 4)
	s, err := start(context.Background(), Options{Interface: "eth-test", Announcement: testAdvertisement(), OnChange: func(c []Candidate) { changes <- c }}, testDeps(conn))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	a := testAdvertisement()
	a.HubID, a.Name, a.Address = "other", "Peer", "192.168.1.21:7443"
	wire, _ := encodeAnnouncement(a, 120)
	conn.reads <- wire
	select {
	case got := <-changes:
		if len(got) != 1 || got[0].HubID != "other" {
			t.Fatalf("wrong update: %+v", got)
		}
	case <-time.After(time.Second):
		t.Fatal("candidate not delivered")
	}
	query, err := parsePacket(encodeQuery())
	if err != nil || query.response || len(query.questions) != 1 || query.questions[0].name != ServiceName {
		t.Fatalf("invalid query: %+v %v", query, err)
	}
}

func TestRejectExcessRecordCount(t *testing.T) {
	wire := make([]byte, 12)
	binary.BigEndian.PutUint16(wire[6:], 65535)
	if _, err := parsePacket(wire); err == nil {
		t.Fatal("unbounded record count accepted")
	}
}

func TestCacheUsesShortestTTLAndBoundsMemory(t *testing.T) {
	now := time.Unix(1000, 0)
	wire, _ := encodeAnnouncement(testAdvertisement(), 120)
	p, _ := parsePacket(wire)
	p.records[3].ttl = 2
	c := newCache("self", "eth-test")
	c.apply(p, now)
	if got := c.candidates(now); len(got) != 1 || !got[0].ExpiresAt.Equal(now.Add(2*time.Second)) {
		t.Fatalf("shortest record TTL ignored: %+v", got)
	}
	if len(c.candidates(now.Add(2*time.Second))) != 0 {
		t.Fatal("candidate outlived address record")
	}
	for i := 0; i < maxCacheRecords*2; i++ {
		p.records[0].target = "extra-" + time.Unix(int64(i), 0).Format("150405") + "." + ServiceName
		c.apply(packet{response: true, records: p.records[:1]}, now)
	}
	if len(c.records) > maxCacheRecords {
		t.Fatal("unbounded cache")
	}
}

func TestCancelledStartDoesNotOpenSocket(t *testing.T) {
	conn := newFakeConn()
	deps := testDeps(conn)
	opened := false
	deps.listen = func(*net.Interface, net.IP) (net.PacketConn, error) {
		opened = true
		return conn, nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := start(ctx, Options{Interface: "eth-test", Announcement: testAdvertisement()}, deps); !errors.Is(err, context.Canceled) {
		t.Fatalf("want canceled, got %v", err)
	}
	if opened {
		t.Fatal("canceled Start opened a socket")
	}
}

func TestFailedInitialWriteClosesSocket(t *testing.T) {
	conn := newFakeConn()
	_ = conn.Close()
	s, err := start(context.Background(), Options{Interface: "eth-test", Announcement: testAdvertisement()}, testDeps(conn))
	if err == nil || s != nil {
		t.Fatal("failed socket reported enabled")
	}
}

func TestRuntimeSocketFailureIsObservable(t *testing.T) {
	conn := newFakeConn()
	s, err := start(context.Background(), Options{Interface: "eth-test", Announcement: testAdvertisement()}, testDeps(conn))
	if err != nil {
		t.Fatal(err)
	}
	_ = conn.Close()
	select {
	case <-s.Done():
		if !errors.Is(s.Err(), net.ErrClosed) {
			t.Fatalf("socket error hidden: %v", s.Err())
		}
	case <-time.After(time.Second):
		t.Fatal("socket failure did not stop service")
	}
}

func TestNegativeRecordDetailsAndZone(t *testing.T) {
	wire, _ := encodeAnnouncement(testAdvertisement(), 120)
	for _, mutate := range []func([]byte){
		func(b []byte) { b[2] |= 2 },       // truncated response
		func(b []byte) { b[0] = 1 },        // unicast transaction ID
		func(b []byte) { b[len(b)-5] = 3 }, // invalid A RDLENGTH
	} {
		b := append([]byte(nil), wire...)
		mutate(b)
		if _, err := parsePacket(b); err == nil {
			t.Fatal("invalid DNS detail accepted")
		}
	}
	a := testAdvertisement()
	a.Address = "[fe80::1%wrong-interface]:7443"
	if _, err := start(context.Background(), Options{Interface: "eth-test", Announcement: a}, testDeps(newFakeConn())); err == nil {
		t.Fatal("foreign IPv6 zone accepted")
	}
}

func FuzzParsePacket(f *testing.F) {
	wire, _ := encodeAnnouncement(testAdvertisement(), 120)
	f.Add(wire)
	f.Add(encodeQuery())
	f.Add([]byte{0, 0, 0x84, 0, 0, 0, 0, 1, 0, 0, 0, 0, 0xc0, 12})
	f.Fuzz(func(t *testing.T, data []byte) {
		p, err := parsePacket(data)
		if err == nil {
			c := newCache("self", "eth-test")
			c.apply(p, time.Unix(1000, 0))
			_ = c.candidates(time.Unix(1001, 0))
		}
	})
}

func testDeps(conn net.PacketConn) dependencies {
	return dependencies{
		lookup: func(name string) (*net.Interface, []net.Addr, error) {
			return &net.Interface{Name: name, Index: 2, Flags: net.FlagUp | net.FlagMulticast}, []net.Addr{&net.IPNet{IP: net.ParseIP("192.168.1.20"), Mask: net.CIDRMask(24, 32)}}, nil
		},
		listen: func(*net.Interface, net.IP) (net.PacketConn, error) { return conn, nil },
		now:    time.Now,
	}
}

type fakeConn struct {
	mu     sync.Mutex
	reads  chan []byte
	closed chan struct{}
	writes [][]byte
	once   sync.Once
}

func newFakeConn() *fakeConn {
	return &fakeConn{reads: make(chan []byte, 8), closed: make(chan struct{})}
}
func (c *fakeConn) ReadFrom(b []byte) (int, net.Addr, error) {
	select {
	case wire := <-c.reads:
		return copy(b, wire), &net.UDPAddr{IP: net.ParseIP("192.168.1.21"), Port: 5353}, nil
	case <-c.closed:
		return 0, nil, net.ErrClosed
	}
}
func (c *fakeConn) WriteTo(b []byte, addr net.Addr) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	select {
	case <-c.closed:
		return 0, net.ErrClosed
	default:
	}
	c.writes = append(c.writes, append([]byte(nil), b...))
	return len(b), nil
}
func (c *fakeConn) Close() error {
	c.once.Do(func() { close(c.closed) })
	return nil
}
func (c *fakeConn) LocalAddr() net.Addr              { return &net.UDPAddr{Port: 5353} }
func (c *fakeConn) SetDeadline(time.Time) error      { return nil }
func (c *fakeConn) SetReadDeadline(time.Time) error  { return nil }
func (c *fakeConn) SetWriteDeadline(time.Time) error { return nil }
func (c *fakeConn) writeCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.writes)
}
func (c *fakeConn) lastWrite() []byte {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]byte(nil), c.writes[len(c.writes)-1]...)
}
