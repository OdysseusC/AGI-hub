package mesh

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestManagementGrantIsSeparateRevocableAndNotInheritedByPairing(t *testing.T) {
	a, b := newTestService(t, t.TempDir(), nil), newTestService(t, t.TempDir(), nil)
	var calls atomic.Int32
	b.opts.Manage = func(_ context.Context, in ManagementRequest) (ManagementResponse, error) {
		calls.Add(1)
		return ManagementResponse{Status: http.StatusOK, Body: json.RawMessage(`{"ok":true}`)}, nil
	}
	enable(t, a)
	enable(t, b)
	pair(t, a, b)
	in := ManagementRequest{Method: http.MethodGet, Path: "/v1/manage"}
	denied := func() {
		t.Helper()
		_, err := a.RemoteManagement(context.Background(), b.id, in)
		var rejected *ManagementError
		if !errors.As(err, &rejected) || rejected.Status != http.StatusForbidden {
			t.Fatalf("expected separate management grant, got %v", err)
		}
	}
	denied()
	if err := b.SetManagementAllowed(a.id, true); err != nil {
		t.Fatal(err)
	}
	if !b.Snapshot().Peers[0].ManageAllowed {
		t.Fatal("grant absent from snapshot")
	}
	out, err := a.RemoteManagement(context.Background(), b.id, in)
	if err != nil || out.Status != http.StatusOK || calls.Load() != 1 {
		t.Fatalf("allowed request failed: %+v %v", out, err)
	}
	if err := b.SetManagementAllowed(a.id, false); err != nil {
		t.Fatal(err)
	}
	denied() // same pinned keep-alive transport must consult the new grant.
	if calls.Load() != 1 {
		t.Fatal("revoked request reached callback")
	}
	if err := b.SetManagementAllowed(a.id, true); err != nil {
		t.Fatal(err)
	}
	if err := b.Disconnect(a.id); err != nil {
		t.Fatal(err)
	}
	if err := a.Disconnect(b.id); err != nil {
		t.Fatal(err)
	}
	if err := b.SetManagementAllowed(a.id, true); err == nil {
		t.Fatal("unpaired identity granted management")
	}
	pair(t, a, b)
	denied()
	if b.Snapshot().Peers[0].ManageAllowed {
		t.Fatal("new pairing inherited old grant")
	}
}

