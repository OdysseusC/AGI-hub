package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"agihub/internal/install"
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

func TestManagementInstallPreservesRegisteredScopedHelperAndProfile(t *testing.T) {
	for _, profile := range []string{"default", "custom"} {
		t.Run(profile, func(t *testing.T) {
			s := networkFixture(t, "managed-install")
			home, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			t.Setenv("HOME", home)
			t.Setenv("USERPROFILE", home)
			opts := install.Options{Host: "workbuddy", Endpoint: "workbuddy", DataDir: s.management.dir,
				BinaryPath: filepath.Join(home, "endpoint-scoped-helper"), HubBinaryPath: filepath.Join(home, "maintenance-hub"),
				SkillsDir: filepath.Join(home, ".workbuddy", "skills")}
			if profile == "custom" {
				opts.SkillsDir = filepath.Join(home, "selected-profile", "skills")
			}
			folder, err := install.Skill(opts)
			if err != nil {
				t.Fatal(err)
			}
			metadataPath := filepath.Join(folder, "installation.json")
			before, err := os.ReadFile(metadataPath)
			if err != nil {
				t.Fatal(err)
			}
			w := managementCall(t, s, "/v1/manage/agent", `{"endpoint":"workbuddy","action":"install"}`, s.Config.AdminToken)
			if w.Code != 200 {
				t.Fatal(w.Code, w.Body.String())
			}
			after, err := os.ReadFile(metadataPath)
			if err != nil || string(after) != string(before) {
				t.Fatalf("UI install replaced the registered helper or profile: %s, %v", after, err)
			}
			if profile == "custom" {
				if _, err := os.Stat(filepath.Join(home, ".workbuddy")); !os.IsNotExist(err) {
					t.Fatal("UI install created an unselected default profile", err)
				}
			}
			var result struct {
				Complete bool                 `json:"skill_sync_complete"`
				Sync     []install.SyncResult `json:"skill_sync"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil || !result.Complete || len(result.Sync) != 1 || result.Sync[0].SkillPath != folder {
				t.Fatal("registered installation was not synchronized", w.Body.String(), err)
			}
			for _, action := range []string{"edited", "uninstalled"} {
				if action == "edited" {
					if err := os.WriteFile(filepath.Join(folder, "SKILL.md"), []byte("user-edited instruction"), 0600); err != nil {
						t.Fatal(err)
					}
				} else if err := os.RemoveAll(folder); err != nil {
					t.Fatal(err)
				}
				w = managementCall(t, s, "/v1/manage/agent", `{"endpoint":"workbuddy","action":"install"}`, s.Config.AdminToken)
				if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &result) != nil || result.Complete || len(result.Sync) != 1 {
					t.Fatal("UI hid incomplete synchronization", action, w.Code, w.Body.String())
				}
				if action == "edited" {
					body, err := os.ReadFile(filepath.Join(folder, "SKILL.md"))
					if err != nil || string(body) != "user-edited instruction" || result.Sync[0].State != "conflict" {
						t.Fatal("UI overwrote a modified instruction", string(body), err, w.Body.String())
					}
				} else if _, err := os.Stat(folder); !os.IsNotExist(err) || result.Sync[0].State != "missing" {
					t.Fatal("UI recreated an uninstalled skill", err, w.Body.String())
				}
			}
		})
	}
}

func TestManagementEnablesOnlyImplementedMailboxProtocols(t *testing.T) {
	for _, test := range []struct {
		id, driver string
		want       int
	}{
		{"codex", "codex", 1}, {"workbuddy", "workbuddy", 1}, {"claude", "claude-code", 1},
		{"grok", "grok-routine", 0}, {"codex", "custom-driver", 0},
	} {
		t.Run(test.driver, func(t *testing.T) {
			s := networkFixture(t, "protocol-"+test.driver)
			cfg, err := LoadConfig(s.management.dir)
			if err != nil {
				t.Fatal(err)
			}
			for i := range cfg.Bindings {
				if cfg.Bindings[i].Endpoint.ID != test.id {
					continue
				}
				e := &cfg.Bindings[i].Endpoint
				e.Driver, e.MailboxProtocolVersion = test.driver, 0
				if err := s.Broker.Register(*e); err != nil {
					t.Fatal(err)
				}
			}
			if err := writePrivateJSON(filepath.Join(s.management.dir, "config.json"), cfg); err != nil {
				t.Fatal(err)
			}
			body, _ := json.Marshal(map[string]string{"endpoint": test.id, "action": "enable", "session_id": "native-protocol-probe"})
			w := managementCall(t, s, "/v1/manage/agent", string(body), s.Config.AdminToken)
			if w.Code != 200 {
				t.Fatal(w.Code, w.Body.String())
			}
			cfg, err = LoadConfig(s.management.dir)
			if err != nil {
				t.Fatal(err)
			}
			for _, binding := range cfg.Bindings {
				if binding.Endpoint.ID == test.id && binding.Endpoint.MailboxProtocolVersion != test.want {
					t.Fatal("unsupported protocol persisted", binding.Endpoint)
				}
			}
			if e := s.Broker.Snapshot().Endpoints[test.id]; !e.Enabled || e.MailboxProtocolVersion != test.want {
				t.Fatal("wrong active protocol", e)
			}
		})
	}
}

func TestManagementBoundSessionDoesNotClaimClientOfflineOrConnected(t *testing.T) {
	s := networkFixture(t, "bound-without-lease")
	w := managementCall(t, s, "/v1/manage", "", s.Config.AdminToken)
	var result struct {
		Agents []struct {
			ID     string `json:"id"`
			State  string `json:"state"`
			Detail string `json:"detail"`
		} `json:"agents"`
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &result) != nil {
		t.Fatal(w.Code, w.Body.String())
	}
	for _, agent := range result.Agents {
		if agent.ID == "codex" {
			if agent.State != "bound" || !strings.Contains(agent.Detail, "连接状态尚未验证") {
				t.Fatal("a binding without lease evidence was treated as a connection observation", agent)
			}
			return
		}
	}
	t.Fatal("configured endpoint missing from management view")
}
