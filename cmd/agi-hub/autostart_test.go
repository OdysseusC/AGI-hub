package main

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"agihub/internal/app"
	"agihub/internal/autostart"
)

func startupCLIData(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := app.InitConfig(dir); err != nil {
		t.Fatal(err)
	}
	return dir
}

func startupLaunchFile(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "launch.json")
	if err := os.WriteFile(path, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestAutostartCLIRegisterDoesNotEnableOrStartClient(t *testing.T) {
	dir := startupCLIData(t)
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	client := autostart.Client{Endpoint: "workbuddy", Enabled: true, Command: binary, Args: []string{"this-must-never-execute"}, Cwd: dir}
	body, _ := json.Marshal(client)
	path := startupLaunchFile(t, string(body))
	if err := runAutostart([]string{"register-client", "--data", dir, "--endpoint", "workbuddy", "--file", path}); err != nil {
		t.Fatal(err)
	}
	plan, err := autostart.Load(dir)
	if err != nil || plan.Enabled || len(plan.Clients) != 1 || plan.Clients[0].Enabled || plan.Clients[0].Command != binary {
		t.Fatalf("registration was not inert: %+v %v", plan, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "startup-state.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("registration started a runner: %v", err)
	}
	if err := runAutostart([]string{"run", "--data", dir}); err != nil {
		t.Fatalf("disabled foreground runner failed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".agi-hub-startup.lock")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("disabled CLI runner acquired a lock or started work: %v", err)
	}
}

func TestAutostartCLIRejectsUnsafeLaunchInput(t *testing.T) {
	for _, body := range []string{
		`{"endpoint":"codex","unexpected":"ignored"}`,
		`{"endpoint":"codex"} {"endpoint":"claude"}`,
		`{"endpoint":"claude"}`,
		`{"endpoint":"unknown"}`,
	} {
		t.Run(body, func(t *testing.T) {
			dir := startupCLIData(t)
			endpoint := "codex"
			if strings.Contains(body, "unknown") {
				endpoint = "unknown"
			}
			err := runAutostart([]string{"register-client", "--data", dir, "--endpoint", endpoint, "--file", startupLaunchFile(t, body)})
			if err == nil {
				t.Fatal("unsafe or mismatched launch JSON was accepted")
			}
			if _, err := os.Stat(filepath.Join(dir, "startup.json")); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("rejected launch changed startup plan: %v", err)
			}
		})
	}
	dir := startupCLIData(t)
	if err := runAutostart([]string{"register-client", "--data", dir, "--endpoint", "codex", "--file", "relative.json"}); err == nil {
		t.Fatal("relative launch path was accepted")
	}
}

func TestProbeStartupHubVerifiesServiceIdentityAndReadiness(t *testing.T) {
	for _, test := range []struct {
		name   string
		status int
		body   string
		ready  bool
	}{
		{"matching", 200, `{"service":"agi-hub","ready":true,"device_id":"expected-device"}`, true},
		{"ordinary-200", 200, `{"ok":true}`, false},
		{"html-200", 200, `<html>some other server</html>`, false},
		{"other-service", 200, `{"service":"other","ready":true,"device_id":"expected-device"}`, false},
		{"wrong-device", 200, `{"service":"agi-hub","ready":true,"device_id":"other-device"}`, false},
		{"not-ready", 200, `{"service":"agi-hub","ready":false,"device_id":"expected-device"}`, false},
		{"wrong-type", 200, `{"service":"agi-hub","ready":"true","device_id":"expected-device"}`, false},
		{"auth-rejected", 401, `{"service":"agi-hub","ready":true,"device_id":"expected-device"}`, false},
		{"forbidden", 403, `{"service":"agi-hub","ready":true,"device_id":"expected-device"}`, false},
		{"trailing-object", 200, `{"service":"agi-hub","ready":true,"device_id":"expected-device"} {}`, false},
		{"oversize", 200, `{"service":"` + strings.Repeat("x", 5000) + `","ready":true,"device_id":"expected-device"}`, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet || r.URL.Path != "/v1/health" || r.Header.Get("Authorization") != "Bearer startup-test-token" {
					t.Errorf("readiness probe used wrong endpoint or authentication: %s %s", r.Method, r.URL.Path)
				}
				w.WriteHeader(test.status)
				_, _ = w.Write([]byte(test.body))
			}))
			defer server.Close()
			cfg := app.Config{Listen: strings.TrimPrefix(server.URL, "http://"), AdminToken: "startup-test-token", DeviceID: "expected-device"}
			err := probeStartupHub(context.Background(), cfg)
			if test.ready && err != nil {
				t.Fatal(err)
			}
			if !test.ready && (err == nil || errors.Is(err, autostart.ErrHubUnavailable)) {
				t.Fatalf("occupied/untrusted listener did not fail closed: %v", err)
			}
		})
	}
}

func TestProbeStartupHubDoesNotFollowRedirectsOrTreatAuthAsAbsent(t *testing.T) {
	var forwarded bool
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		forwarded = true
		_, _ = w.Write([]byte(`{"service":"agi-hub","ready":true,"device_id":"expected-device"}`))
	}))
	defer target.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusFound)
	}))
	defer redirect.Close()
	cfg := app.Config{Listen: strings.TrimPrefix(redirect.URL, "http://"), AdminToken: "startup-test-token", DeviceID: "expected-device"}
	if err := probeStartupHub(context.Background(), cfg); err == nil || errors.Is(err, autostart.ErrHubUnavailable) {
		t.Fatalf("redirect allowed another Hub startup: %v", err)
	}
	if forwarded {
		t.Fatal("startup bearer token followed a redirect")
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	cfg.Listen = listener.Addr().String()
	_ = listener.Close()
	if err := probeStartupHub(context.Background(), cfg); !errors.Is(err, autostart.ErrHubUnavailable) {
		t.Fatalf("connection refusal did not identify an absent listener: %v", err)
	}
}
