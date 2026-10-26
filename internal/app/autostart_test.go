package app

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"agihub/internal/autostart"
)

func TestRegisterStartupClientRequiresKnownEndpointAndNativeHost(t *testing.T) {
	dir := t.TempDir()
	if err := InitConfig(dir); err != nil {
		t.Fatal(err)
	}
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, endpoint := range []string{"codex", "claude"} {
		client := autostart.Client{Endpoint: endpoint, Enabled: true, Command: binary, Args: []string{"must-never-execute"}, Cwd: dir, Environment: map[string]string{"CLAUDE_CODE_ENTRYPOINT": "must-never-persist"}}
		if err := RegisterStartupClient(dir, client); err != nil {
			t.Fatal(err)
		}
	}
	plan, err := autostart.Load(dir)
	if err != nil || plan.Enabled || len(plan.Clients) != 2 {
		t.Fatalf("native registration failed: %+v %v", plan, err)
	}
	for _, client := range plan.Clients {
		if !client.RequiresHost || client.Enabled || client.Command != "" || client.Cwd != "" || len(client.Args) != 0 || len(client.Environment) != 0 {
			t.Fatalf("native host launch or environment was captured: %+v", client)
		}
	}
	before, _ := os.ReadFile(filepath.Join(dir, "startup.json"))
	if err := RegisterStartupClient(dir, autostart.Client{Endpoint: "not-configured", Command: binary, Cwd: dir}); err == nil {
		t.Fatal("unknown local endpoint was registered")
	}
	after, _ := os.ReadFile(filepath.Join(dir, "startup.json"))
	if string(after) != string(before) {
		t.Fatal("rejected endpoint changed the startup plan")
	}
}

func TestStartupManagementOnlySelectsRegisteredIDsAndRejectsCommands(t *testing.T) {
	f := newAPIFixture(t)
	dir := t.TempDir()
	if err := InitConfig(dir); err != nil {
		t.Fatal(err)
	}
	if err := f.server.StartManagement(context.Background(), dir); err != nil {
		t.Fatal(err)
	}
	defer f.server.CloseManagement()
	for _, body := range []string{
		`{"enabled":true,"clients":[],"command":"arbitrary-command"}`,
		`{"enabled":true,"clients":[],"binary":"arbitrary-binary"}`,
		`{"enabled":true,"clients":[],"args":["arbitrary-args"]}`,
		`{"enabled":true,"clients":[],"environment":{"API_KEY":"private"}}`,
		`{"enabled":true,"clients":["not-configured"]}`,
		`{"enabled":true,"clients":["codex"]}`,
	} {
		w := managementCall(t, f.server, "/v1/manage/autostart", body, f.adminToken())
		if w.Code != http.StatusUnprocessableEntity {
			t.Fatalf("unsafe startup API input accepted: %d %s %s", w.Code, body, w.Body.String())
		}
	}
	for _, path := range []string{"startup.json", "startup-state.json", ".agi-hub-startup.lock"} {
		if _, err := os.Stat(filepath.Join(dir, path)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("rejected API action changed startup or started a runner: %s %v", path, err)
		}
	}
}

func TestStartupManagementEnforcesAdminHostAndOriginBeforeMutation(t *testing.T) {
	f := newAPIFixture(t)
	for _, test := range []struct {
		name, token, host, origin string
		want                      int
	}{
		{"no-token", "", f.server.Config.Listen, "", http.StatusUnauthorized},
		{"endpoint-token", f.clientToken("origin"), f.server.Config.Listen, "", http.StatusUnauthorized},
		{"wrong-host", f.adminToken(), "other.invalid", "", http.StatusForbidden},
		{"foreign-origin", f.adminToken(), f.server.Config.Listen, "https://other.invalid", http.StatusForbidden},
	} {
		t.Run(test.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodPost, "http://"+f.server.Config.Listen+"/v1/manage/autostart", strings.NewReader(`{"enabled":true,"clients":[]}`))
			r.Host = test.host
			if test.token != "" {
				r.Header.Set("Authorization", "Bearer "+test.token)
			}
			if test.origin != "" {
				r.Header.Set("Origin", test.origin)
			}
			w := httptest.NewRecorder()
			f.server.Handler().ServeHTTP(w, r)
			if w.Code != test.want {
				t.Fatalf("startup authentication boundary: %d %s", w.Code, w.Body.String())
			}
		})
	}
}

func TestStartupHealthIsAdminOnlyAndDoesNotClaimNativeReadiness(t *testing.T) {
	f := newAPIFixture(t)
	before := f.server.Broker.Snapshot()
	for _, token := range []string{"", f.clientToken("worker")} {
		w := f.request(http.MethodGet, "/v1/health?endpoint=worker", token, "")
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("Client could read administrative Hub readiness: %d %s", w.Code, w.Body.String())
		}
	}
	w := f.request(http.MethodGet, "/v1/health", f.adminToken(), "")
	if w.Code != http.StatusOK {
		t.Fatalf("Hub health unavailable: %d %s", w.Code, w.Body.String())
	}
	var health map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &health); err != nil || len(health) != 3 || health["service"] != "agi-hub" || health["ready"] != true || health["device_id"] != f.server.Config.DeviceID {
		t.Fatalf("unexpected readiness claim: %v %v", health, err)
	}
	if !reflect.DeepEqual(before, f.server.Broker.Snapshot()) {
		t.Fatal("health probe changed business or native session state")
	}
}

func TestStartupStatusSeparatesStaleSnapshotAndOmitsLaunchDetails(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows registration status invokes PowerShell; this test performs no OS task operations")
	}
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	plan := autostart.Plan{Version: 1, Binary: filepath.Join(dir, "private-hub-binary"), Clients: []autostart.Client{{
		Endpoint: "standalone", Command: filepath.Join(dir, "private-client-binary"), Cwd: dir,
		Args: []string{"private-launch-argument"}, Environment: map[string]string{"HTTPS_PROXY": "http://private-proxy-host.invalid:8080"},
	}}}
	if err := autostart.Save(dir, plan); err != nil {
		t.Fatal(err)
	}
	previous := autostart.RuntimeState{PID: 987654, Components: []autostart.ComponentState{{ID: "client:standalone", State: "running", PID: 987653, Attempts: 1}}}
	body, _ := json.Marshal(previous)
	if err := os.WriteFile(filepath.Join(dir, "startup-state.json"), body, 0600); err != nil {
		t.Fatal(err)
	}
	status, err := StartupStatus(dir)
	if err != nil {
		t.Fatal(err)
	}
	if status["runtime_active"] != false || status["configured_enabled"] != false || status["enabled"] != false {
		t.Fatalf("old running snapshot was reported active: %+v", status)
	}
	if observed, ok := status["runtime"].(autostart.RuntimeState); !ok || observed.PID != previous.PID || observed.Components[0].State != "running" {
		t.Fatalf("status lost its explicitly historical observation: %+v", status["runtime"])
	}
	encoded, _ := json.Marshal(status)
	for _, private := range []string{"private-hub-binary", "private-client-binary", "private-launch-argument", "private-proxy-host"} {
		if strings.Contains(string(encoded), private) {
			t.Fatalf("startup API exposed launch details: %s", private)
		}
	}
	if runtime.GOOS == "linux" && status["supported"] != false {
		t.Fatal("Linux was reported as having an implemented native login registration")
	}
}
