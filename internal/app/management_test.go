package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func managementCall(t *testing.T, s *Server, path, body, token string) *httptest.ResponseRecorder {
	t.Helper()
	method := http.MethodGet
	if body != "" {
		method = http.MethodPost
	}
	r := httptest.NewRequest(method, "http://"+s.Config.Listen+path, strings.NewReader(body))
	r.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	return w
}

func TestManagementDefaultsOffAndIsAdminOnly(t *testing.T) {
	f := newAPIFixture(t)
	if err := f.server.StartManagement(context.Background(), t.TempDir()); err != nil {
		t.Fatal(err)
	}
	defer f.server.CloseManagement()
	w := managementCall(t, f.server, "/v1/manage", "", f.adminToken())
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var body struct {
		Hub struct {
			ID                 string `json:"id"`
			Network, Discovery bool
		}
		Agents  []any
		Network map[string]any
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Hub.ID) != 64 || body.Hub.Network || body.Hub.Discovery {
		t.Fatal("network was not opt-in", w.Body.String())
	}
	if strings.Contains(w.Body.String(), f.adminToken()) || strings.Contains(w.Body.String(), "native-origin") {
		t.Fatal("management status leaked private binding")
	}
	w = managementCall(t, f.server, "/v1/manage", "", f.clientToken("origin"))
	if w.Code != 401 {
		t.Fatal("endpoint can administer", w.Code)
	}
	w = managementCall(t, f.server, "/v1/manage/settings", `{"name":"test","network_enabled":false,"discovery_enabled":true,"listen":"127.0.0.1:7332","interface":"invalid"}`, f.adminToken())
	if w.Code == 200 {
		t.Fatal("discovery allowed without network")
	}
}

func TestManagementRejectsArbitraryActionsAndSharesOnlyEnabledMailboxes(t *testing.T) {
	f := newAPIFixture(t)
	if err := f.server.StartManagement(context.Background(), t.TempDir()); err != nil {
		t.Fatal(err)
	}
	defer f.server.CloseManagement()
	for _, body := range []string{
		`{"endpoint":"origin","action":"shell","command":"anything"}`,
		`{"endpoint":"origin","action":"share"}`,
		`{"endpoint":"missing","action":"install"}`,
	} {
		w := managementCall(t, f.server, "/v1/manage/agent", body, f.adminToken())
		if w.Code == 200 {
			t.Fatal("accepted unsupported or unsafe action", body)
		}
	}
}

func TestDisabledWildcardSettingsCanBeSavedWithoutSelectingAnInterface(t *testing.T) {
	f := newAPIFixture(t)
	if err := f.server.StartManagement(context.Background(), t.TempDir()); err != nil {
		t.Fatal(err)
	}
	defer f.server.CloseManagement()
	w := managementCall(t, f.server, "/v1/manage/settings", `{"name":"renamed offline hub","network_enabled":false,"discovery_enabled":false,"listen":"0.0.0.0:7347","interface":""}`, f.adminToken())
	if w.Code != 200 {
		t.Fatalf("cannot save disabled default or turn LAN off: %d %s", w.Code, w.Body.String())
	}
	if f.server.management.mesh.Snapshot().Config.Enabled {
		t.Fatal("saving disabled settings enabled LAN")
	}
}
