package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func fixedFixture(t *testing.T, handler http.HandlerFunc) (string, binding, string) {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	workspace := filepath.Join(root, "workspace")
	os.Mkdir(workspace, 0700)
	cfg := binding{Endpoint: "workbuddy", BaseURL: server.URL, EndpointToken: strings.Repeat("t", 64), WorkspaceRoot: workspace, DataDir: filepath.Join(root, "private")}
	raw, _ := json.Marshal(cfg)
	filename := filepath.Join(root, "binding.json")
	if err := os.WriteFile(filename, raw, 0600); err != nil {
		t.Fatal(err)
	}
	return filename, cfg, root
}

func TestOnlyFixedScopedCommandsReachHub(t *testing.T) {
	calls := 0
	filename, cfg, _ := fixedFixture(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path != "/v1/mail/send" || r.URL.Query().Get("endpoint") != "workbuddy" || r.Header.Get("Authorization") != "Bearer "+strings.Repeat("t", 64) || r.Header.Get("Origin") != "http://"+r.Host {
			t.Errorf("incorrect scoped request")
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"id":"saved"}`))
	})
	request := filepath.Join(cfg.WorkspaceRoot, "request.json")
	os.WriteFile(request, []byte(`{"id":"saved","recipient":"codex","kind":"request","content":"One authorized check","origin_ref":"human-request"}`), 0600)
	var out bytes.Buffer
	if err := run(filename, []string{"mail-send", "--endpoint", "workbuddy", "--data", cfg.DataDir, "--file", request}, bytes.NewReader(nil), &out); err != nil {
		t.Fatal(err)
	}
	if calls != 1 || strings.Contains(out.String(), cfg.EndpointToken) {
		t.Fatal("incorrect call count or credential disclosure")
	}
	for _, args := range [][]string{{"serve"}, {"session", "--state", "idle"}, {"mail-send", "--endpoint", "codex", "--file", request}, {"mail-send", "--data", filepath.Dir(cfg.DataDir), "--file", request}, {"mail-send", "--file", "-"}, {"peers", "--config", filename}, {"mail-state"}} {
		if err := run(filename, args, bytes.NewReader(nil), &out); err == nil {
			t.Errorf("invalid command accepted: %v", args)
		}
	}
	if calls != 1 {
		t.Fatal("rejected command reached Hub")
	}
	for _, args := range [][]string{{"peer-send", "--file", request}, {"peer-state", "--correlation-id", "old"}, {"mail-send", "--peer", "codex", "--file", request}} {
		if err := run(filename, args, bytes.NewReader(nil), &out); err == nil {
			t.Errorf("obsolete or guessed command accepted: %v", args)
		}
	}
}

func TestMessagesChooseRecipientWithoutTaskWorkflow(t *testing.T) {
	received := []map[string]any{}
	filename, cfg, _ := fixedFixture(t, func(w http.ResponseWriter, r *http.Request) {
		var value map[string]any
		if err := json.NewDecoder(r.Body).Decode(&value); err != nil {
			t.Error(err)
		}
		received = append(received, value)
		w.Write([]byte(`{"message_id":"saved"}`))
	})
	var out bytes.Buffer
	for index, body := range []string{`{"id":"m1","recipient":"codex","kind":"reply","content":"One checked value","correlation_id":"conversation","reply_to":"received-message"}`, `{"id":"m2","recipient":"another-approved-endpoint","kind":"status","content":"Checking files","correlation_id":"conversation"}`} {
		file := filepath.Join(cfg.WorkspaceRoot, string(rune('a'+index))+".json")
		os.WriteFile(file, []byte(body), 0600)
		if err := run(filename, []string{"mail-send", "--file", file}, bytes.NewReader(nil), &out); err != nil {
			t.Fatal(err)
		}
	}
	if len(received) != 2 || received[0]["recipient"] != "codex" || received[1]["recipient"] != "another-approved-endpoint" {
		t.Fatal("helper selected recipients instead of retaining message data")
	}
	file := filepath.Join(cfg.WorkspaceRoot, "old-workflow.json")
	os.WriteFile(file, []byte(`{"id":"old","recipient":"codex","kind":"request","content":"Check","parent_task_id":"parent"}`), 0600)
	if err := run(filename, []string{"mail-send", "--file", file}, bytes.NewReader(nil), &out); err == nil {
		t.Fatal("old workflow field accepted")
	}
	if len(received) != 2 {
		t.Fatal("rejected workflow request reached Hub")
	}
}

func TestMailValidationExplainsJSONRepairWithoutInventingIDOrFlags(t *testing.T) {
	cases := []struct{ body, code string }{
		{`{"recipient":"codex","kind":"request","content":"check"}`, "mail_id_required"},
		{`{"id":" ","recipient":"codex","kind":"request","content":"check"}`, "mail_id_required"},
		{`{"id":"stable","recipient":"codex","kind":"request","content":"check","goal":"obsolete"}`, "unsupported_mail_fields"},
		{`{"id":"stable","kind":"request","content":"check"}`, "mail_recipient_required"},
		{`{"id":"stable","recipient":"codex","kind":"task","content":"check"}`, "mail_kind_invalid"},
		{`{"id":"stable","recipient":"codex","kind":"request"}`, "mail_content_required"},
		{`{"id":1,"recipient":"codex","kind":"request","content":"check"}`, "mail_field_type_invalid"},
		{`[]`, "mail_json_object_required"},
	}
	for _, item := range cases {
		err := validateMail([]byte(item.body))
		if err == nil || !strings.Contains(err.Error(), item.code) || !strings.Contains(err.Error(), `"id":"saved-stable-id"`) || !strings.Contains(err.Error(), "optional fields: correlation_id, origin_ref, reply_to, deadline, max_messages") {
			t.Errorf("unhelpful validation error for %s: %v", item.body, err)
		}
	}
	if err := validateMail([]byte(`{"id":"saved","recipient":"codex","kind":"reply","content":"checked","correlation_id":"known","reply_to":"prior-message"}`)); err != nil {
		t.Fatal(err)
	}
}

func TestWorkspaceOutsideFileAndSymlinkRejected(t *testing.T) {
	calls := 0
	filename, cfg, root := fixedFixture(t, func(w http.ResponseWriter, r *http.Request) { calls++; w.Write([]byte(`{"ok":true}`)) })
	outside := filepath.Join(root, "outside.json")
	os.WriteFile(outside, []byte(`{"event_id":"e","task_id":"t","endpoint":"workbuddy","state":"accepted","run_id":"r"}`), 0600)
	var out bytes.Buffer
	if err := run(filename, []string{"report", "--file", outside}, bytes.NewReader(nil), &out); err == nil {
		t.Fatal("outside file accepted")
	}
	if runtime.GOOS != "windows" {
		link := filepath.Join(cfg.WorkspaceRoot, "link.json")
		if err := os.Symlink(outside, link); err != nil {
			t.Fatal(err)
		}
		if err := run(filename, []string{"report", "--file", link}, bytes.NewReader(nil), &out); err == nil {
			t.Fatal("symlink accepted")
		}
	}
	if calls != 0 {
		t.Fatal("outside file triggered Hub call")
	}
}

func TestNoRedirectOrEnvironmentProxyAndReportEndpointChecked(t *testing.T) {
	destinationCalls := 0
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { destinationCalls++; w.Write([]byte(`{}`)) }))
	defer destination.Close()
	filename, cfg, _ := fixedFixture(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, destination.URL, http.StatusTemporaryRedirect)
	})
	var out bytes.Buffer
	t.Setenv("HTTP_PROXY", "http://127.0.0.1:1")
	if err := run(filename, []string{"peers"}, bytes.NewReader(nil), &out); err == nil {
		t.Fatal("redirect accepted")
	}
	if destinationCalls != 0 {
		t.Fatal("redirect followed")
	}
	badReport := []byte(`{"event_id":"e","task_id":"t","endpoint":"codex","state":"accepted","run_id":"r"}`)
	if err := run(filename, []string{"report", "--endpoint", cfg.Endpoint}, bytes.NewReader(badReport), &out); err == nil {
		t.Fatal("report spoofing accepted")
	}
}

func TestBindingRejectsRemoteURLAndAdminLikeFlags(t *testing.T) {
	filename, cfg, _ := fixedFixture(t, func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(`{}`)) })
	for _, base := range []string{"https://127.0.0.1:7331", "http://localhost:7331", "http://example.com:7331", "http://127.0.0.1:7331/path", "http://user:password@127.0.0.1:7331", "http://127.0.0.1:7331?target=other"} {
		cfg.BaseURL = base
		raw, _ := json.Marshal(cfg)
		os.WriteFile(filename, raw, 0600)
		if err := run(filename, []string{"peers"}, bytes.NewReader(nil), &bytes.Buffer{}); err == nil {
			t.Errorf("invalid URL accepted: %s", base)
		}
	}
}
