package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"agihub/internal/app"
	"agihub/internal/install"
	"agihub/internal/manager"
)

func TestCLIReadsLegalStateLargerThanTwoMiB(t *testing.T) {
	state := manager.Snapshot{Version: 1, Tasks: map[string]manager.Task{}, Messages: map[string]manager.Envelope{}}
	for i := 0; i < 17; i++ {
		id := fmt.Sprintf("large-%d", i)
		sub := manager.Submission{ID: id, Goal: "valid goal", Context: strings.Repeat("x", 64<<10), Mode: "queue", MaxHandoffs: 1}
		state.Tasks[id] = manager.Task{Submission: sub, Execution: "waiting"}
		content, _ := json.Marshal(sub)
		state.Messages[id] = manager.Envelope{ID: id, TaskID: id, Content: string(content), Delivery: "pending"}
	}
	encoded, _ := json.Marshal(state)
	if len(encoded) <= 2<<20 || len(encoded) >= 4<<20 {
		t.Fatal("fixture does not exercise a legal large state")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(encoded)
	}))
	defer server.Close()
	value, err := requestValue(app.Config{Listen: strings.TrimPrefix(server.URL, "http://"), AdminToken: strings.Repeat("test", 16)}, "GET", "/v1/state", "", nil)
	if err != nil || len(value.(map[string]any)["tasks"].(map[string]any)) != 17 {
		t.Fatal("legal state was truncated:", err)
	}
}

func TestCLIManagedSkillSyncPreservesConflictsAndScopedReportHelper(t *testing.T) {
	data, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	skills, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := app.InitConfig(data); err != nil {
		t.Fatal(err)
	}
	config, err := app.LoadConfig(data)
	if err != nil {
		t.Fatal(err)
	}
	_, version, err := install.CurrentSkill()
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(app.SkillRules{ProtocolVersion: 1, Revision: install.SkillRevision, Version: version})
	}))
	defer server.Close()
	config.Listen = strings.TrimPrefix(server.URL, "http://")
	encoded, _ := json.Marshal(config)
	if err := os.WriteFile(filepath.Join(data, "config.json"), encoded, 0600); err != nil {
		t.Fatal(err)
	}
	hub := filepath.Join(data, "hub")
	report := filepath.Join(data, "report-helper")
	args := []string{"install-skill", "--data", data, "--host", "workbuddy", "--endpoint", "workbuddy", "--skills-dir", skills, "--binary", report, "--hub-binary", hub}
	if err := run(args); err != nil {
		t.Fatal(err)
	}
	for _, command := range []string{"sync-skills", "skills-status"} {
		if err := run([]string{command, "--data", data}); err != nil {
			t.Fatalf("global %s failed: %v", command, err)
		}
		if err := run([]string{command, "--data", data, "--endpoint", "workbuddy"}); err != nil {
			t.Fatal(err)
		}
	}
	metadata, err := os.ReadFile(filepath.Join(skills, "agi-hub-client", "installation.json"))
	var installed struct {
		BinaryPath    string `json:"binary_path"`
		HubBinaryPath string `json:"hub_binary_path"`
	}
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(metadata, &installed); err != nil || installed.BinaryPath != report || installed.HubBinaryPath != hub {
		t.Fatal("report and management paths were not separately preserved", err)
	}
	path := filepath.Join(skills, "agi-hub-client", "SKILL.md")
	modified := []byte("user modified instructions\n")
	if err := os.WriteFile(path, modified, 0600); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"sync-skills", "--data", data}); err == nil {
		t.Fatal("CLI reported sync success despite a conflict")
	}
	content, _ := os.ReadFile(path)
	if string(content) != string(modified) {
		t.Fatal("conflict was overwritten")
	}
	states, err := install.Status(data)
	if err != nil || len(states) != 1 || states[0].HostLoadState != "unknown" {
		t.Fatal("file updates were confused with native loading", err)
	}
}

func TestSkillSyncRefusesRunningHubWithDifferentRules(t *testing.T) {
	_, version, err := install.CurrentSkill()
	if err != nil {
		t.Fatal(err)
	}
	for _, match := range []bool{false, true} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/v1/skill-release" || r.Header.Get("Authorization") != "Bearer local-test-only" {
				t.Error("wrong release check route or credentials")
			}
			digest := version
			if !match {
				digest = "sha256:different-release"
			}
			_ = json.NewEncoder(w).Encode(app.SkillRules{ProtocolVersion: 1, Revision: install.SkillRevision, Version: digest})
		}))
		err := checkSkillSource(app.Config{Listen: strings.TrimPrefix(server.URL, "http://"), AdminToken: "local-test-only"})
		server.Close()
		if (err == nil) != match {
			t.Fatalf("source agreement %v produced error %v", match, err)
		}
	}
}

func TestCLIIdempotentRetryKeepsOriginalDeadline(t *testing.T) {
	dir := t.TempDir()
	b, err := manager.Open(dir, "test-device", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	for _, id := range []string{"origin", "worker"} {
		if err = b.Register(manager.Endpoint{ID: id, DeviceID: "test-device", SessionID: "native-" + id, Enabled: true, IdleWakeVerified: true, Modes: []string{"queue"}, Driver: "manual"}); err != nil {
			t.Fatal(err)
		}
	}
	if err = b.SetLeader("origin"); err != nil {
		t.Fatal(err)
	}
	cfg := app.Config{Version: 1, DeviceID: "test-device", AdminToken: strings.Repeat("test", 16)}
	api := &app.Server{Broker: b, Config: cfg}
	server := httptest.NewServer(api.Handler())
	defer server.Close()
	cfg.Listen = strings.TrimPrefix(server.URL, "http://")
	api.Config = cfg
	encoded, _ := json.Marshal(cfg)
	if err = os.WriteFile(filepath.Join(dir, "config.json"), encoded, 0600); err != nil {
		t.Fatal(err)
	}
	args := []string{"submit", "--data", dir, "--id", "repeatable", "--to", "worker", "--goal", "same goal"}
	first := append(append([]string{}, args...), "--deadline", "2030-01-01T00:00:00Z")
	if err = run(first); err != nil {
		t.Fatal(err)
	}
	if err = run(args); err != nil {
		t.Fatal("same ID and content retry should keep deadline:", err)
	}
	if len(b.Snapshot().Tasks) != 1 || b.Snapshot().Tasks["repeatable"].Deadline != "2030-01-01T00:00:00Z" {
		t.Fatal("CLI retry created a new task or changed its deadline")
	}
}
