package app

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"agihub/internal/manager"
)

type apiFixture struct {
	server    *Server
	workspace string
}

func newAPIFixture(t *testing.T) *apiFixture {
	t.Helper()
	workspace := t.TempDir()
	cfg := Config{
		Version: 1, DeviceID: "local-test-device", Listen: "127.0.0.1:7331",
		AdminToken: strings.Repeat("a", 64), Workspaces: map[string]string{"work": workspace},
	}
	b, err := manager.Open(t.TempDir(), cfg.DeviceID, cfg.Workspaces)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := b.Close(); err != nil {
			t.Errorf("Close broker: %v", err)
		}
	})
	for _, id := range []string{"origin", "worker", "other"} {
		if err := b.Register(manager.Endpoint{
			ID: id, DeviceID: cfg.DeviceID, SessionID: "native-" + id,
			Enabled: true, IdleWakeVerified: true, Modes: []string{"queue"}, Driver: "manual",
		}); err != nil {
			t.Fatal(err)
		}
		if err := b.SetSession(id, "idle", ""); err != nil {
			t.Fatal(err)
		}
	}
	if err := b.SetLeader("origin"); err != nil {
		t.Fatal(err)
	}
	return &apiFixture{server: &Server{Broker: b, Config: cfg}, workspace: workspace}
}

func (f *apiFixture) adminToken() string { return f.server.Config.AdminToken }
func (f *apiFixture) clientToken(endpoint string) string {
	return EndpointToken(f.adminToken(), endpoint)
}

func marshalBody(t *testing.T, value any) string {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(encoded)
}

func (f *apiFixture) request(method, path, token, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "http://"+f.server.Config.Listen+path, strings.NewReader(body))
	r.Host = f.server.Config.Listen
	r.Header.Set("Content-Type", "application/json")
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	f.server.Handler().ServeHTTP(w, r)
	return w
}

func checkStatus(t *testing.T, w *httptest.ResponseRecorder, want int) {
	t.Helper()
	if w.Code != want {
		t.Fatalf("HTTP status = %d, want %d; body=%s", w.Code, want, w.Body.String())
	}
}

func apiSubmission(id string) manager.Submission {
	return manager.Submission{
		ID: id, Recipient: "worker", Goal: "Produce a reviewed local artifact",
		Mode: "queue", MaxHandoffs: 1,
		Deadline: time.Now().Add(time.Hour).UTC().Format(time.RFC3339),
	}
}

