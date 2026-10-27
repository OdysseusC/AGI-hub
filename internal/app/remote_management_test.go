package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"agihub/internal/manager"
	"agihub/internal/mesh"
)

func remoteProxyCall(s *Server, method, path, body, token string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "http://"+s.Config.Listen+path, strings.NewReader(body))
	r.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	s.remoteManagement(w, r)
	return w
}

func TestExecuteRemoteManagementReusesOnlyFixedLocalOperations(t *testing.T) {
	s := networkFixture(t, "remote-local-execution")
	for _, path := range []string{"/v1/manage", "/v1/state"} {
		out, err := s.executeRemoteManagement(context.Background(), mesh.ManagementRequest{Method: "GET", Path: path})
		if err != nil || out.Status != http.StatusOK {
			t.Fatalf("fixed local view failed: %+v %v", out, err)
		}
		if !json.Valid(out.Body) || strings.Contains(string(out.Body), s.Config.AdminToken) || strings.Contains(string(out.Body), EndpointToken(s.Config.AdminToken, "codex")) {
			t.Fatal("management response exposed authentication material")
		}
	}
	out, err := s.executeRemoteManagement(context.Background(), mesh.ManagementRequest{Method: "POST", Path: "/v1/manage/agent", Body: json.RawMessage(`{"endpoint":"codex","action":"disable"}`)})
	if err != nil || out.Status != http.StatusOK || s.Broker.Snapshot().Endpoints["codex"].Enabled {
		t.Fatalf("bounded agent action failed: %+v %v", out, err)
	}
	for _, in := range []mesh.ManagementRequest{
		{Method: "POST", Path: "/v1/manage/network", Body: json.RawMessage(`{"action":"allow_management","peer_id":"other"}`)},
		{Method: "POST", Path: "/v1/manage/settings", Body: json.RawMessage(`{"network_enabled":false}`)},
		{Method: "GET", Path: "/v1/state?endpoint=codex"},
		{Method: "GET", Path: "/remote/another/v1/manage"},
		{Method: "POST", Path: "/v1/tasks", Body: json.RawMessage(`{}`)},
		{Method: "DELETE", Path: "/v1/manage"},
	} {
		if _, err := s.executeRemoteManagement(context.Background(), in); err == nil {
			t.Fatalf("non-whitelisted operation reached local API: %+v", in)
		}
	}
}

func TestRemoteProxyRequiresLocalAdminAndFixedPeerPath(t *testing.T) {
	f := newAPIFixture(t)
	id := strings.Repeat("a", 64)
	path := "/remote/" + id + "/v1/manage"
	for _, token := range []string{"", "wrong", f.clientToken("origin")} {
		w := remoteProxyCall(f.server, "GET", path, "", token)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("non-admin proxy status %d", w.Code)
		}
	}
	for _, path := range []string{"/remote/not-a-hub/v1/manage", "/remote/" + id + "/v1/manage?url=http://example.invalid", "/remote/" + id + "/v1/manage/../state", "/remote/" + id + "/remote/other/v1/manage"} {
		w := remoteProxyCall(f.server, "GET", path, "", f.adminToken())
		if w.Code != http.StatusForbidden && w.Code != http.StatusNotFound {
			t.Fatalf("forbidden route reached remote service: %s %d", path, w.Code)
		}
	}
	w := remoteProxyCall(f.server, "POST", "/remote/"+id+"/v1/manage/agent", strings.Repeat("x", mesh.MaxManagementBody+1), f.adminToken())
	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatal("oversized proxy body accepted")
	}
}

