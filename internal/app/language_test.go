package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"agihub/internal/install"
	"agihub/internal/manager"
)

func TestLanguageManagementPersistsChineseWithoutChangingNativeBinding(t *testing.T) {
	s := networkFixture(t, "language-settings")
	before := s.Broker.Snapshot().Endpoints["claude"]
	w := managementCall(t, s, "/v1/manage", "", s.Config.AdminToken)
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(w.Body.Bytes(), &raw); err != nil {
		t.Fatal(err)
	}
	var agents []map[string]any
	if err := json.Unmarshal(raw["agents"], &agents); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, agent := range agents {
		if agent["id"] == "claude" {
			found = agent["communication_language"] == "en"
		}
	}
	if !found {
		t.Fatal("disabled Claude must show its default language", w.Body.String())
	}
	save := func(body, token string) int {
		t.Helper()
		return managementCall(t, s, "/v1/manage/agent", body, token).Code
	}
	body := `{"endpoint":"claude","action":"language","communication_language":"zh-hans"}`
	if save(body, EndpointToken(s.Config.AdminToken, "codex")) != 401 {
		t.Fatal("client changed another endpoint's language")
	}
	if save(body, s.Config.AdminToken) != 200 {
		t.Fatal("explicit Chinese must be selectable")
	}
	cfg, err := LoadConfig(s.management.dir)
	if err != nil {
		t.Fatal(err)
	}
	after := s.Broker.Snapshot().Endpoints["claude"]
	if after.CommunicationLanguage != "zh-Hans" || after.SessionID != before.SessionID || after.Enabled != before.Enabled || after.IdleWakeVerified != before.IdleWakeVerified {
		t.Fatal("language save changed binding or failed", after)
	}
	persisted := false
	for _, binding := range cfg.Bindings {
		persisted = persisted || binding.Endpoint.ID == "claude" && binding.Endpoint.CommunicationLanguage == "zh-Hans"
	}
	if !persisted {
		t.Fatal("setting did not survive configuration reload")
	}
	for _, invalid := range []string{
		`{"endpoint":"claude","action":"language","communication_language":"English please"}`,
		`{"endpoint":"claude","action":"language","session_id":"replace-session","communication_language":"en"}`,
	} {
		if save(invalid, s.Config.AdminToken) == 200 {
			t.Fatal("accepted invalid language action", invalid)
		}
	}
	cfg.Bindings[0].Endpoint.CommunicationLanguage = "en\ninstructions"
	if err := writePrivateJSON(filepath.Join(s.management.dir, "config.json"), cfg); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadConfig(s.management.dir); err == nil {
		t.Fatal("invalid saved preference accepted")
	}
}

func TestLanguageSaveSynchronizesOtherManagedSkillsAndReportsConflict(t *testing.T) {
	s := networkFixture(t, "language-skill")
	cfg, err := LoadConfig(s.management.dir)
	if err != nil {
		t.Fatal(err)
	}
	for i := range cfg.Bindings {
		if cfg.Bindings[i].Endpoint.ID == "codex" {
			cfg.Bindings[i].VisiblePeers = []string{"claude"}
		}
	}
	if err := writePrivateJSON(filepath.Join(s.management.dir, "config.json"), cfg); err != nil {
		t.Fatal(err)
	}
	skillsDir := t.TempDir()
	folder, err := install.Skill(install.Options{
		Host: "codex", Endpoint: "codex", DataDir: s.management.dir,
		BinaryPath: filepath.Join(t.TempDir(), "agi-hub.exe"), SkillsDir: skillsDir,
	})
	if err != nil {
		t.Fatal(err)
	}
	policyPath := filepath.Join(folder, "communication-policy.md")
	save := func(value string) struct {
		OK       bool                 `json:"ok"`
		Complete bool                 `json:"skill_sync_complete"`
		Results  []install.SyncResult `json:"skill_sync"`
	} {
		t.Helper()
		w := managementCall(t, s, "/v1/manage/agent", `{"endpoint":"claude","action":"language","communication_language":"`+value+`"}`, s.Config.AdminToken)
		var response struct {
			OK       bool                 `json:"ok"`
			Complete bool                 `json:"skill_sync_complete"`
			Results  []install.SyncResult `json:"skill_sync"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil || w.Code != 200 {
			t.Fatal(w.Code, w.Body.String(), err)
		}
		return response
	}
	response := save("ru")
	policy, err := os.ReadFile(policyPath)
	if err != nil || !response.OK || !response.Complete || len(response.Results) != 1 || response.Results[0].State != "updated" || !strings.Contains(string(policy), `"send_language": "ru"`) {
		t.Fatal("UI save did not reach another Agent's installed policy", response, err)
	}
	if err := os.WriteFile(policyPath, []byte("user-edited policy"), 0600); err != nil {
		t.Fatal(err)
	}
	response = save("zh")
	policy, err = os.ReadFile(policyPath)
	if err != nil || !response.OK || response.Complete || len(response.Results) != 1 || response.Results[0].State != "conflict" || string(policy) != "user-edited policy" {
		t.Fatal("sync conflict was hidden or overwritten", response, err)
	}
	if s.Broker.Snapshot().Endpoints["claude"].CommunicationLanguage != "zh" {
		t.Fatal("a skill conflict incorrectly undid the saved setting")
	}
}

func TestCatalogLanguageVersionAndMessageHints(t *testing.T) {
	f := mailboxFixture(t)
	f.server.Config.Bindings[0].VisiblePeers = []string{"worker"}
	worker := f.server.Broker.Snapshot().Endpoints["worker"]
	worker.Driver = "claude-code"
	if err := f.server.Broker.Register(worker); err != nil {
		t.Fatal(err)
	}
	before, err := f.server.catalog("origin")
	if err != nil || len(before.Peers) != 1 || before.Peers[0].SendLanguage != "en" {
		t.Fatal("directory does not tell caller the required Claude language", before, err)
	}
	worker.CommunicationLanguage = "ru"
	if err := f.server.Broker.Register(worker); err != nil {
		t.Fatal(err)
	}
	after, err := f.server.catalog("origin")
	if err != nil || before.Version == after.Version || after.Peers[0].SendLanguage != "ru" {
		t.Fatal("language change did not invalidate cached directory", after, err)
	}
	w := f.request("POST", "/v1/mail/send?endpoint=origin", f.clientToken("origin"),
		`{"id":"cannot-override","recipient":"worker","kind":"request","content":"x","origin_ref":"test","communication_language":"zh"}`)
	if w.Code == 200 {
		t.Fatal("sender overrode the language policy via public mail schema")
	}
	message := publishAPI(t, f, "origin", manager.MailSubmission{ID: "language-api", Recipient: "worker", Kind: "request", Content: "Проверьте цитату: 中文", OriginRef: "human-ref"})
	if message.CommunicationLanguage != "ru" || !strings.Contains(message.Content, "中文") {
		t.Fatal("language hint missing or source material changed", message)
	}
}
