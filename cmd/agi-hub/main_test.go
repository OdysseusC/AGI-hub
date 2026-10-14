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
