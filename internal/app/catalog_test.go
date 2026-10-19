package app

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"agihub/internal/manager"
)

func catalogFixture(t *testing.T) *apiFixture {
	f := newAPIFixture(t)
	state := f.server.Broker.Snapshot()
	f.server.Config.Bindings = []Binding{
		{Endpoint: state.Endpoints["origin"], VisiblePeers: []string{"worker"}},
		{Endpoint: state.Endpoints["worker"], Purpose: "Review the supplied files"},
		{Endpoint: state.Endpoints["other"], Purpose: "private-unlisted-purpose"},
	}
	return f
}

func readCatalog(t *testing.T, f *apiFixture, endpoint string) Catalog {
	t.Helper()
	w := f.request(http.MethodGet, "/v1/catalog?endpoint="+endpoint, f.clientToken(endpoint), "")
	checkStatus(t, w, http.StatusOK)
	var catalog Catalog
	if err := json.Unmarshal(w.Body.Bytes(), &catalog); err != nil {
		t.Fatal(err)
	}
	return catalog
}

func TestCatalogIsExplicitlyScopedAndDoesNotExposeNativeAddresses(t *testing.T) {
	f := catalogFixture(t)
	w := f.request(http.MethodGet, "/v1/catalog?endpoint=origin", f.clientToken("origin"), "")
	checkStatus(t, w, http.StatusOK)
	for _, private := range []string{"native-", "session_id", "workspace", "private-unlisted-purpose", f.adminToken(), "endpoint_token"} {
		if strings.Contains(w.Body.String(), private) {
			t.Fatalf("directory exposed private state: %s", private)
		}
	}
	catalog := readCatalog(t, f, "origin")
	if len(catalog.Peers) != 1 || catalog.Peers[0].ID != "worker" || catalog.Peers[0].Purpose != "Review the supplied files" {
		t.Fatalf("unexpected visible peers: %+v", catalog)
	}
	if len(readCatalog(t, f, "worker").Peers) != 0 {
		t.Fatal("visibility was made reciprocal or defaulted to all members")
	}
	checkStatus(t, f.request(http.MethodGet, "/v1/catalog?endpoint=other", f.clientToken("origin"), ""), http.StatusUnauthorized)
	checkStatus(t, f.request(http.MethodGet, "/v1/state", f.clientToken("origin"), ""), http.StatusUnauthorized)
	checkStatus(t, f.request(http.MethodPost, "/v1/tasks", f.clientToken("origin"), "{}"), http.StatusUnauthorized)
}

func TestCatalogRevisionTracksMembershipButNotChatActivity(t *testing.T) {
	f := catalogFixture(t)
	before := readCatalog(t, f, "origin")
	if err := f.server.Broker.SetSession("worker", "busy", "private-native-run"); err != nil {
		t.Fatal(err)
	}
	if readCatalog(t, f, "origin").Version != before.Version {
		t.Fatal("chat activity invalidated the directory revision")
	}
	worker := f.server.Broker.Snapshot().Endpoints["worker"]
	worker.Enabled = false
	changes, cancel := f.server.Broker.Subscribe()
	defer cancel()
	if err := f.server.Broker.Register(worker); err != nil {
		t.Fatal(err)
	}
	select {
	case <-changes:
	default:
		t.Fatal("membership change did not notify the existing event subscribers")
	}
	after := readCatalog(t, f, "origin")
	if after.Version == before.Version || len(after.Peers) != 0 {
		t.Fatal("disabled member remained visible or version did not change")
	}
	checkStatus(t, f.request(http.MethodGet, "/v1/catalog?endpoint=worker", f.clientToken("worker"), ""), http.StatusUnprocessableEntity)
	checkStatus(t, f.request(http.MethodGet, "/v1/skills?endpoint=worker", f.clientToken("worker"), ""), http.StatusUnprocessableEntity)
}

func TestSkillRulesOnlyTransmitBodyOnDemandAndNeverWake(t *testing.T) {
	f := catalogFixture(t)
	before := f.server.Broker.Snapshot()
	read := func(path string) SkillRules {
		w := f.request(http.MethodGet, path, f.clientToken("origin"), "")
		checkStatus(t, w, http.StatusOK)
		var rules SkillRules
		if err := json.Unmarshal(w.Body.Bytes(), &rules); err != nil {
			t.Fatal(err)
		}
		return rules
	}
	metadata := read("/v1/skills?endpoint=origin")
	if metadata.ProtocolVersion != 1 || !strings.HasPrefix(metadata.Version, "sha256:") || metadata.Content != "" {
		t.Fatalf("metadata request returned full rules or invalid version: %+v", metadata)
	}
	body := read("/v1/skills?endpoint=origin&content=1")
	if body.Version != metadata.Version || !strings.Contains(body.Content, "name: agi-hub-client") {
		t.Fatal("body and metadata version disagree")
	}
	unchanged := read("/v1/skills?endpoint=origin&content=1&known_version=" + body.Version)
	if unchanged.Content != "" || unchanged.Version != body.Version {
		t.Fatal("unchanged rules were copied again")
	}
	after := f.server.Broker.Snapshot()
	if len(after.Tasks) != len(before.Tasks) || len(after.Messages) != len(before.Messages) || after.Sessions["origin"] != before.Sessions["origin"] {
		t.Fatal("reading metadata caused a wake or task mutation")
	}
	checkStatus(t, f.request(http.MethodGet, "/v1/skills?endpoint=worker&content=1", f.clientToken("origin"), ""), http.StatusUnauthorized)
}

func TestDirectoryConfigurationRequiresExplicitKnownPeers(t *testing.T) {
	base := clientConfigFixture()
	base.Bindings = append(base.Bindings, Binding{Endpoint: manager.Endpoint{ID: "peer", DeviceID: "local", Driver: "manual"}})
	for _, peers := range [][]string{{"unknown"}, {base.Bindings[0].Endpoint.ID}, {"peer", "peer"}} {
		cfg := base
		cfg.Bindings = append([]Binding{}, base.Bindings...)
		cfg.Bindings[0].VisiblePeers = peers
		if _, err := LoadConfig(writeConfigFixture(t, cfg)); err == nil {
			t.Fatalf("invalid visibility accepted: %v", peers)
		}
	}
	base.Bindings[0].VisiblePeers = []string{"peer"}
	if _, err := LoadConfig(writeConfigFixture(t, base)); err != nil {
		t.Fatal("valid explicit visibility refused:", err)
	}
	base.Bindings[1].Purpose = "multiline\ninstruction"
	if _, err := LoadConfig(writeConfigFixture(t, base)); err == nil {
		t.Fatal("multiline purpose accepted")
	}
}