func TestAPIReturnsVerifiedArtifactToOriginalEntryPoint(t *testing.T) {
	f := newAPIFixture(t)
	submission := apiSubmission("local-round-trip")
	submission.RequireArtifact = true
	w := f.request(http.MethodPost, "/v1/tasks", f.adminToken(), marshalBody(t, submission))
	checkStatus(t, w, http.StatusOK)
	var task manager.Task
	if err := json.Unmarshal(w.Body.Bytes(), &task); err != nil {
		t.Fatal(err)
	}
	if task.Origin != "origin" || task.Coordinator != "origin" || task.Execution != "waiting" {
		t.Fatalf("submission did not capture the selected entry point: %+v", task)
	}
	w = f.request(http.MethodPost, "/v1/inbox?endpoint=worker", f.clientToken("worker"), "")
	checkStatus(t, w, http.StatusOK)
	var envelope manager.Envelope
	if err := json.Unmarshal(w.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Kind != "task" || envelope.TaskID != task.ID || envelope.Recipient != "worker" || envelope.Delivery != "dispatching" {
		t.Fatalf("unexpected inbox task: %+v", envelope)
	}
	w = f.request(http.MethodPost, "/v1/delivery?endpoint=worker", f.clientToken("worker"), marshalBody(t, map[string]string{
		"message_id": envelope.ID, "outcome": "delivered", "detail": "Test transport accepted the message",
	}))
	checkStatus(t, w, http.StatusOK)
	if f.server.Broker.Snapshot().Tasks[task.ID].Execution != "waiting" {
		t.Fatal("HTTP delivery receipt incorrectly completed execution")
	}
	accepted := manager.Report{EventID: "round-trip-accepted", TaskID: task.ID, Endpoint: "worker", State: "accepted", RunID: "round-trip-run"}
	w = f.request(http.MethodPost, "/v1/report?endpoint=worker", f.clientToken("worker"), marshalBody(t, accepted))
	checkStatus(t, w, http.StatusOK)
	w = f.request(http.MethodPost, "/v1/inbox?endpoint=origin", f.clientToken("origin"), "")
	checkStatus(t, w, http.StatusOK)
	if strings.TrimSpace(w.Body.String()) != "null" {
		t.Fatal("acceptance receipt generated a final return message")
	}
	contents := []byte("Reviewed artifact created by local integration test\n")
	if err := os.WriteFile(filepath.Join(f.workspace, "result.txt"), contents, 0600); err != nil {
		t.Fatal(err)
	}
	completed := manager.Report{
		EventID: "round-trip-completed", TaskID: task.ID, Endpoint: "worker",
		State: "succeeded", RunID: accepted.RunID, Summary: "Reviewed artifact ready",
		Artifacts: []manager.Artifact{{DeviceID: f.server.Config.DeviceID, WorkspaceID: "work", Path: "result.txt"}},
	}
	w = f.request(http.MethodPost, "/v1/report?endpoint=worker", f.clientToken("worker"), marshalBody(t, completed))
	checkStatus(t, w, http.StatusOK)
	w = f.request(http.MethodPost, "/v1/inbox?endpoint=origin", f.clientToken("origin"), "")
	checkStatus(t, w, http.StatusOK)
	var result manager.Envelope
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Kind != "result" || result.TaskID != task.ID || result.Recipient != "origin" {
		t.Fatalf("result did not return to original entry point: %+v", result)
	}
	var returnedTask manager.Task
	if err := json.Unmarshal([]byte(result.Content), &returnedTask); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(contents)
	if returnedTask.Execution != "succeeded" || returnedTask.Summary != completed.Summary || len(returnedTask.Artifacts) != 1 || returnedTask.Artifacts[0].SHA256 != hex.EncodeToString(sum[:]) {
		t.Fatalf("return message lacks verified completion evidence: %+v", returnedTask)
	}
	w = f.request(http.MethodPost, "/v1/delivery?endpoint=origin", f.clientToken("origin"), marshalBody(t, map[string]string{"message_id": result.ID, "outcome": "delivered", "detail": "Original entry received verified result"}))
	checkStatus(t, w, http.StatusOK)
	w = f.request(http.MethodGet, "/v1/state", f.adminToken(), "")
	checkStatus(t, w, http.StatusOK)
	var snapshot manager.Snapshot
	if err := json.Unmarshal(w.Body.Bytes(), &snapshot); err != nil {
		t.Fatal(err)
	}
	if snapshot.Messages[result.ID].Delivery != "delivered" || snapshot.Tasks[task.ID].Execution != "succeeded" {
		t.Fatal("state does not distinguish successful work from successful result delivery")
	}
}

func TestAPISeparatesAdministrativeAndEndpointPermissions(t *testing.T) {
	f := newAPIFixture(t)
	for _, tc := range []struct {
		name   string
		method string
		path   string
		token  string
		body   string
	}{
		{name: "anonymous-state", method: http.MethodGet, path: "/v1/state"},
		{name: "invalid-admin-token", method: http.MethodGet, path: "/v1/state", token: "invalid"},
		{name: "client-cannot-read-all-state", method: http.MethodGet, path: "/v1/state", token: f.clientToken("worker")},
		{name: "client-cannot-read-peer-binding", method: http.MethodGet, path: "/v1/endpoint?endpoint=other", token: f.clientToken("worker")},
		{name: "client-cannot-change-leader", method: http.MethodPost, path: "/v1/leader", token: f.clientToken("worker"), body: `{"endpoint":"other"}`},
		{name: "client-cannot-submit", method: http.MethodPost, path: "/v1/tasks", token: f.clientToken("worker"), body: marshalBody(t, apiSubmission("unauthorized-task"))},
		{name: "client-cannot-change-session", method: http.MethodPost, path: "/v1/session", token: f.clientToken("worker"), body: `{"endpoint":"other","state":"idle"}`},
		{name: "client-cannot-consume-other-inbox", method: http.MethodPost, path: "/v1/inbox?endpoint=other", token: f.clientToken("worker")},
		{name: "unregistered-client-token", method: http.MethodPost, path: "/v1/inbox?endpoint=unknown", token: f.clientToken("unknown")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			checkStatus(t, f.request(tc.method, tc.path, tc.token, tc.body), http.StatusUnauthorized)
		})
	}
	if snapshot := f.server.Broker.Snapshot(); snapshot.Leader != "origin" || len(snapshot.Tasks) != 0 || snapshot.Sessions["other"].State != "idle" {
		t.Fatal("unauthorized request changed administrative state")
	}
	checkStatus(t, f.request(http.MethodGet, "/v1/state", f.adminToken(), ""), http.StatusOK)
	checkStatus(t, f.request(http.MethodPost, "/v1/leader", f.adminToken(), `{"endpoint":"other"}`), http.StatusOK)
	if f.server.Broker.Snapshot().Leader != "other" {
		t.Fatal("administrator could not change the leader")
	}
}

