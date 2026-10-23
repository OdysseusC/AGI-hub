package mesh

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptrace"
	"strings"
	"sync"
	"testing"
	"time"
)

func newTestService(t *testing.T, dir string, receive func(context.Context, Message) error) *Service {
	t.Helper()
	s, err := New(Options{DataDir: dir, LocalAgents: func() []Agent { return []Agent{{ID: "same", Name: "Assistant"}} }, Receive: receive})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func enable(t *testing.T, s *Service) {
	t.Helper()
	if err := s.Configure(Config{Enabled: true, ListenAddr: "127.0.0.1:0", Name: "Test hub", Exports: []string{"same"}}); err != nil {
		t.Fatal(err)
	}
}

func eventually(t *testing.T, f func() bool) {
	t.Helper()
	end := time.Now().Add(8 * time.Second)
	for time.Now().Before(end) {
		if f() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("condition did not become true")
}

func pair(t *testing.T, a, b *Service) {
	t.Helper()
	invite, err := a.Invite(time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	request, err := b.RequestPair(context.Background(), invite.Code)
	if err != nil {
		t.Fatal(err)
	}
	if len(a.Snapshot().Peers) != 0 && len(a.Snapshot().Pending) == 0 {
		t.Fatal("request was not pending")
	}
	if err = a.AcceptPair(context.Background(), request.ID); err != nil {
		t.Fatal(err)
	}
	eventually(t, func() bool {
		for _, p := range b.Snapshot().Peers {
			if p.HubID == a.Snapshot().HubID && p.Online {
				return true
			}
		}
		return false
	})
}

func message(a, b *Service, id string) Message {
	return Message{ID: id, FromHub: a.Snapshot().HubID, ToHub: b.Snapshot().HubID, FromEndpoint: "same", ToEndpoint: "same", ExpiresAt: time.Now().Add(time.Hour).UTC(), Payload: json.RawMessage(`{"body":"hello"}`)}
}

func TestIdentityStableAndDefaultOff(t *testing.T) {
	dir := t.TempDir()
	s := newTestService(t, dir, nil)
	first := s.Snapshot()
	if first.Config.Enabled || first.Address != "" {
		t.Fatal("LAN started without opt-in")
	}
	if len(first.HubID) != 64 {
		t.Fatal("missing stable key-derived identity")
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	next := newTestService(t, dir, nil)
	if next.Snapshot().HubID != first.HubID {
		t.Fatal("restart changed identity")
	}
	state, _ := json.Marshal(next.Snapshot())
	if strings.Contains(string(state), "private") || strings.Contains(string(state), "secret") {
		t.Fatal("snapshot exposes secret material")
	}
}

func TestPairConfirmationAndDurableDuplicateDelivery(t *testing.T) {
	var mu sync.Mutex
	var got []Message
	a := newTestService(t, t.TempDir(), nil)
	b := newTestService(t, t.TempDir(), func(_ context.Context, m Message) error {
		mu.Lock()
		defer mu.Unlock()
		got = append(got, m)
		return nil
	})
	enable(t, a)
	enable(t, b)
	inv, err := a.Invite(time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	req, err := b.RequestPair(context.Background(), inv.Code)
	if err != nil {
		t.Fatal(err)
	}
	if len(a.Snapshot().Peers) != 0 || len(b.Snapshot().Peers) != 0 {
		t.Fatal("pairing did not wait for inviter confirmation")
	}
	if _, err = b.Send(context.Background(), message(b, a, "unconfirmed")); err == nil {
		t.Fatal("unconfirmed peer authorized")
	}
	if err = a.AcceptPair(context.Background(), req.ID); err != nil {
		t.Fatal(err)
	}
	eventually(t, func() bool { return len(b.Snapshot().Peers) == 1 })
	m := message(a, b, "durable")
	if _, err = a.Send(context.Background(), m); err != nil {
		t.Fatal(err)
	}
	eventually(t, func() bool { mu.Lock(); defer mu.Unlock(); return len(got) == 1 })
	if _, err = a.Send(context.Background(), m); err != nil {
		t.Fatal(err)
	}
	m.Payload = json.RawMessage(`{"body":"changed"}`)
	if _, err = a.Send(context.Background(), m); err == nil {
		t.Fatal("ID/content conflict accepted")
	}
	time.Sleep(200 * time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	if len(got) != 1 {
		t.Fatalf("duplicate callback: %d", len(got))
	}
	if got[0].FromHub != a.Snapshot().HubID || got[0].ToHub != b.Snapshot().HubID {
		t.Fatal("qualified addresses lost")
	}
}

func TestFailedDurableCallbackDoesNotAcknowledge(t *testing.T) {
	a := newTestService(t, t.TempDir(), nil)
	b := newTestService(t, t.TempDir(), func(context.Context, Message) error { return errors.New("disk unavailable") })
	enable(t, a)
	enable(t, b)
	pair(t, a, b)
	receipt, err := a.Send(context.Background(), message(a, b, "disk-failure"))
	if err != nil {
		t.Fatal(err)
	}
	if receipt.Status == "received" {
		t.Fatal("ACK preceded durable callback")
	}
}

func TestRelayRequiresDestinationTrustAndPrefersDirect(t *testing.T) {
	var mu sync.Mutex
	received := 0
	a := newTestService(t, t.TempDir(), nil)
	b := newTestService(t, t.TempDir(), nil)
	c := newTestService(t, t.TempDir(), func(context.Context, Message) error { mu.Lock(); received++; mu.Unlock(); return nil })
	enable(t, a)
	enable(t, b)
	enable(t, c)
	pair(t, a, b)
	pair(t, b, c)
	// Discovery/directory visibility through B must never authorize A -> C.
	if _, err := a.Send(context.Background(), message(a, c, "untrusted")); err == nil {
		t.Fatal("transitive trust accepted")
	}
	pair(t, a, c)
	eventually(t, func() bool {
		for _, r := range a.Snapshot().Routes {
			if r.HubID == c.id {
				return len(r.Path) == 2
			}
		}
		return false
	})
	// Simulate loss of the A-C direct path while preserving both pinned trusts.
	a.mu.Lock()
	ap := a.disk.Peers[c.id]
	ap.Address = "127.0.0.1:1"
	a.disk.Peers[c.id] = ap
	delete(a.online, c.id)
	a.mu.Unlock()
	c.mu.Lock()
	cp := c.disk.Peers[a.id]
	cp.Address = "127.0.0.1:1"
	c.disk.Peers[a.id] = cp
	delete(c.online, a.id)
	c.mu.Unlock()
	eventually(t, func() bool {
		for _, r := range a.Snapshot().Routes {
			if r.HubID == c.id {
				return len(r.Path) == 3 && r.Path[1] == b.id
			}
		}
		return false
	})
	if _, err := a.Send(context.Background(), message(a, c, "relay")); err != nil {
		t.Fatal(err)
	}
	eventually(t, func() bool { mu.Lock(); defer mu.Unlock(); return received == 1 })
}

func TestOutboxAndInboxSurviveRestart(t *testing.T) {
	dirA, dirB := t.TempDir(), t.TempDir()
	a := newTestService(t, dirA, nil)
	var mu sync.Mutex
	count := 0
	receive := func(context.Context, Message) error { mu.Lock(); count++; mu.Unlock(); return nil }
	b := newTestService(t, dirB, receive)
	enable(t, a)
	enable(t, b)
	pair(t, a, b)
	m := message(a, b, "restart")
	// Keep the listener address stable across a restart.
	b.mu.Lock()
	b.disk.Config.ListenAddr = b.address
	if err := b.persistLocked(); err != nil {
		t.Fatal(err)
	}
	b.mu.Unlock()
	if err := b.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Send(context.Background(), m); err != nil {
		t.Fatal(err)
	}
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	a2 := newTestService(t, dirA, nil)
	b2 := newTestService(t, dirB, receive)
	eventually(t, func() bool { mu.Lock(); defer mu.Unlock(); return count == 1 })
	eventually(t, func() bool {
		for _, r := range a2.Snapshot().Outbox {
			if r.MessageID == m.ID {
				return r.Status == "received"
			}
		}
		return false
	})
	if err := b2.Close(); err != nil {
		t.Fatal(err)
	}
	b3 := newTestService(t, dirB, receive)
	// Re-deliver the exact signed envelope, simulating a lost original ACK.
	env := a2.signMessage(m)
	if _, err := b3.deliver(context.Background(), a2.id, relayFrame{Envelope: env, Path: []string{a2.id}}); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if count != 1 {
		t.Fatalf("duplicate after restart: %d", count)
	}
}

func TestEnvelopeIntegrityExportRevocationAndLoopLimit(t *testing.T) {
	a := newTestService(t, t.TempDir(), nil)
	b := newTestService(t, t.TempDir(), func(context.Context, Message) error { return nil })
	enable(t, a)
	enable(t, b)
	pair(t, a, b)
	m := message(a, b, "integrity")
	env := a.signMessage(m)
	altered := env
	altered.Payload = json.RawMessage(`{"changed":true}`)
	if _, err := b.deliver(context.Background(), a.id, relayFrame{Envelope: altered, Path: []string{a.id}}); err == nil {
		t.Fatal("tampering accepted")
	}
	if _, err := b.deliver(context.Background(), a.id, relayFrame{Envelope: env, Path: []string{a.id, b.id, a.id}}); err == nil {
		t.Fatal("routing loop accepted")
	}
	cfg := b.Snapshot().Config
	cfg.Exports = []string{}
	if err := b.Configure(cfg); err != nil {
		t.Fatal(err)
	}
	if _, err := b.deliver(context.Background(), a.id, relayFrame{Envelope: env, Path: []string{a.id}}); err == nil {
		t.Fatal("revoked export accepted")
	}
}

func TestExpiredInvitationAndUnknownTLSIdentity(t *testing.T) {
	a := newTestService(t, t.TempDir(), nil)
	b := newTestService(t, t.TempDir(), nil)
	enable(t, a)
	enable(t, b)
	inv, err := a.Invite(time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(5 * time.Millisecond)
	if _, err = b.RequestPair(context.Background(), inv.Code); err == nil {
		t.Fatal("expired invitation accepted")
	}
	a.mu.Lock()
	peer := a.selfLocked()
	a.mu.Unlock()
	var directory directoryFrame
	if err = b.request(context.Background(), peer, "/v1/directory", nil, &directory); err == nil {
		t.Fatal("unknown TLS identity accepted")
	}
}

func TestConnectionsAreReusedAndIdentityPinned(t *testing.T) {
	a := newTestService(t, t.TempDir(), nil)
	b := newTestService(t, t.TempDir(), nil)
	c := newTestService(t, t.TempDir(), nil)
	enable(t, a)
	enable(t, b)
	enable(t, c)
	pair(t, a, b)
	a.mu.Lock()
	peer := a.selfLocked()
	a.mu.Unlock()
	reused := false
	ctx := httptrace.WithClientTrace(context.Background(), &httptrace.ClientTrace{GotConn: func(info httptrace.GotConnInfo) { reused = info.Reused }})
	if err := b.request(ctx, peer, "/v1/directory", nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := b.request(ctx, peer, "/v1/directory", nil, nil); err != nil {
		t.Fatal(err)
	}
	if !reused {
		t.Fatal("paired transport did not reuse connection")
	}
	c.mu.Lock()
	wrong := c.selfLocked()
	c.mu.Unlock()
	wrong.Address = peer.Address
	if err := b.request(context.Background(), wrong, "/v1/directory", nil, nil); err == nil {
		t.Fatal("server with wrong pinned identity accepted")
	}
}

func TestCompletedHistoryDoesNotConsumeActiveQueueCapacity(t *testing.T) {
	a := newTestService(t, t.TempDir(), nil)
	b := newTestService(t, t.TempDir(), func(context.Context, Message) error { return nil })
	enable(t, a)
	enable(t, b)
	pair(t, a, b)
	a.mu.Lock()
	for n := 0; n < maxQueue; n++ {
		id := fmt.Sprintf("old-%d", n)
		a.disk.Outbox[id] = outboxRecord{Receipt: Receipt{MessageID: id, Status: "received"}}
	}
	a.mu.Unlock()
	b.mu.Lock()
	for n := 0; n < maxQueue; n++ {
		b.disk.Inbox[fmt.Sprintf("old/%d", n)] = inboxRecord{Received: true, Hash: "old"}
	}
	b.mu.Unlock()
	if _, err := a.Send(context.Background(), message(a, b, "after-history")); err != nil {
		t.Fatal(err)
	}
	eventually(t, func() bool {
		a.mu.Lock()
		defer a.mu.Unlock()
		return a.disk.Outbox["after-history"].Receipt.Status == "received"
	})
}

func TestOldVersionAndForgedDirectoryRoutesAreIgnored(t *testing.T) {
	a := newTestService(t, t.TempDir(), nil)
	b := newTestService(t, t.TempDir(), nil)
	enable(t, a)
	enable(t, b)
	pair(t, a, b)
	frame := mustDirectory(t, b)
	frame.Entries[0].Name = "forged"
	a.mu.Lock()
	delete(a.ads, b.id)
	a.mu.Unlock()
	a.acceptDirectory(frame)
	a.mu.Lock()
	_, exists := a.ads[b.id]
	a.mu.Unlock()
	if exists {
		t.Fatal("forged directory accepted")
	}
	frame = mustDirectory(t, b)
	frame.Version = Version - 1
	a.acceptDirectory(frame)
	a.mu.Lock()
	_, exists = a.ads[b.id]
	a.mu.Unlock()
	if exists {
		t.Fatal("old mesh directory version accepted")
	}
}

func TestConfigureRejectsNonLANAddressesAndFailsClosed(t *testing.T) {
	s := newTestService(t, t.TempDir(), nil)
	for _, address := range []string{"example.com:443", "8.8.8.8:443", "0.0.0.0:7347", "127.0.0.1:0", "127.0.0.1:http"} {
		if err := s.Configure(Config{Enabled: true, ListenAddr: "127.0.0.1:0", AdvertiseAddr: address}); err == nil {
			t.Fatalf("unsafe advertised address accepted: %s", address)
		}
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	if err = s.Configure(Config{Enabled: true, ListenAddr: ln.Addr().String()}); err == nil {
		t.Fatal("occupied listener did not fail")
	}
	if state := s.Snapshot(); state.Config.Enabled || state.Address != "" {
		t.Fatal("failed configuration left LAN half-open")
	}
	enable(t, s)
	if !s.Snapshot().Config.Enabled || s.Snapshot().Address == "" {
		t.Fatal("could not recover after failed listen")
	}
}

func TestSharedAddressSpaceBoundariesAndDestinationRestrictions(t *testing.T) {
	for _, tc := range []struct {
		address               string
		destination, listener bool
	}{
		{"100.63.255.255:7347", false, false},
		{"100.64.0.0:7347", true, true},
		{"100.70.1.2:7347", true, true},
		{"100.127.255.255:7347", true, true},
		{"100.128.0.0:7347", false, false},
		{"[::ffff:100.70.1.2]:7347", true, true},
		{"[fd00:1::2]:7347", true, true},
		{"8.8.8.8:7347", false, false},
		{"[2001:4860:4860::8888]:7347", false, false},
		{"0.0.0.0:7347", false, true},
		{"[::]:7347", false, true},
		{"100.70.1.2:0", false, true},
		{"example.ts.net:7347", false, false},
	} {
		t.Run(tc.address, func(t *testing.T) {
			if got := validAddress(tc.address, false); got != tc.destination {
				t.Fatalf("destination accepted=%v, want %v", got, tc.destination)
			}
			if got := validAddress(tc.address, true); got != tc.listener {
				t.Fatalf("listener accepted=%v, want %v", got, tc.listener)
			}
		})
	}
}

func TestSharedAddressInvitationPairsThroughExistingManualRoute(t *testing.T) {
	const advertised = "100.70.1.2:7347"
	// Validate the existing overlay's concrete bind address without creating a
	// real overlay interface or changing the machine's network configuration.
	if err := validateConfig(Config{ListenAddr: advertised, AdvertiseAddr: advertised}); err != nil {
		t.Fatal(err)
	}
	a := newTestService(t, t.TempDir(), nil)
	b := newTestService(t, t.TempDir(), nil)
	if err := a.Configure(Config{Enabled: true, ListenAddr: "127.0.0.1:0", AdvertiseAddr: advertised, Exports: []string{"same"}}); err != nil {
		t.Fatal(err)
	}
	enable(t, b)
	inv, err := a.Invite(time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := base64.RawURLEncoding.DecodeString(inv.Code)
	var decoded inviteCode
	if err := json.Unmarshal(raw, &decoded); err != nil || decoded.Peer.Address != advertised {
		t.Fatalf("shared advertised address missing from invitation: %v", err)
	}
	// The TLS exchange stays entirely on loopback. An explicit route override
	// changes only the destination address, preserving the invitation's identity.
	actual := a.listener.Addr().String()
	req, err := b.RequestPairAt(context.Background(), actual, inv.Code)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.AcceptPair(context.Background(), req.ID); err != nil {
		t.Fatal(err)
	}
	eventually(t, func() bool { return len(b.Snapshot().Peers) == 1 && b.Snapshot().Peers[0].Online })
	peer := b.Snapshot().Peers[0]
	if peer.HubID != a.id || peer.Address != actual {
		t.Fatal("manual route replaced the invited identity")
	}
	b.acceptDirectory(mustDirectory(t, a))
	b.mu.Lock()
	ad, ok := b.ads[a.id]
	b.mu.Unlock()
	if !ok || ad.Address != advertised {
		t.Fatal("signed shared-address directory was rejected")
	}
}

func TestManualAddressOverrideRetainsInviteIdentityPin(t *testing.T) {
	a := newTestService(t, t.TempDir(), nil)
	b := newTestService(t, t.TempDir(), nil)
	enable(t, a)
	enable(t, b)
	inv, err := a.Invite(time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := base64.RawURLEncoding.DecodeString(inv.Code)
	var invite inviteCode
	if err = json.Unmarshal(raw, &invite); err != nil {
		t.Fatal(err)
	}
	actual := invite.Peer.Address
	invite.Peer.Address = "127.0.0.1:1"
	raw, _ = json.Marshal(invite)
	req, err := b.RequestPairAt(context.Background(), actual, base64.RawURLEncoding.EncodeToString(raw))
	if err != nil {
		t.Fatal(err)
	}
	if err = a.AcceptPair(context.Background(), req.ID); err != nil {
		t.Fatal(err)
	}
	eventually(t, func() bool { return len(b.Snapshot().Peers) == 1 })
	if b.Snapshot().Peers[0].HubID != a.id || b.Snapshot().Peers[0].Address != actual {
		t.Fatal("manual override changed invited identity")
	}
}

type offlineRoundTripper func(*http.Request) (*http.Response, error)

func (f offlineRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func TestRequestPreservesIPv6ScopeAndPairingQueryWithoutNetwork(t *testing.T) {
	for _, address := range []string{"[fe80::1%en0]:7347", "[fe80::1%12]:7347", "[fd00:1::2]:7347", "100.70.1.2:7347"} {
		t.Run(address, func(t *testing.T) {
			if !validAddress(address, false) {
				t.Fatal("test address is outside the declared supported address set")
			}
			peer := peerRecord{HubID: strings.Repeat("a", 64), Address: address}
			called := false
			client := &http.Client{Transport: offlineRoundTripper(func(r *http.Request) (*http.Response, error) {
				called = true
				if r.URL.Scheme != "https" || r.URL.Host != address || r.URL.Path != "/v1/pair/status" || r.URL.Query().Get("id") != "proof+slash/=" || r.Method != http.MethodGet {
					t.Fatalf("address scope or pairing request changed: %s", r.URL)
				}
				return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"ok":true}`))}, nil
			})}
			// Exercise the real request construction while replacing the transport
			// entirely. No interface, listener, TLS handshake or peer is contacted.
			s := &Service{clients: map[string]*http.Client{peer.HubID + "|" + address: client}}
			var result struct {
				OK bool `json:"ok"`
			}
			if err := s.request(context.Background(), peer, "/v1/pair/status?id=proof%2Bslash%2F%3D", nil, &result); err != nil {
				t.Fatal(err)
			}
			if !called || !result.OK {
				t.Fatal("request did not reach the offline transport and decode its reply")
			}
		})
	}
}

func TestDiscoveredAddressChangeRequiresPinnedIdentity(t *testing.T) {
	a := newTestService(t, t.TempDir(), nil)
	b := newTestService(t, t.TempDir(), nil)
	enable(t, a)
	enable(t, b)
	pair(t, a, b)
	b.mu.Lock()
	old := b.disk.Peers[a.id]
	old.Address = "127.0.0.1:1"
	b.disk.Peers[a.id] = old
	delete(b.online, a.id)
	b.mu.Unlock()
	candidate := a.Announcement()
	b.SetCandidates([]Candidate{candidate})
	eventually(t, func() bool {
		for _, p := range b.Snapshot().Peers {
			if p.HubID == a.id {
				return p.Address == candidate.Address && p.Online
			}
		}
		return false
	})
}

func TestConnectionLimitReleasesOnClose(t *testing.T) {
	raw, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ln := newLimitedListener(raw, 1)
	defer ln.Close()
	first, err := net.Dial("tcp", raw.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	accepted, err := ln.Accept()
	if err != nil {
		t.Fatal(err)
	}
	second, err := net.Dial("tcp", raw.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	next := make(chan net.Conn, 1)
	go func() { c, _ := ln.Accept(); next <- c }()
	select {
	case c := <-next:
		if c != nil {
			c.Close()
		}
		t.Fatal("connection limit not enforced")
	case <-time.After(20 * time.Millisecond):
	}
	_ = accepted.Close()
	select {
	case c := <-next:
		if c == nil {
			t.Fatal("listener closed unexpectedly")
		}
		_ = c.Close()
	case <-time.After(time.Second):
		t.Fatal("connection slot was not released")
	}
}

func TestQueuedSendStopsWhenLocalExportIsRevoked(t *testing.T) {
	a := newTestService(t, t.TempDir(), nil)
	b := newTestService(t, t.TempDir(), nil)
	enable(t, a)
	enable(t, b)
	pair(t, a, b)
	if err := b.Close(); err != nil {
		t.Fatal(err)
	}
	m := message(a, b, "revoke-outgoing")
	if _, err := a.Send(context.Background(), m); err != nil {
		t.Fatal(err)
	}
	cfg := a.Snapshot().Config
	cfg.Exports = []string{}
	if err := a.Configure(cfg); err != nil {
		t.Fatal(err)
	}
	a.sendOne(context.Background(), m.ID)
	a.mu.Lock()
	status := a.disk.Outbox[m.ID].Receipt.Status
	a.mu.Unlock()
	if status != "failed" {
		t.Fatalf("revoked sender queue status=%s", status)
	}
	m.ID = "new-unshared"
	if _, err := a.Send(context.Background(), m); err == nil {
		t.Fatal("unshared local sender accepted")
	}
}

func TestReconnectBacksOffFailedPeer(t *testing.T) {
	a := newTestService(t, t.TempDir(), nil)
	b := newTestService(t, t.TempDir(), nil)
	enable(t, a)
	enable(t, b)
	pair(t, a, b)
	a.mu.Lock()
	p := a.disk.Peers[b.id]
	p.Address = "127.0.0.1:1"
	a.disk.Peers[b.id] = p
	a.mu.Unlock()
	a.refresh(context.Background(), p)
	a.mu.Lock()
	first := a.retry[b.id]
	a.mu.Unlock()
	if first.Failures < 1 || time.Until(first.Next) < 700*time.Millisecond {
		t.Fatal("failed connection has no retry backoff")
	}
	a.refresh(context.Background(), p)
	a.mu.Lock()
	second := a.retry[b.id]
	a.mu.Unlock()
	if second.Failures <= first.Failures || !second.Next.After(first.Next) {
		t.Fatal("repeated failure did not extend backoff")
	}
}

func TestDirectoryFrameAndAgentTextAreBounded(t *testing.T) {
	s := newTestService(t, t.TempDir(), nil)
	enable(t, s)
	s.opts.LocalAgents = func() []Agent {
		agents := []Agent{}
		for n := 0; n < 128; n++ {
			agents = append(agents, Agent{ID: "same", Name: strings.Repeat("x", 8192), Status: strings.Repeat("x", 8192)})
		}
		return agents
	}
	frame := mustDirectory(t, s)
	raw, _ := json.Marshal(frame)
	if len(raw) > maxFrame {
		t.Fatalf("directory frame too large: %d", len(raw))
	}
	for _, agent := range frame.Entries[0].Agents {
		if len(agent.Name) > 120 || len(agent.Status) > 100 {
			t.Fatal("directory agent text unbounded")
		}
	}
}

func TestExpiredRejectedInboxPreservesDedupWithoutBlockingNewMail(t *testing.T) {
	a := newTestService(t, t.TempDir(), nil)
	dir := t.TempDir()
	b := newTestService(t, dir, func(context.Context, Message) error { return nil })
	enable(t, a)
	enable(t, b)
	pair(t, a, b)
	expiredMessage := message(a, b, "expired-rejected")
	expiredMessage.ExpiresAt = time.Now().Add(-time.Minute)
	hash := messageHash(expiredMessage)
	b.mu.Lock()
	for n := 0; n < maxQueue; n++ {
		m := expiredMessage
		m.ID = fmt.Sprintf("expired-rejected-%d", n)
		if n == 0 {
			m.ID = expiredMessage.ID
		}
		b.disk.Inbox[a.id+"/"+m.ID] = inboxRecord{Envelope: a.signMessage(m), Hash: messageHash(m)}
	}
	if err := b.persistLocked(); err != nil {
		t.Fatal(err)
	}
	b.mu.Unlock()
	// The receive path must free expired active slots without requiring a timer.
	incoming := message(a, b, "fresh-after-expiry")
	if _, err := b.deliver(context.Background(), a.id, relayFrame{Envelope: a.signMessage(incoming), Path: []string{a.id}}); err != nil {
		t.Fatal(err)
	}
	if err := b.Close(); err != nil {
		t.Fatal(err)
	}
	restarted := newTestService(t, dir, func(context.Context, Message) error { return nil })
	restarted.mu.Lock()
	record := restarted.disk.Inbox[a.id+"/"+expiredMessage.ID]
	restarted.mu.Unlock()
	if record.Received {
		t.Fatal("expired rejected mail became a successful receipt")
	}
	if record.Hash != hash || len(record.Envelope.Payload) != 0 {
		t.Fatal("expiry tombstone lost hash or retained payload")
	}
	conflict := expiredMessage
	conflict.ExpiresAt = time.Now().Add(time.Hour)
	conflict.Payload = json.RawMessage(`{"replacement":true}`)
	if _, err := restarted.deliver(context.Background(), a.id, relayFrame{Envelope: a.signMessage(conflict), Path: []string{a.id}}); err == nil {
		t.Fatal("expired ID allowed conflicting reuse")
	}
	if _, err := restarted.deliver(context.Background(), a.id, relayFrame{Envelope: a.signMessage(expiredMessage), Path: []string{a.id}}); err == nil {
		t.Fatal("expired rejection produced a success ACK")
	}
}

func TestDisconnectCannotBeUndoneByPendingPairConfirmation(t *testing.T) {
	a := newTestService(t, t.TempDir(), nil)
	b := newTestService(t, t.TempDir(), nil)
	enable(t, a)
	enable(t, b)
	pair(t, a, b)
	inv, err := a.Invite(time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = b.RequestPairAt(context.Background(), a.Snapshot().Address, inv.Code); err != nil {
		t.Fatal(err)
	}
	if err = b.Disconnect(a.id); err != nil {
		t.Fatal(err)
	}
	for _, p := range b.Snapshot().Pending {
		if p.HubID == a.id {
			t.Fatal("disconnect left pending reauthorization")
		}
	}
	b.maintain()
	if len(b.Snapshot().Peers) != 0 {
		t.Fatal("stale confirmation restored a disconnected peer")
	}
	if err = b.Close(); err != nil {
		t.Fatal(err)
	}
	restarted := newTestService(t, b.opts.DataDir, nil)
	if len(restarted.Snapshot().Peers) != 0 || len(restarted.Snapshot().Pending) != 0 {
		t.Fatal("disconnect was not durably complete")
	}
}
