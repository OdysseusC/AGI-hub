package app

import (
	"context"
	"encoding/json"
	"net"
	"path/filepath"
	"testing"
	"time"

	"agihub/internal/manager"
	"agihub/internal/mesh"
)

func networkFixture(t *testing.T, name string) *Server {
	t.Helper()
	dir := t.TempDir()
	if err := InitConfig(dir); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	for i := range cfg.Bindings {
		if cfg.Bindings[i].Endpoint.ID == "codex" {
			e := &cfg.Bindings[i].Endpoint
			e.Enabled = true
			e.SessionID = "native-" + name
			e.MailboxProtocolVersion = 1
			e.IdleWakeVerified = true
		}
	}
	if err := writePrivateJSON(filepath.Join(dir, "config.json"), cfg); err != nil {
		t.Fatal(err)
	}
	b, err := manager.Open(dir, cfg.DeviceID, cfg.Workspaces)
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{Broker: b, Config: cfg}
	for _, binding := range cfg.Bindings {
		if err := b.Register(binding.Endpoint); err != nil {
			t.Fatal(err)
		}
	}
	if err := b.SetSession("codex", "idle", ""); err != nil {
		t.Fatal(err)
	}
	if err := s.StartManagement(context.Background(), dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.CloseManagement(); _ = b.Close() })
	if err := s.management.mesh.Configure(mesh.Config{Enabled: true, ListenAddr: "127.0.0.1:0", Name: name, Exports: []string{"codex"}}); err != nil {
		t.Fatal(err)
	}
	return s
}

func awaitNetwork(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(12 * time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("network condition timed out")
}

func TestTwoHubsSameNamedAgentsExchangeMailboxWithoutNativeCalls(t *testing.T) {
	a, b := networkFixture(t, "A"), networkFixture(t, "B")
	ha, hb := a.management.mesh.Snapshot().HubID, b.management.mesh.Snapshot().HubID
	if ha == hb {
		t.Fatal("independent installations shared identity")
	}
	// Discovery is not involved in manual pairing.
	invite, err := a.management.mesh.Invite(time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	req, err := b.management.mesh.RequestPair(context.Background(), invite.Code)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.management.mesh.AcceptPair(context.Background(), req.ID); err != nil {
		t.Fatal(err)
	}
	awaitNetwork(t, func() bool { return len(a.remoteCatalog("codex")) == 1 && len(b.remoteCatalog("codex")) == 1 })
	body := manager.MailSubmission{ID: "same-id", Recipient: hb + "/codex", Kind: "request", Content: "synthetic test only", OriginRef: "offline-test"}
	send := func(s *Server, request manager.MailSubmission) manager.Envelope {
		w := managementCall(t, s, "/v1/mail/send?endpoint=codex", marshalBody(t, request), EndpointToken(s.Config.AdminToken, "codex"))
		if w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
		var e manager.Envelope
		if err := json.Unmarshal(w.Body.Bytes(), &e); err != nil {
			t.Fatal(err)
		}
		return e
	}
	first := send(a, body)
	if first.SourceMessageID != body.ID || first.Network != nil {
		t.Fatal("client response must map canonical ID without duplicating network payload")
	}
	if duplicate := send(a, body); duplicate.ID != first.ID {
		t.Fatal("retry identity changed")
	}
	awaitNetwork(t, func() bool {
		return len(b.Broker.Snapshot().Messages) == 1 && a.Broker.Snapshot().Messages[first.ID].Delivery == "remote_received"
	})
	claim := func(s *Server) *manager.Envelope {
		w := managementCall(t, s, "/v1/mail/inbox?endpoint=codex", "{}", EndpointToken(s.Config.AdminToken, "codex"))
		if w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
		var e *manager.Envelope
		if err := json.Unmarshal(w.Body.Bytes(), &e); err != nil || e == nil {
			t.Fatal("missing native envelope", err)
		}
		if e.Network != nil || e.MailFingerprint != "" {
			t.Fatal("native prompt contains duplicate private transport payload")
		}
		return e
	}
	got := claim(b)
	if got.Sender != ha+"/codex" || got.Recipient != "codex" || got.RootOrigin.SessionID != "" {
		t.Fatalf("incorrect host/session boundary: %+v", got)
	}
	// Simulate a client acknowledgement, never a native assistant/model call.
	if err := b.Broker.Delivery(got.ID, "delivered", "offline simulated transport"); err != nil {
		t.Fatal(err)
	}
	reply := send(b, manager.MailSubmission{ID: "same-id", Recipient: got.Sender, Kind: "result", Content: "synthetic result", CorrelationID: got.CorrelationID, ReplyTo: got.ID})
	if reply.ID == first.ID {
		t.Fatal("different hosts collided on client message ID")
	}
	awaitNetwork(t, func() bool { return len(a.Broker.Snapshot().Messages) == 2 })
	got = claim(a)
	if got.ID != reply.ID || got.RootOrigin.SessionID != "native-A" {
		t.Fatalf("reply lost original local binding: %+v", got)
	}
	if len(a.Broker.Snapshot().Tasks) != 0 || len(b.Broker.Snapshot().Tasks) != 0 {
		t.Fatal("network invented workflow tasks")
	}
	// A local share toggle removes the directory authorization without changing
	// the existing native binding or manufacturing an idle/verified state.
	if _, err := a.manageAgent("codex", "unshare", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := a.publishRemote("codex", manager.MailSubmission{ID: "blocked", Recipient: hb + "/codex", Kind: "request", Content: "x", OriginRef: "test"}); err == nil {
		t.Fatal("unshared local agent sent remote mail")
	}
	if a.Broker.Snapshot().Endpoints["codex"].SessionID != "native-A" {
		t.Fatal("sharing changed native session")
	}
}

func TestManageBindingNeverFabricatesNativeCapability(t *testing.T) {
	s := networkFixture(t, "binding")
	w := managementCall(t, s, "/v1/manage/agent", `{"endpoint":"claude","action":"enable","session_id":"explicit-user-session"}`, s.Config.AdminToken)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	e := s.Broker.Snapshot().Endpoints["claude"]
	if e.SessionID != "explicit-user-session" || !e.Enabled || e.IdleWakeVerified || s.Broker.Snapshot().Sessions["claude"].State != "unknown" {
		t.Fatal("installation fabricated capability", e)
	}
	persisted, err := LoadConfig(s.management.dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, b := range persisted.Bindings {
		if b.Endpoint.ID == "claude" && b.Endpoint.SessionID != e.SessionID {
			t.Fatal("binding not persisted")
		}
	}
}

func TestUnavailableSavedLANListenerKeepsLocalManagementAvailable(t *testing.T) {
	f := newAPIFixture(t)
	dir := t.TempDir()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	saved, err := mesh.New(mesh.Options{DataDir: filepath.Join(dir, "network")})
	if err != nil {
		t.Fatal(err)
	}
	if err := saved.Configure(mesh.Config{Enabled: true, ListenAddr: listener.Addr().String(), Name: "unavailable"}); err != nil {
		t.Fatal(err)
	}
	if err := f.server.StartManagement(context.Background(), dir); err != nil {
		t.Fatalf("optional LAN failure disabled local mailbox: %v", err)
	}
	defer f.server.CloseManagement()
	w := managementCall(t, f.server, "/v1/manage", "", f.adminToken())
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	var response struct {
		Hub struct {
			Enabled bool   `json:"network_enabled"`
			Error   string `json:"network_error"`
		}
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Hub.Enabled || response.Hub.Error == "" {
		t.Fatal("failed LAN listener presented as running", w.Body.String())
	}
}