func TestManagementHasIndependentBoundedTLSBudget(t *testing.T) {
	a, b := newTestService(t, t.TempDir(), nil), newTestService(t, t.TempDir(), nil)
	var calls atomic.Int32
	b.opts.Manage = func(ctx context.Context, _ ManagementRequest) (ManagementResponse, error) {
		calls.Add(1)
		timer := time.NewTimer(9 * time.Second)
		defer timer.Stop()
		select {
		case <-timer.C:
			return ManagementResponse{Status: http.StatusOK, Body: json.RawMessage(`{"ok":true}`)}, nil
		case <-ctx.Done():
			return ManagementResponse{}, ctx.Err()
		}
	}
	enable(t, a)
	enable(t, b)
	pair(t, a, b)
	if err := b.SetManagementAllowed(a.id, true); err != nil {
		t.Fatal(err)
	}
	a.mu.Lock()
	peer := a.disk.Peers[b.id]
	a.mu.Unlock()
	ordinary := a.client(peer)
	transport := ordinary.Transport
	if ordinary.Timeout != 7*time.Second || b.server.WriteTimeout != 8*time.Second {
		t.Fatalf("ordinary transport budgets changed: client=%v server=%v", ordinary.Timeout, b.server.WriteTimeout)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	started := time.Now()
	out, err := a.RemoteManagement(ctx, b.id, ManagementRequest{Method: http.MethodGet, Path: "/v1/manage"})
	if err != nil || out.Status != http.StatusOK || string(out.Body) != `{"ok":true}` {
		t.Fatalf("management callback beyond the ordinary budgets failed: %+v %v", out, err)
	}
	if time.Since(started) < 9*time.Second || calls.Load() != 1 {
		t.Fatalf("delayed callback did not run exactly once: elapsed=%v calls=%d", time.Since(started), calls.Load())
	}
	if ordinary.Timeout != 7*time.Second || a.client(peer) != ordinary || a.client(peer).Transport != transport || b.server.WriteTimeout != 8*time.Second {
		t.Fatal("management mutated the ordinary client, connection pool, or global server deadline")
	}
}

func TestManagementGrantPersistsOnlyInLocalState(t *testing.T) {
	dir := t.TempDir()
	a, b := newTestService(t, t.TempDir(), nil), newTestService(t, dir, nil)
	enable(t, a)
	enable(t, b)
	pair(t, a, b)
	if err := b.SetManagementAllowed(a.id, true); err != nil {
		t.Fatal(err)
	}
	b.mu.Lock()
	wire, _ := json.Marshal(b.selfLocked())
	b.mu.Unlock()
	if strings.Contains(string(wire), "management") {
		t.Fatal("grant leaked into pairing identity")
	}
	if err := b.Close(); err != nil {
		t.Fatal(err)
	}
	restored := newTestService(t, dir, nil)
	if !restored.Snapshot().Peers[0].ManageAllowed {
		t.Fatal("explicit local grant was not persisted")
	}
}

func TestManagementNeverRoutesThroughAnotherHub(t *testing.T) {
	a, b, c := newTestService(t, t.TempDir(), nil), newTestService(t, t.TempDir(), nil), newTestService(t, t.TempDir(), nil)
	var calls atomic.Int32
	b.opts.Manage = func(context.Context, ManagementRequest) (ManagementResponse, error) {
		calls.Add(1)
		return ManagementResponse{Status: 200, Body: json.RawMessage(`{}`)}, nil
	}
	enable(t, a)
	enable(t, b)
	enable(t, c)
	pair(t, a, b)
	pair(t, b, c)
	if err := b.SetManagementAllowed(a.id, true); err != nil {
		t.Fatal(err)
	}
	if _, err := a.RemoteManagement(context.Background(), c.id, ManagementRequest{Method: "GET", Path: "/v1/manage"}); err == nil {
		t.Fatal("non-direct management accepted")
	}
	if calls.Load() != 0 {
		t.Fatal("management forwarded to intermediate Hub")
	}
}

func TestManagementRequestHasFixedMethodsPathsAndBounds(t *testing.T) {
	for _, in := range []ManagementRequest{
		{Method: "GET", Path: "/v1/manage"}, {Method: "GET", Path: "/v1/state"},
		{Method: "POST", Path: "/v1/manage/agent", Body: json.RawMessage(`{"endpoint":"codex","action":"disable"}`)},
		{Method: "POST", Path: "/v1/manage/autostart", Body: json.RawMessage(`{}`)},
	} {
		if err := ValidateManagementRequest(in); err != nil {
			t.Fatalf("valid operation rejected: %+v: %v", in, err)
		}
	}
	for _, in := range []ManagementRequest{
		{Method: "PUT", Path: "/v1/manage/agent", Body: json.RawMessage(`{}`)},
		{Method: "POST", Path: "/v1/manage/network", Body: json.RawMessage(`{"action":"allow_management"}`)},
		{Method: "POST", Path: "/v1/manage/settings", Body: json.RawMessage(`{}`)},
		{Method: "GET", Path: "/remote/peer/v1/manage"},
		{Method: "GET", Path: "https://127.0.0.1/v1/manage"},
		{Method: "GET", Path: "/v1/state?token=anything"},
		{Method: "GET", Path: "/v1/state", Body: json.RawMessage(`{}`)},
		{Method: "POST", Path: "/v1/manage/agent", Body: json.RawMessage(`[]`)},
		{Method: "POST", Path: "/v1/manage/agent", Body: json.RawMessage(`{"x":"` + strings.Repeat("x", MaxManagementBody) + `"}`)},
	} {
		if err := ValidateManagementRequest(in); err == nil {
			t.Fatalf("forbidden operation accepted: %+v", in)
		}
	}
}

func TestManagementRPCRejectsUnknownEnvelopeFieldsAndQuery(t *testing.T) {
	s := newTestService(t, t.TempDir(), nil)
	s.mu.Lock()
	s.disk.Config.Enabled = true
	s.disk.Peers["peer"] = peerRecord{}
	s.disk.Management["peer"] = true
	s.mu.Unlock()
	for _, body := range []string{`{"method":"GET","path":"/v1/manage","url":"http://example.invalid"}`, `{"method":"GET","path":"/v1/manage"} {}`} {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("POST", "/v1/manage", strings.NewReader(body))
		s.handleManagement(w, r, "peer")
		if w.Code != http.StatusBadRequest {
			t.Fatalf("malformed RPC status %d", w.Code)
		}
	}
	w := httptest.NewRecorder()
	s.handleManagement(w, httptest.NewRequest("POST", "/v1/manage?peer=another", strings.NewReader(`{}`)), "peer")
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatal("query accepted")
	}
}

type managementReadHook struct {
	io.Reader
	once sync.Once
	hook func()
}

func (r *managementReadHook) Read(body []byte) (int, error) {
	r.once.Do(r.hook)
	return r.Reader.Read(body)
}

func TestManagementRechecksGrantAfterReceivingBody(t *testing.T) {
	s := newTestService(t, t.TempDir(), nil)
	called := false
	s.opts.Manage = func(context.Context, ManagementRequest) (ManagementResponse, error) {
		called = true
		return ManagementResponse{Status: 200, Body: json.RawMessage(`{}`)}, nil
	}
	s.mu.Lock()
	s.disk.Config.Enabled = true
	s.disk.Peers["peer"] = peerRecord{}
	s.disk.Management["peer"] = true
	s.mu.Unlock()
	body := &managementReadHook{Reader: strings.NewReader(`{"method":"GET","path":"/v1/manage"}`), hook: func() {
		if err := s.SetManagementAllowed("peer", false); err != nil {
			t.Fatal(err)
		}
	}}
	r := httptest.NewRequest("POST", "/v1/manage", body)
	w := httptest.NewRecorder()
	s.handleManagement(w, r, "peer")
	if w.Code != http.StatusForbidden || called {
		t.Fatal("revocation during request parsing reached callback")
	}
}