func TestEndpointMetadataIsScopedAndDoesNotExposeOtherState(t *testing.T) {
	f := newAPIFixture(t)
	w := f.request(http.MethodGet, "/v1/endpoint?endpoint=worker", f.clientToken("worker"), "")
	checkStatus(t, w, http.StatusOK)
	var binding struct {
		ProtocolVersion int              `json:"protocol_version"`
		Endpoint        manager.Endpoint `json:"endpoint"`
		Session         manager.Session  `json:"session"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &binding); err != nil {
		t.Fatal(err)
	}
	if binding.ProtocolVersion != 1 || binding.Endpoint.ID != "worker" || binding.Endpoint.SessionID != "native-worker" || binding.Session.State != "idle" {
		t.Fatal("incorrect endpoint metadata")
	}
	for _, secret := range []string{f.adminToken(), f.clientToken("worker"), "native-other", "resource_owners", "\"tasks\""} {
		if strings.Contains(w.Body.String(), secret) {
			t.Fatal("endpoint metadata exposed unrelated state or credentials")
		}
	}
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("metadata permits caching")
	}
}

func TestEndpointCannotAcknowledgeOrReportAnotherEndpointsWork(t *testing.T) {
	f := newAPIFixture(t)
	checkStatus(t, f.request(http.MethodPost, "/v1/tasks", f.adminToken(), marshalBody(t, apiSubmission("owned-task"))), http.StatusOK)
	w := f.request(http.MethodPost, "/v1/inbox?endpoint=worker", f.clientToken("worker"), "")
	checkStatus(t, w, http.StatusOK)
	var env manager.Envelope
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	checkStatus(t, f.request(http.MethodPost, "/v1/delivery?endpoint=other", f.clientToken("other"), marshalBody(t, map[string]string{"message_id": env.ID, "outcome": "delivered"})), http.StatusUnprocessableEntity)
	report := manager.Report{EventID: "forged-receipt", TaskID: env.TaskID, Endpoint: "other", State: "accepted", RunID: "forged-run"}
	checkStatus(t, f.request(http.MethodPost, "/v1/report?endpoint=other", f.clientToken("other"), marshalBody(t, report)), http.StatusUnprocessableEntity)
	report.Endpoint = "worker"
	checkStatus(t, f.request(http.MethodPost, "/v1/report?endpoint=other", f.clientToken("other"), marshalBody(t, report)), http.StatusUnprocessableEntity)
	checkStatus(t, f.request(http.MethodPost, "/v1/report?endpoint=worker", f.clientToken("other"), marshalBody(t, report)), http.StatusUnauthorized)
	snapshot := f.server.Broker.Snapshot()
	if snapshot.Messages[env.ID].Delivery != "dispatching" || snapshot.Tasks[env.TaskID].Execution != "waiting" || len(snapshot.EventIDs) != 0 {
		t.Fatal("another endpoint forged a delivery or execution receipt")
	}
}

func TestAPIBlocksUntrustedHostAndOrigin(t *testing.T) {
	f := newAPIFixture(t)
	for _, tc := range []struct {
		name   string
		host   string
		origin string
		want   int
	}{
		{name: "correct-host-no-origin", host: f.server.Config.Listen, want: http.StatusOK},
		{name: "matching-origin", host: f.server.Config.Listen, origin: "http://" + f.server.Config.Listen, want: http.StatusOK},
		{name: "foreign-host", host: "attacker.example:7331", want: http.StatusForbidden},
		{name: "localhost-alias", host: "localhost:7331", want: http.StatusForbidden},
		{name: "foreign-origin", host: f.server.Config.Listen, origin: "http://attacker.example", want: http.StatusForbidden},
		{name: "opaque-origin", host: f.server.Config.Listen, origin: "null", want: http.StatusForbidden},
		{name: "wrong-origin-scheme", host: f.server.Config.Listen, origin: "https://" + f.server.Config.Listen, want: http.StatusForbidden},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "http://"+f.server.Config.Listen+"/v1/state", nil)
			r.Host = tc.host
			r.Header.Set("Authorization", "Bearer "+f.adminToken())
			if tc.origin != "" {
				r.Header.Set("Origin", tc.origin)
			}
			w := httptest.NewRecorder()
			f.server.Handler().ServeHTTP(w, r)
			checkStatus(t, w, tc.want)
		})
	}
}

func TestAPIRejectsOversizedUnknownAndTrailingJSON(t *testing.T) {
	base := apiSubmission("invalid-json")
	oversized := base
	oversized.Context = strings.Repeat("x", 1<<20)
	var fields map[string]any
	if err := json.Unmarshal([]byte(marshalBody(t, base)), &fields); err != nil {
		t.Fatal(err)
	}
	fields["silently_ignored_field"] = true
	for _, tc := range []struct {
		name string
		body string
	}{
		{name: "oversized", body: marshalBody(t, oversized)},
		{name: "unknown-field", body: marshalBody(t, fields)},
		{name: "extra-json-object", body: marshalBody(t, base) + " {}"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newAPIFixture(t)
			checkStatus(t, f.request(http.MethodPost, "/v1/tasks", f.adminToken(), tc.body), http.StatusUnprocessableEntity)
			if len(f.server.Broker.Snapshot().Tasks) != 0 {
				t.Fatal("rejected JSON persisted a task")
			}
		})
	}
}

func readSSEFrame(t *testing.T, reader *bufio.Reader) string {
	t.Helper()
	var frame strings.Builder
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatalf("reading event stream before request deadline: %v", err)
		}
		frame.WriteString(line)
		if line == "\n" {
			return frame.String()
		}
	}
}

func TestEventStreamSendsInitialAndCommittedChangesWithoutPolling(t *testing.T) {
	f := newAPIFixture(t)
	ts := httptest.NewUnstartedServer(f.server.Handler())
	f.server.Config.Listen = ts.Listener.Addr().String()
	ts.Start()
	defer ts.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	r, err := http.NewRequestWithContext(ctx, http.MethodGet, ts.URL+"/v1/events?endpoint=worker", nil)
	if err != nil {
		t.Fatal(err)
	}
	r.Header.Set("Authorization", "Bearer "+f.clientToken("worker"))
	response, err := ts.Client().Do(r)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK || response.Header.Get("Content-Type") != "text/event-stream" {
		t.Fatalf("unexpected event stream status=%d type=%q", response.StatusCode, response.Header.Get("Content-Type"))
	}
	reader := bufio.NewReader(response.Body)
	if frame := readSSEFrame(t, reader); !strings.HasPrefix(frame, "event: change\n") {
		t.Fatalf("missing initial event: %q", frame)
	}
	checkStatus(t, f.request(http.MethodPost, "/v1/tasks", f.adminToken(), marshalBody(t, apiSubmission("event-driven-task"))), http.StatusOK)
	if frame := readSSEFrame(t, reader); !strings.HasPrefix(frame, "event: change\n") {
		t.Fatalf("committed work did not emit a change event: %q", frame)
	}
	if _, ok := f.server.Broker.Snapshot().Tasks["event-driven-task"]; !ok {
		t.Fatal("event was published before corresponding state was committed")
	}
	// Cancellation unblocks the streaming request and lets the server close;
	// the short context deadline also prevents keepalive from passing this test.
	cancel()
}

func TestDefaultTemplateLoadsAndStartsWithDisabledUnboundAssistants(t *testing.T) {
	dir := t.TempDir()
	if err := InitConfig(dir); err != nil {
		t.Fatal(err)
	}
	privateConfig := filepath.Join(dir, "config.json")
	original, err := os.ReadFile(privateConfig)
	if err != nil {
		t.Fatal(err)
	}
	if err := InitConfig(dir); err == nil {
		t.Fatal("initialization must not overwrite existing personal configuration")
	}
	unchanged, err := os.ReadFile(privateConfig)
	if err != nil || string(original) != string(unchanged) {
		t.Fatal("repeat initialization changed personal configuration")
	}
	cfg, err := LoadConfig(dir)
	if err != nil {
		t.Fatalf("load initialized template: %v", err)
	}
	b, err := manager.Open(dir, cfg.DeviceID, cfg.Workspaces)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	if len(cfg.Bindings) == 0 {
		t.Fatal("template does not expose any candidate bindings")
	}
	for _, binding := range cfg.Bindings {
		if binding.Endpoint.Enabled || binding.Endpoint.SessionID != "" {
			t.Fatal("new candidate should start disabled and unbound")
		}
		if err := b.Register(binding.Endpoint); err != nil {
			t.Fatalf("unbound disabled candidate prevented startup: %v", err)
		}
		if message, err := b.Next(binding.Endpoint.ID); err != nil || message != nil {
			t.Fatalf("disabled candidate attempted delivery: message=%+v err=%v", message, err)
		}
	}
	f := &apiFixture{server: &Server{Broker: b, Config: cfg}}
	checkStatus(t, f.request(http.MethodGet, "/v1/state", cfg.AdminToken, ""), http.StatusOK)
	w := f.request(http.MethodGet, "/", "", "")
	checkStatus(t, w, http.StatusOK)
	if !strings.Contains(w.Body.String(), "助手会话") || !strings.Contains(w.Body.String(), "任务与成果") {
		t.Fatal("initialized sidecar cannot render its Chinese status page")
	}
}

func TestStatusPageExplainsObservableStatesWithoutLeakingToken(t *testing.T) {
	f := newAPIFixture(t)
	w := f.request(http.MethodGet, "/", "", "")
	checkStatus(t, w, http.StatusOK)
	if !strings.HasPrefix(w.Header().Get("Content-Type"), "text/html") || !strings.Contains(w.Body.String(), `lang="zh-CN"`) {
		t.Fatal("status page is not a Chinese HTML document")
	}
	for _, text := range []string{"需要你处理", "投递不确定", "成果位置", "本机管理密钥"} {
		if !strings.Contains(w.Body.String(), text) {
			t.Fatalf("status page lacks user-facing concept %q", text)
		}
	}
	if strings.Contains(w.Body.String(), f.adminToken()) {
		t.Fatal("public status page leaked configured administrative token")
	}
	if w.Header().Get("X-Content-Type-Options") != "nosniff" || !strings.Contains(w.Header().Get("Content-Security-Policy"), "frame-ancestors 'none'") {
		t.Fatal("status page lacks browser isolation headers")
	}
}

func TestConfigRejectsNonLoopbackControlPlane(t *testing.T) {
	for _, address := range []string{"0.0.0.0:7331", "192.0.2.1:7331", "localhost:7331"} {
		t.Run(address, func(t *testing.T) {
			dir := t.TempDir()
			cfg := Config{Version: 1, DeviceID: "local", AdminToken: strings.Repeat("a", 64), Listen: address}
			if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(marshalBody(t, cfg)), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := LoadConfig(dir); err == nil {
				t.Fatalf("control-plane address %q must be rejected", address)
			}
		})
	}
}
