package app

import (
	"context"
	"encoding/json"
	"net/http"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"agihub/internal/manager"
)

// Exercise the same durable config/journal boundary as serve, without launching
// a Client, an assistant, or a separate process.
func reopenConfiguredRecoveryFixture(t *testing.T, previous *Server) *Server {
	t.Helper()
	dir := previous.management.dir
	if err := previous.CloseManagement(); err != nil {
		t.Fatal(err)
	}
	if err := previous.Broker.Close(); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	b, err := manager.Open(dir, cfg.DeviceID, cfg.Workspaces)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = b.Close() })
	for _, binding := range cfg.Bindings {
		if err := b.Register(binding.Endpoint); err != nil {
			t.Fatal(err)
		}
	}
	s := &Server{Broker: b, Config: cfg}
	if err := s.StartManagement(context.Background(), dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.CloseManagement() })
	return s
}

func recoveryAgent(t *testing.T, s *Server, endpoint string) map[string]any {
	t.Helper()
	w := managementCall(t, s, "/v1/manage", "", s.Config.AdminToken)
	checkStatus(t, w, http.StatusOK)
	var out struct {
		Agents []map[string]any `json:"agents"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	for _, agent := range out.Agents {
		if agent["id"] == endpoint {
			return agent
		}
	}
	t.Fatalf("missing endpoint %q", endpoint)
	return nil
}

func TestVerifiedBindingSurvivesManagementSaveAndConfiguredRestart(t *testing.T) {
	s := networkFixture(t, "persisted-verification")
	dir := s.management.dir
	cfg, err := LoadConfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	var approved manager.Endpoint
	for i := range cfg.Bindings {
		e := &cfg.Bindings[i].Endpoint
		if e.ID != "codex" {
			continue
		}
		e.IdleWakeVerified = true
		e.ResultQueueWhileBusyVerified = true
		e.MailboxWhileBusyVerified = true
		e.PeerProtocolVersion = 1
		e.PeerWhileBusyVerified = true
		approved = *e
	}
	if err := writePrivateJSON(filepath.Join(dir, "config.json"), cfg); err != nil {
		t.Fatal(err)
	}
	if err := s.Broker.Register(approved); err != nil {
		t.Fatal(err)
	}
	for _, action := range []string{"disable", "enable"} {
		body := marshalBody(t, map[string]string{"endpoint": "codex", "action": action, "session_id": approved.SessionID})
		checkStatus(t, managementCall(t, s, "/v1/manage/agent", body, s.Config.AdminToken), http.StatusOK)
		got := s.Broker.Snapshot().Endpoints["codex"]
		want := approved
		want.Enabled = action == "enable"
		if !reflect.DeepEqual(want, got) {
			t.Fatalf("same-binding management save changed verification: %+v", got)
		}
	}
	s = reopenConfiguredRecoveryFixture(t, s)
	if got := s.Broker.Snapshot().Endpoints["codex"]; !reflect.DeepEqual(approved, got) {
		t.Fatalf("configured restart lost persisted verification: %+v", got)
	}
	if state := s.Broker.Snapshot().Sessions["codex"].State; state != "unknown" {
		t.Fatalf("restart invented current native state %q", state)
	}
}

func TestConfiguredVerificationRevocationIsNotOverriddenByOldJournal(t *testing.T) {
	s := networkFixture(t, "verification-revocation")
	if !s.Broker.Snapshot().Endpoints["codex"].IdleWakeVerified {
		t.Fatal("fixture lacks the old durable verification")
	}
	cfg, err := LoadConfig(s.management.dir)
	if err != nil {
		t.Fatal(err)
	}
	for i := range cfg.Bindings {
		if cfg.Bindings[i].Endpoint.ID == "codex" {
			cfg.Bindings[i].Endpoint.IdleWakeVerified = false
		}
	}
	if err := writePrivateJSON(filepath.Join(s.management.dir, "config.json"), cfg); err != nil {
		t.Fatal(err)
	}
	s = reopenConfiguredRecoveryFixture(t, s)
	if s.Broker.Snapshot().Endpoints["codex"].IdleWakeVerified {
		t.Fatal("old journal overrode the operator's explicit verification revocation")
	}
}

func TestManagementReconnectExposesUnknownNativeStateUntilObservation(t *testing.T) {
	s := networkFixture(t, "native-state-observability")
	fixture := &apiFixture{server: s}
	acquireTestLease(t, fixture, "codex", "same-desktop-client")
	if agent := recoveryAgent(t, s, "codex"); agent["state"] != "verified" {
		t.Fatalf("fixture does not represent previously verified native state: %v", agent)
	}
	s = reopenConfiguredRecoveryFixture(t, s)
	fixture.server = s
	acquireTestLease(t, fixture, "codex", "same-desktop-client")
	metadata := managementCall(t, s, "/v1/endpoint?endpoint=codex", "", EndpointToken(s.Config.AdminToken, "codex"))
	checkStatus(t, metadata, http.StatusOK)
	var endpoint struct {
		Session manager.Session `json:"session"`
	}
	if err := json.Unmarshal(metadata.Body.Bytes(), &endpoint); err != nil || endpoint.Session.State != "unknown" {
		t.Fatalf("restart did not retain explicit unknown metadata: %v %+v", err, endpoint.Session)
	}
	agent := recoveryAgent(t, s, "codex")
	if agent["state"] != "connected" {
		t.Fatalf("lease should establish transport connectivity only: %v", agent)
	}
	// The management card is what the user sees. It must distinguish its live
	// transport from the still-unknown native state, without demanding rebind.
	if detail, _ := agent["detail"].(string); !strings.Contains(detail, "unknown") {
		t.Errorf("connected management card hides current native unknown state: %q", detail)
	}
	binding := s.Broker.Snapshot().Endpoints["codex"]
	checkStatus(t, endpointSessionRequest(fixture, "codex", fixture.clientToken("codex"), binding.SessionID, "", `{"state":"idle"}`), http.StatusOK)
	if after := recoveryAgent(t, s, "codex"); after["state"] != "verified" {
		t.Fatalf("confirmed same-binding state did not restore readiness: %v", after)
	}
	if got := s.Broker.Snapshot().Endpoints["codex"]; !reflect.DeepEqual(binding, got) {
		t.Fatal("observing current native state changed binding or verification")
	}
}
