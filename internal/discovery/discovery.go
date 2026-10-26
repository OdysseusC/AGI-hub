// Package discovery provides opt-in, interface-scoped DNS-SD discovery.
// Importing this package, enumerating interfaces, and constructing options never
// open sockets. Only Start enables multicast. Candidates are untrusted hints;
// discovery neither establishes trust nor connects to advertised addresses.
package discovery

import (
	"context"
	"errors"
	"fmt"
	"net"
	"reflect"
	"sort"
	"strings"
	"sync"
	"time"
)

type Advertisement struct {
	HubID    string `json:"hub_id"`
	Name     string `json:"name"`
	Address  string `json:"address"`
	Protocol string `json:"protocol"`
}

type Candidate struct {
	Advertisement
	Interface string    `json:"interface"`
	LastSeen  time.Time `json:"last_seen"`
	ExpiresAt time.Time `json:"expires_at"`
}

type Options struct {
	// Interface is the explicit OS interface name, never empty or "all".
	Interface    string
	Announcement Advertisement
	// OnChange runs serially on the service worker. It must return promptly and
	// must not synchronously call Close. The slice belongs to the callback.
	OnChange func([]Candidate)
}

type NetworkInterface struct {
	Name      string   `json:"name"`
	Index     int      `json:"index"`
	Addresses []string `json:"addresses"`
}

// Interfaces returns active multicast-capable non-loopback interfaces with
// IPv4 connectivity. This first implementation transports mDNS over IPv4 and
// can announce or discover either A or AAAA service addresses.
func Interfaces() ([]NetworkInterface, error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil, err
	}
	out := make([]NetworkInterface, 0)
	for _, iface := range ifaces {
		if !eligible(iface) {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		item := NetworkInterface{Name: iface.Name, Index: iface.Index, Addresses: make([]string, 0)}
		has4 := false
		for _, addr := range addrs {
			ip := addressIP(addr)
			if usableIP(ip) {
				item.Addresses = append(item.Addresses, ip.String())
				has4 = has4 || ip.To4() != nil
			}
		}
		if has4 {
			sort.Strings(item.Addresses)
			out = append(out, item)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Index < out[j].Index })
	return out, nil
}

func eligible(i net.Interface) bool {
	return i.Flags&net.FlagUp != 0 && i.Flags&net.FlagMulticast != 0 && i.Flags&net.FlagLoopback == 0
}

func addressIP(a net.Addr) net.IP {
	switch a := a.(type) {
	case *net.IPNet:
		return a.IP
	case *net.IPAddr:
		return a.IP
	}
	return nil
}

type dependencies struct {
	lookup func(string) (*net.Interface, []net.Addr, error)
	listen func(*net.Interface, net.IP) (net.PacketConn, error)
	now    func() time.Time
}

type Service struct {
	cancel context.CancelFunc
	done   chan struct{}
	mu     sync.RWMutex
	err    error
}

// Start explicitly enables discovery. Announcement.Address must belong to the
// selected interface. Unsupported platforms or interface filtering failures are
// errors; discovery never falls back to listening on every interface.
func Start(ctx context.Context, options Options) (*Service, error) {
	return start(ctx, options, dependencies{
		lookup: func(name string) (*net.Interface, []net.Addr, error) {
			i, err := net.InterfaceByName(name)
			if err != nil {
				return nil, nil, err
			}
			addrs, err := i.Addrs()
			return i, addrs, err
		},
		listen: listenMulticast,
		now:    time.Now,
	})
}

func start(parent context.Context, options Options, deps dependencies) (*Service, error) {
	if parent == nil {
		return nil, errors.New("discovery requires a context")
	}
	if err := parent.Err(); err != nil {
		return nil, err
	}
	if strings.TrimSpace(options.Interface) == "" {
		return nil, errors.New("select a network interface before enabling discovery")
	}
	ip, _, err := validateAdvertisement(options.Announcement)
	if err != nil {
		return nil, err
	}
	host, _, _ := net.SplitHostPort(options.Announcement.Address)
	if at := strings.LastIndexByte(host, '%'); at >= 0 && host[at+1:] != options.Interface {
		return nil, errors.New("address zone must match the selected interface")
	}
	iface, addrs, err := deps.lookup(options.Interface)
	if err != nil {
		return nil, fmt.Errorf("discovery interface: %w", err)
	}
	if !eligible(*iface) {
		return nil, errors.New("discovery requires an active multicast-capable LAN interface")
	}
	var ipv4 net.IP
	matches := false
	for _, addr := range addrs {
		local := addressIP(addr)
		if local.Equal(ip) {
			matches = true
		}
		if usableIP(local) && local.To4() != nil && ipv4 == nil {
			ipv4 = local.To4()
		}
	}
	if !matches || ipv4 == nil {
		return nil, errors.New("discovery address must belong to the selected interface, which must have IPv4 connectivity")
	}
	announcement, _ := encodeAnnouncement(options.Announcement, recordTTL)
	goodbye, _ := encodeAnnouncement(options.Announcement, 0)
	conn, err := deps.listen(iface, ipv4)
	if err != nil {
		return nil, fmt.Errorf("start discovery: %w", err)
	}
	// Startup failure is synchronous, so the caller cannot show enabled state
	// merely because a socket could be opened.
	for _, wire := range [][]byte{announcement, encodeQuery()} {
		if err := send(conn, wire); err != nil {
			_ = conn.Close()
			return nil, fmt.Errorf("start discovery: %w", err)
		}
	}
	ctx, cancel := context.WithCancel(parent)
	s := &Service{cancel: cancel, done: make(chan struct{})}
	go s.run(ctx, conn, options, deps.now, announcement, goodbye)
	return s, nil
}