func TestRemoteProxyPinnedTLSGrantAndRevocation(t *testing.T) {
	a, b := networkFixture(t, "proxy-A"), networkFixture(t, "proxy-B")
	aID, bID := a.management.mesh.Snapshot().HubID, b.management.mesh.Snapshot().HubID
	invite, err := a.management.mesh.Invite(time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	request, err := b.management.mesh.RequestPair(context.Background(), invite.Code)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.management.mesh.AcceptPair(context.Background(), request.ID); err != nil {
		t.Fatal(err)
	}
	awaitNetwork(t, func() bool { return len(b.management.mesh.Snapshot().Peers) == 1 })
	path := "/remote/" + bID + "/v1/manage"
	w := remoteProxyCall(a, "GET", path, "", a.Config.AdminToken)
	if w.Code != http.StatusForbidden {
		t.Fatalf("pairing implicitly granted management: %d %s", w.Code, w.Body.String())
	}
	if err := b.management.mesh.SetManagementAllowed(aID, true); err != nil {
		t.Fatal(err)
	}
	w = remoteProxyCall(a, "GET", path, "", a.Config.AdminToken)
	if w.Code != http.StatusOK {
		t.Fatalf("granted proxy failed: %d %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "proxy-B") || strings.Contains(w.Body.String(), a.Config.AdminToken) || strings.Contains(w.Body.String(), b.Config.AdminToken) {
		t.Fatal("proxy returned the wrong Hub or exposed a token")
	}
	w = remoteProxyCall(a, "POST", "/remote/"+bID+"/v1/manage/agent", `{"endpoint":"codex","action":"disable"}`, a.Config.AdminToken)
	if w.Code != http.StatusOK || b.Broker.Snapshot().Endpoints["codex"].Enabled {
		t.Fatalf("remote agent action failed: %d %s", w.Code, w.Body.String())
	}
	if !a.Broker.Snapshot().Endpoints["codex"].Enabled {
		t.Fatal("proxy changed the local endpoint")
	}
	if err := b.management.mesh.SetManagementAllowed(aID, false); err != nil {
		t.Fatal(err)
	}
	w = remoteProxyCall(a, "GET", path, "", a.Config.AdminToken)
	if w.Code != http.StatusForbidden {
		t.Fatalf("revoked proxy remained authorized: %d", w.Code)
	}
}

func TestRemoteStatePreviewKeepsLargeHistoryManageableAndLocalStateComplete(t *testing.T) {
	a, b := networkFixture(t, "large-preview-A"), networkFixture(t, "large-preview-B")
	aID, bID := a.management.mesh.Snapshot().HubID, b.management.mesh.Snapshot().HubID
	invite, err := a.management.mesh.Invite(time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	request, err := b.management.mesh.RequestPair(context.Background(), invite.Code)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.management.mesh.AcceptPair(context.Background(), request.ID); err != nil {
		t.Fatal(err)
	}
	awaitNetwork(t, func() bool { return len(b.management.mesh.Snapshot().Peers) == 1 })
	if err := b.management.mesh.SetManagementAllowed(aID, true); err != nil {
		t.Fatal(err)
	}
	const hiddenTail = "full-local-message-tail-is-not-part-of-the-preview"
	content := strings.Repeat("预", 20000) + hiddenTail
	for i := 0; i < 36; i++ {
		_, err := b.Broker.Publish("codex", manager.MailSubmission{
			ID: fmt.Sprintf("preview-message-%02d", i), Recipient: "codex", Kind: "request", Content: content,
			OriginRef: "offline-review", Deadline: time.Now().Add(time.Hour).Format(time.RFC3339),
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	localState := func() *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodGet, "http://"+b.Config.Listen+"/v1/state", nil)
		r.Header.Set("Authorization", "Bearer "+b.Config.AdminToken)
		w := httptest.NewRecorder()
		b.Handler().ServeHTTP(w, r)
		return w
	}
	full := localState()
	if full.Code != http.StatusOK || full.Body.Len() <= 2<<20 || !strings.Contains(full.Body.String(), hiddenTail) {
		t.Fatalf("expected a legal, complete local snapshot over 2 MiB: status=%d bytes=%d", full.Code, full.Body.Len())
	}
	for _, suffix := range []string{"/v1/manage", "/v1/state"} {
		w := remoteProxyCall(a, http.MethodGet, "/remote/"+bID+suffix, "", a.Config.AdminToken)
		if w.Code != http.StatusOK || w.Body.Len() >= mesh.MaxManagementBody {
			t.Fatalf("large history blocked remote %s: status=%d bytes=%d body=%s", suffix, w.Code, w.Body.Len(), w.Body.String())
		}
		if suffix == "/v1/state" {
			var preview struct {
				RemotePreview bool                            `json:"remote_preview"`
				Truncated     bool                            `json:"truncated"`
				TotalMessages int                             `json:"total_messages"`
				Messages      map[string]remoteMailboxPreview `json:"messages"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &preview); err != nil {
				t.Fatal(err)
			}
			if !preview.RemotePreview || !preview.Truncated || preview.TotalMessages != 36 || len(preview.Messages) != 30 || preview.Messages["preview-message-35"].ID == "" || preview.Messages["preview-message-05"].ID != "" || strings.Contains(w.Body.String(), hiddenTail) {
				t.Fatalf("wrong remote preview metadata or history bounds: %+v", preview)
			}
			for _, message := range preview.Messages {
				if utf8.RuneCountInString(message.Content) != 2048 || !message.Truncated || message.Sender != "codex" || message.Recipient != "codex" || message.CorrelationID != message.ID || message.Delivery != "pending" {
					t.Fatalf("preview changed mailbox identity or rune boundary: %+v", message)
				}
			}
		}
	}
	w := remoteProxyCall(a, http.MethodPost, "/remote/"+bID+"/v1/manage/agent", `{"endpoint":"codex","action":"disable"}`, a.Config.AdminToken)
	if w.Code != http.StatusOK || b.Broker.Snapshot().Endpoints["codex"].Enabled {
		t.Fatalf("large history blocked a remote configuration change: %d %s", w.Code, w.Body.String())
	}
	for _, suffix := range []string{"/v1/manage", "/v1/state"} {
		w = remoteProxyCall(a, http.MethodGet, "/remote/"+bID+suffix, "", a.Config.AdminToken)
		if w.Code != http.StatusOK {
			t.Fatalf("post-save refresh failed for %s: %d %s", suffix, w.Code, w.Body.String())
		}
	}
	full = localState()
	if full.Code != http.StatusOK || full.Body.Len() <= 2<<20 || !strings.Contains(full.Body.String(), hiddenTail) || strings.Contains(full.Body.String(), `"remote_preview"`) {
		t.Fatal("remote preview changed or truncated the local full-state API")
	}
}

func TestRemoteStatePreviewProjectsOnlyBoundedDashboardFields(t *testing.T) {
	snapshot := manager.Snapshot{Messages: map[string]manager.Envelope{}, Tasks: map[string]manager.Task{}}
	for i := 0; i < 40; i++ {
		id := fmt.Sprintf("item-%02d", i)
		snapshot.Messages[id] = manager.Envelope{
			Mailbox: true, ID: id, Sender: "sender", Recipient: "recipient", Kind: "reply", Delivery: "delivered",
			CorrelationID: "conversation", ReplyTo: "previous", Content: strings.Repeat("信", 3000),
			Network: &manager.NetworkMail{}, Detail: "do-not-export-detail", RootOrigin: &manager.OriginContext{SessionID: "do-not-export-session"},
		}
		artifacts := make([]manager.Artifact, 8)
		for j := range artifacts {
			artifacts[j] = manager.Artifact{DeviceID: "origin-device", WorkspaceID: "origin-workspace", Path: strings.Repeat("资", 400), SHA256: strings.Repeat("a", 64)}
		}
		snapshot.Tasks[id] = manager.Task{Submission: manager.Submission{ID: id, Goal: strings.Repeat("目", 400), Context: "do-not-export-context", Recipient: "recipient", AuthorizedActions: []string{"do-not-export-actions"}}, Execution: "succeeded", Summary: strings.Repeat("结", 2000), Artifacts: artifacts}
	}
	snapshot.Messages["legacy-envelope"] = manager.Envelope{ID: "legacy-envelope", Content: "do-not-export-legacy-envelope"}
	body, err := remoteStatePreview(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	var preview struct {
		RemotePreview bool                            `json:"remote_preview"`
		Truncated     bool                            `json:"truncated"`
		TotalMessages int                             `json:"total_messages"`
		TotalTasks    int                             `json:"total_tasks"`
		Messages      map[string]remoteMailboxPreview `json:"messages"`
		Tasks         map[string]remoteTaskPreview    `json:"tasks"`
	}
	if err := json.Unmarshal(body, &preview); err != nil {
		t.Fatal(err)
	}
	if !preview.RemotePreview || !preview.Truncated || preview.TotalMessages != 40 || preview.TotalTasks != 40 || len(preview.Messages) != 30 || len(preview.Tasks) != 30 || preview.Messages["item-09"].ID != "" || preview.Tasks["item-09"].ID != "" {
		t.Fatalf("unexpected projection counts: %+v", preview)
	}
	for _, forbidden := range []string{`"network":`, `"context":`, `"root_origin":`, `"authorized_actions":`, `"sha256":`, "do-not-export-"} {
		if strings.Contains(string(body), forbidden) {
			t.Fatalf("remote preview leaked unused state %q", forbidden)
		}
	}
	message := preview.Messages["item-39"]
	if message.ID != "item-39" || message.CorrelationID != "conversation" || message.ReplyTo != "previous" || message.Sender != "sender" || message.Recipient != "recipient" || message.Kind != "reply" || message.Delivery != "delivered" || !message.Mailbox || utf8.RuneCountInString(message.Content) != 2048 {
		t.Fatalf("preview lost mailbox fields used by the dashboard: %+v", message)
	}
	item := preview.Tasks["item-39"]
	if item.ID != "item-39" || item.Recipient != "recipient" || item.Execution != "succeeded" || utf8.RuneCountInString(item.Goal) != 256 || utf8.RuneCountInString(item.Summary) != 1024 || len(item.Artifacts) != 5 {
		t.Fatalf("preview lost or exceeded task fields: %+v", item)
	}
	for _, artifact := range item.Artifacts {
		if artifact.DeviceID != "origin-device" || artifact.WorkspaceID != "origin-workspace" || utf8.RuneCountInString(artifact.Path) != 256 {
			t.Fatalf("preview lost artifact ownership or exceeded its path limit: %+v", artifact)
		}
	}
	if utf8.RuneCountInString(snapshot.Messages["item-39"].Content) != 3000 || len(snapshot.Tasks["item-39"].Artifacts) != 8 || snapshot.Tasks["item-39"].Context != "do-not-export-context" {
		t.Fatal("building a remote preview mutated the source snapshot")
	}
}

func TestRemoteStatePreviewEscapingAlwaysFitsExistingFrameBudget(t *testing.T) {
	snapshot := manager.Snapshot{Messages: map[string]manager.Envelope{}, Tasks: map[string]manager.Task{}}
	for i := 0; i < 40; i++ {
		id := fmt.Sprintf("%03d", i) + strings.Repeat("\x01", 125)
		snapshot.Messages[id] = manager.Envelope{Mailbox: true, ID: id, Sender: id, Recipient: id, CorrelationID: id, ReplyTo: id, Content: strings.Repeat("\x01", 64<<10), Kind: "reply", Delivery: "delivered"}
		artifacts := make([]manager.Artifact, 5)
		for j := range artifacts {
			artifacts[j] = manager.Artifact{DeviceID: id, WorkspaceID: id, Path: strings.Repeat("\x01", 256)}
		}
		snapshot.Tasks[id] = manager.Task{Submission: manager.Submission{ID: id, Recipient: id, Goal: strings.Repeat("\x01", 256)}, Execution: "succeeded", Summary: strings.Repeat("\x01", 1024), Artifacts: artifacts}
	}
	body, err := remoteStatePreview(snapshot)
	if err != nil || len(body) >= mesh.MaxManagementBody || !json.Valid(body) {
		t.Fatalf("escaped preview exceeds the existing body budget: %d, %v", len(body), err)
	}
	var preview struct {
		Truncated bool                       `json:"truncated"`
		Messages  map[string]json.RawMessage `json:"messages"`
		Tasks     map[string]json.RawMessage `json:"tasks"`
	}
	if err := json.Unmarshal(body, &preview); err != nil {
		t.Fatal(err)
	}
	newest := "039" + strings.Repeat("\x01", 125)
	if !preview.Truncated || len(preview.Messages)+len(preview.Tasks) >= 60 || len(preview.Messages[newest]) == 0 || len(preview.Tasks[newest]) == 0 {
		t.Fatal("byte-budget reduction did not retain the newest bounded entries")
	}
	frame, err := json.Marshal(mesh.ManagementResponse{Status: http.StatusOK, Body: body})
	if err != nil || len(frame) >= 2<<20 {
		t.Fatalf("preview does not fit the fixed management envelope: %d, %v", len(frame), err)
	}
}
