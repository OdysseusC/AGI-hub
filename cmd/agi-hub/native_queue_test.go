package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"agihub/internal/app"
)

func TestNativeQueueCLIUsesExplicitAdminDecisionAndNativeBinding(t *testing.T) {
	dir := t.TempDir()
	if err := app.InitConfig(dir); err != nil {
		t.Fatal(err)
	}
	cfg, err := app.LoadConfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	decisions := make(chan bool, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/manage/native-queue" || r.Header.Get("Authorization") != "Bearer "+cfg.AdminToken {
			t.Error("CLI did not use the local administrative native queue route")
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		var body struct {
			Endpoint string `json:"endpoint"`
			Session  string `json:"native_session_id"`
			Verified *bool  `json:"verified"`
			Evidence string `json:"evidence_ref"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Endpoint != "grok" || body.Session != "routine-label" || body.Evidence != "opaque-probe-ref" || body.Verified == nil {
			t.Error("CLI omitted the explicit decision, evidence or binding guard")
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		decisions <- *body.Verified
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()
	cfg.Listen = strings.TrimPrefix(server.URL, "http://")
	body, _ := json.Marshal(cfg)
	if err := os.WriteFile(filepath.Join(dir, "config.json"), body, 0600); err != nil {
		t.Fatal(err)
	}
	base := []string{"native-queue", "--data", dir, "--endpoint", "grok", "--native-session", "routine-label", "--evidence-ref", "opaque-probe-ref"}
	for _, decision := range []string{"true", "false"} {
		if err := run(append(append([]string{}, base...), "--verified="+decision)); err != nil {
			t.Fatal(err)
		}
	}
	if len(decisions) != 2 || !<-decisions || <-decisions {
		t.Fatal("CLI did not grant then revoke")
	}
	if err := run(base); err == nil {
		t.Fatal("CLI accepted a missing explicit verified decision")
	}
	if len(decisions) != 0 {
		t.Fatal("invalid CLI request reached the administrative API")
	}
}