var multicastAddress = &net.UDPAddr{IP: net.IPv4(224, 0, 0, 251), Port: 5353}

func send(conn net.PacketConn, wire []byte) error {
	if err := conn.SetWriteDeadline(time.Now().Add(time.Second)); err != nil {
		return err
	}
	_, err := conn.WriteTo(wire, multicastAddress)
	return err
}

type received struct {
	packet packet
	err    error
}

func (s *Service) run(ctx context.Context, conn net.PacketConn, options Options, now func() time.Time, announcement, goodbye []byte) {
	incoming := make(chan received, 16)
	readerDone := make(chan struct{})
	go func() {
		defer close(readerDone)
		buffer := make([]byte, maxPacketSize+1)
		for {
			n, source, err := conn.ReadFrom(buffer)
			if err != nil {
				select {
				case incoming <- received{err: err}:
				case <-ctx.Done():
				}
				return
			}
			udp, ok := source.(*net.UDPAddr)
			if !ok || udp.Port != 5353 || !usableIP(udp.IP) {
				continue
			}
			p, err := parsePacket(buffer[:n])
			if err != nil {
				continue
			}
			select {
			case incoming <- received{packet: p}:
			case <-ctx.Done():
				return
			}
		}
	}()
	defer func() {
		s.cancel()
		_ = send(conn, goodbye)
		_ = conn.Close()
		<-readerDone
		close(s.done)
	}()
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	peers := newCache(options.Announcement.HubID, options.Interface)
	lastReply := now()
	refresh := now().Add(time.Second) // second startup announcement
	var previous []Candidate
	notify := func(at time.Time) {
		current := peers.candidates(at)
		if len(previous) == 0 && len(current) == 0 {
			return
		}
		if !reflect.DeepEqual(previous, current) {
			previous = append([]Candidate(nil), current...)
			if options.OnChange != nil {
				options.OnChange(current)
			}
		}
	}
	for {
		select {
		case <-ctx.Done():
			return
		case event := <-incoming:
			if event.err != nil {
				s.setError(event.err)
				return
			}
			at := now()
			if event.packet.response {
				peers.apply(event.packet, at)
				notify(at)
			} else if at.Sub(lastReply) >= time.Second && relevantQuestion(event.packet, options.Announcement) {
				if err := send(conn, announcement); err != nil {
					s.setError(err)
					return
				}
				lastReply = at
			}
		case <-tick.C:
			at := now()
			notify(at)
			if !at.Before(refresh) {
				for _, wire := range [][]byte{announcement, encodeQuery()} {
					if err := send(conn, wire); err != nil {
						s.setError(err)
						return
					}
				}
				lastReply = at
				refresh = at.Add(60 * time.Second)
			}
		}
	}
}

func relevantQuestion(p packet, a Advertisement) bool {
	instance, host := names(a)
	for _, q := range p.questions {
		if q.name == ServiceName && (q.typ == typePTR || q.typ == typeANY) ||
			q.name == instance && (q.typ == typeSRV || q.typ == typeTXT || q.typ == typeANY) ||
			q.name == host && (q.typ == typeA || q.typ == typeAAAA || q.typ == typeANY) {
			return true
		}
	}
	return false
}

func (s *Service) setError(err error) {
	s.mu.Lock()
	s.err = err
	s.mu.Unlock()
}

// Close withdraws the announcement and waits for socket and worker shutdown.
// It is safe to call more than once.
func (s *Service) Close() error {
	s.cancel()
	<-s.done
	return s.Err()
}

// Done closes after shutdown, including context cancellation or socket failure.
func (s *Service) Done() <-chan struct{} { return s.done }

// Err reports a runtime transport error; ordinary cancellation is not an error.
func (s *Service) Err() error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.err
}

func listenMulticast(iface *net.Interface, ip net.IP) (net.PacketConn, error) {
	conn, err := net.ListenMulticastUDP("udp4", iface, multicastAddress)
	if err != nil {
		return nil, err
	}
	if err := configureSocket(conn, iface, ip); err != nil {
		_ = conn.Close()
		return nil, err
	}
	return scopedPacketConn(conn, iface), nil
}
