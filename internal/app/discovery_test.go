package app

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"agihub/internal/discovery"
	"agihub/internal/mesh"
)

func TestDiscoveryControllerOptInAndCandidateMapping(t *testing.T) {
	backend := newDiscoveryBackend()
	control := newDiscoveryController(backend, nil)
	handle := newDiscoveryHandle()
	starts := 0
	var options discovery.Options
	control.start = func(ctx context.Context, o discovery.Options) (discoveryHandle, error) {
		starts++
		options = o
		return handle, nil
	}
	if enabled, _, _ := control.State(); enabled || starts != 0 {
		t.Fatal("constructor activated discovery")
	}
	if err := control.Configure(context.Background(), true, "ethernet-test"); err != nil {
		t.Fatal(err)
	}
	if starts != 1 || options.Interface != "ethernet-test" || options.Announcement.Address != "192.168.1.20:7347" {
		t.Fatalf("wrong start options: %+v", options)
	}
	expiry := time.Now().Add(time.Minute)
	options.OnChange([]discovery.Candidate{{Advertisement: discovery.Advertisement{HubID: "peer", Name: "Peer", Address: "192.168.1.21:7347", Protocol: "1"}, ExpiresAt: expiry}})
	if got := backend.candidates(); len(got) != 1 || got[0].Version != mesh.Version || got[0].Address != "192.168.1.21:7347" {
		t.Fatalf("candidate mapping failed: %+v", got)
	}
	if err := control.Configure(context.Background(), false, "ethernet-test"); err != nil {
		t.Fatal(err)
	}
	if enabled, _, _ := control.State(); enabled || !handle.isClosed() || len(backend.candidates()) != 0 {
		t.Fatal("disable did not stop discovery and clear candidates")
	}
	// A queued callback from the retired service cannot restore stale peers.
	options.OnChange([]discovery.Candidate{{Advertisement: discovery.Advertisement{HubID: "stale", Protocol: "1"}, ExpiresAt: expiry}})
	if len(backend.candidates()) != 0 {
		t.Fatal("retired service restored candidates")
	}
}

func TestDiscoveryReconfigurationFailsClosed(t *testing.T) {
	backend := newDiscoveryBackend()
	control := newDiscoveryController(backend, nil)
	handle := newDiscoveryHandle()
	control.start = func(context.Context, discovery.Options) (discoveryHandle, error) { return handle, nil }
	if err := control.Configure(context.Background(), true, "old"); err != nil {
		t.Fatal(err)
	}
	control.start = func(context.Context, discovery.Options) (discoveryHandle, error) {
		if !handle.isClosed() {
			t.Fatal("old interface still active")
		}
		return nil, errors.New("selected interface unavailable")
	}
	if err := control.Configure(context.Background(), true, "new"); err == nil {
		t.Fatal("start failure hidden")
	}
	if enabled, iface, message := control.State(); enabled || iface != "new" || message == "" {
		t.Fatalf("bad failed state: %v %q %q", enabled, iface, message)
	}
}

func TestDiscoveryUsesSelectedAddressForWildcard(t *testing.T) {
	backend := newDiscoveryBackend()
	backend.announcement.Address = "0.0.0.0:7347"
	control := newDiscoveryController(backend, nil)
	control.interfaces = func() ([]discovery.NetworkInterface, error) {
		return []discovery.NetworkInterface{
			{Name: "other", Addresses: []string{"192.168.2.1"}},
			{Name: "selected", Addresses: []string{"fd00::1", "192.168.1.1"}},
		}, nil
	}
	control.start = func(_ context.Context, o discovery.Options) (discoveryHandle, error) {
		if o.Announcement.Address != "192.168.1.1:7347" {
			t.Fatalf("wrong selected address: %s", o.Announcement.Address)
		}
		return newDiscoveryHandle(), nil
	}
	if err := control.Configure(context.Background(), true, "selected"); err != nil {
		t.Fatal(err)
	}
	defer control.Close()
}

func TestDiscoveryRequiresEnabledMesh(t *testing.T) {
	backend := newDiscoveryBackend()
	backend.enabled = false
	control := newDiscoveryController(backend, nil)
	control.start = func(context.Context, discovery.Options) (discoveryHandle, error) {
		t.Fatal("disabled mesh started discovery")
		return nil, nil
	}
	if err := control.Configure(context.Background(), true, "selected"); err == nil {
		t.Fatal("discovery enabled without LAN transport")
	}
	if enabled, _, _ := control.State(); enabled {
		t.Fatal("enabled flag true after rejection")
	}
}

func TestDiscoveryRuntimeFailureClearsState(t *testing.T) {
	backend := newDiscoveryBackend()
	changed := make(chan struct{}, 8)
	control := newDiscoveryController(backend, func() { changed <- struct{}{} })
	handle := newDiscoveryHandle()
	control.start = func(context.Context, discovery.Options) (discoveryHandle, error) { return handle, nil }
	if err := control.Configure(context.Background(), true, "selected"); err != nil {
		t.Fatal(err)
	}
	handle.mu.Lock()
	handle.err = errors.New("socket stopped")
	handle.mu.Unlock()
	_ = handle.Close()
	deadline := time.NewTimer(time.Second)
	defer deadline.Stop()
	for {
		enabled, _, message := control.State()
		if !enabled && strings.Contains(message, "socket stopped") {
			return
		}
		select {
		case <-changed:
		case <-deadline.C:
			t.Fatal("runtime socket failure not reflected")
		}
	}
}

type discoveryBackend struct {
	mu           sync.Mutex
	announcement mesh.Candidate
	enabled      bool
	found        []mesh.Candidate
}

func newDiscoveryBackend() *discoveryBackend {
	return &discoveryBackend{enabled: true, announcement: mesh.Candidate{HubID: "self", Name: "Test Hub", Address: "192.168.1.20:7347", Version: mesh.Version}}
}
func (b *discoveryBackend) Announcement() mesh.Candidate { return b.announcement }
func (b *discoveryBackend) Snapshot() mesh.State {
	return mesh.State{Config: mesh.Config{Enabled: b.enabled}}
}
func (b *discoveryBackend) SetCandidates(c []mesh.Candidate) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.found = append([]mesh.Candidate(nil), c...)
}
func (b *discoveryBackend) candidates() []mesh.Candidate {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]mesh.Candidate(nil), b.found...)
}

type fakeDiscoveryHandle struct {
	done chan struct{}
	once sync.Once
	mu   sync.Mutex
	err  error
}

func newDiscoveryHandle() *fakeDiscoveryHandle {
	return &fakeDiscoveryHandle{done: make(chan struct{})}
}
func (h *fakeDiscoveryHandle) Close() error {
	h.once.Do(func() { close(h.done) })
	return h.Err()
}
func (h *fakeDiscoveryHandle) Done() <-chan struct{} { return h.done }
func (h *fakeDiscoveryHandle) Err() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.err
}
func (h *fakeDiscoveryHandle) isClosed() bool {
	select {
	case <-h.done:
		return true
	default:
		return false
	}
}
