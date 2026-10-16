package app

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"agihub/internal/manager"
)

func clientConfigFixture() Config {
	return Config{
		Version: 1, DeviceID: "local", Listen: "127.0.0.1:7331", AdminToken: strings.Repeat("a", 64),
		Bindings: []Binding{{Endpoint: manager.Endpoint{ID: "community-client", DeviceID: "local", Driver: "community-assistant", Modes: []string{"queue"}}}},
	}
}

func TestInitKeepsEveryNativeAssistantDisabled(t *testing.T) {
	dir := t.TempDir()
	if err := InitConfig(dir); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"codex": "codex", "workbuddy": "workbuddy", "claude": "claude-code", "grok": "grok-routine"}
	if len(cfg.Bindings) != len(want) || cfg.Leader != "" {
		t.Fatal("initial configuration must not select a leader or omit a known client")
	}
	for _, b := range cfg.Bindings {
		if want[b.Endpoint.ID] != b.Endpoint.Driver || b.Driver.Type != "external" || b.Endpoint.Enabled || b.Endpoint.SessionID != "" || b.Endpoint.IdleWakeVerified || b.Endpoint.ResultQueueWhileBusyVerified {
			t.Fatal("initialization must not enable or claim verification for any native assistant")
		}
	}
}

func writeConfigFixture(t *testing.T, value any) string {
	t.Helper()
	dir := t.TempDir()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.json"), encoded, 0600); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestConfigAcceptsIndependentClientWithoutNativeCredentials(t *testing.T) {
	cfg, err := LoadConfig(writeConfigFixture(t, clientConfigFixture()))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Bindings[0].Driver.Type != "external" || cfg.Bindings[0].Endpoint.Driver != "community-assistant" {
		t.Fatal("native product identification must remain separate from independent consumption")
	}
}

func TestConfigRefusesLegacyNativeTransportSettings(t *testing.T) {
	for _, field := range []string{"executable", "args", "url", "headers", "body", "timeout_seconds", "access_token"} {
		t.Run(field, func(t *testing.T) {
			cfg := clientConfigFixture()
			encoded, err := json.Marshal(cfg)
			if err != nil {
				t.Fatal(err)
			}
			var value map[string]any
			if err := json.Unmarshal(encoded, &value); err != nil {
				t.Fatal(err)
			}
			binding := value["bindings"].([]any)[0].(map[string]any)
			binding["driver"] = map[string]any{"type": "external", field: "private-value-must-not-be-logged"}
			_, err = LoadConfig(writeConfigFixture(t, value))
			if err == nil || !strings.Contains(err.Error(), "private Client config") {
				t.Fatal("native configuration was silently accepted instead of requiring explicit Client migration")
			}
			if strings.Contains(err.Error(), "private-value-must-not-be-logged") {
				t.Fatal("configuration error exposed a private transport value")
			}
		})
	}
	for _, kind := range []string{"codex-queue", "webhook", "command"} {
		cfg := clientConfigFixture()
		cfg.Bindings[0].Driver.Type = kind
		if _, err := LoadConfig(writeConfigFixture(t, cfg)); err == nil || !strings.Contains(err.Error(), "must be external") {
			t.Fatalf("legacy transport %q did not require migration", kind)
		}
	}
}

func TestConfigRefusesTrailingJSON(t *testing.T) {
	dir := writeConfigFixture(t, clientConfigFixture())
	f, err := os.OpenFile(filepath.Join(dir, "config.json"), os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(" {} "); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadConfig(dir); err == nil {
		t.Fatal("trailing configuration was ignored")
	}
}

// A replacement Client opens new connections to the same inbox. Its predecessor's
// durable claim must never be issued again, even when no receipt was saved.
func TestReplacementClientCannotReplayClaimOrBypassResourceOwnership(t *testing.T) {
	f := newAPIFixture(t)
	job := apiSubmission("claimed-before-client-exit")
	job.Resources = []string{"workspace:shared"}
	checkStatus(t, f.request(http.MethodPost, "/v1/tasks", f.adminToken(), marshalBody(t, job)), http.StatusOK)
	w := f.request(http.MethodPost, "/v1/inbox?endpoint=worker", f.clientToken("worker"), "")
	checkStatus(t, w, http.StatusOK)
	var claimed manager.Envelope
	if err := json.Unmarshal(w.Body.Bytes(), &claimed); err != nil {
		t.Fatal(err)
	}
	second := apiSubmission("after-client-replacement")
	checkStatus(t, f.request(http.MethodPost, "/v1/tasks", f.adminToken(), marshalBody(t, second)), http.StatusOK)
	for _, outcome := range []string{"dispatching", "uncertain"} {
		if outcome == "uncertain" {
			checkStatus(t, f.request(http.MethodPost, "/v1/delivery?endpoint=worker", f.clientToken("worker"), marshalBody(t, map[string]string{"message_id": claimed.ID, "outcome": outcome})), http.StatusOK)
		}
		w = f.request(http.MethodPost, "/v1/inbox?endpoint=worker", f.clientToken("worker"), "")
		checkStatus(t, w, http.StatusOK)
		if strings.TrimSpace(w.Body.String()) != "null" {
			t.Fatal("replacement Client received a previously claimed or conflicting subsequent task")
		}
		state := f.server.Broker.Snapshot()
		if state.Messages[claimed.ID].Delivery != outcome || state.ResourceOwners["workspace:shared"] != job.ID {
			t.Fatal("Client replacement released evidence or resources without reconciliation")
		}
	}
}
