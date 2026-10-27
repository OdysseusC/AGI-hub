package app

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"agihub/internal/manager"
)

func endpointSessionRequest(f *apiFixture, endpoint, token, nativeSession, lease, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodPost, "http://"+f.server.Config.Listen+"/v1/endpoint/session?endpoint="+endpoint, strings.NewReader(body))
	r.Header.Set("Authorization", "Bearer "+token)
	r.Header.Set("X-AGI-Hub-Native-Session", nativeSession)
	r.Header.Set("X-AGI-Hub-Lease", lease)
	w := httptest.NewRecorder()
	f.server.Handler().ServeHTTP(w, r)
	return w
}

func TestEndpointSessionObservationUpdatesOnlyItsBoundSession(t *testing.T) {
	f := newAPIFixture(t)
	if err := f.server.Broker.SetSession("worker", "unknown", ""); err != nil {
		t.Fatal(err)
	}
	before := f.server.Broker.Snapshot()
	w := endpointSessionRequest(f, "worker", f.clientToken("worker"), "native-worker", "", `{"state":"busy","run_id":"actual-native-turn"}`)
	checkStatus(t, w, http.StatusOK)
	before.Sessions["worker"] = manager.Session{State: "busy", RunID: "actual-native-turn"}
	if !reflect.DeepEqual(before, f.server.Broker.Snapshot()) {
		t.Fatal("session observation changed another session, binding, verification, task or delivery")
	}
}

func TestEndpointSessionObservationRejectsCrossScopeBindingAndPayloadChanges(t *testing.T) {
	for _, test := range []struct {
		name, endpoint, tokenEndpoint, nativeSession, body string
		bindingChange                                      string
		want                                               int
	}{
		{name: "anonymous", endpoint: "worker", nativeSession: "native-worker", body: `{"state":"busy","run_id":"actual-turn"}`, want: http.StatusUnauthorized},
		{name: "other endpoint", endpoint: "other", tokenEndpoint: "worker", nativeSession: "native-other", body: `{"state":"idle"}`, want: http.StatusUnauthorized},
		{name: "missing scope", tokenEndpoint: "worker", nativeSession: "native-worker", body: `{"state":"idle"}`, want: http.StatusUnauthorized},
		{name: "missing binding", endpoint: "worker", tokenEndpoint: "worker", body: `{"state":"idle"}`, want: http.StatusBadRequest},
		{name: "old session", endpoint: "worker", tokenEndpoint: "worker", nativeSession: "native-worker", bindingChange: "rebind", body: `{"state":"idle"}`, want: http.StatusConflict},
		{name: "disabled", endpoint: "worker", tokenEndpoint: "worker", nativeSession: "native-worker", bindingChange: "disable", body: `{"state":"idle"}`, want: http.StatusConflict},
		{name: "unbound", endpoint: "worker", tokenEndpoint: "worker", nativeSession: "native-worker", bindingChange: "unbind", body: `{"state":"idle"}`, want: http.StatusConflict},
		{name: "replace endpoint", endpoint: "worker", tokenEndpoint: "worker", nativeSession: "native-worker", body: `{"state":"idle","endpoint":"other"}`, want: http.StatusBadRequest},
		{name: "replace native binding", endpoint: "worker", tokenEndpoint: "worker", nativeSession: "native-worker", body: `{"state":"idle","session_id":"different"}`, want: http.StatusBadRequest},
		{name: "set verification", endpoint: "worker", tokenEndpoint: "worker", nativeSession: "native-worker", body: `{"state":"idle","idle_wake_verified":true}`, want: http.StatusBadRequest},
		{name: "set capability", endpoint: "worker", tokenEndpoint: "worker", nativeSession: "native-worker", body: `{"state":"idle","mailbox_protocol_version":1}`, want: http.StatusBadRequest},
		{name: "trailing object", endpoint: "worker", tokenEndpoint: "worker", nativeSession: "native-worker", body: `{"state":"idle"} {}`, want: http.StatusBadRequest},
		{name: "oversized", endpoint: "worker", tokenEndpoint: "worker", nativeSession: "native-worker", body: `{"state":"idle","run_id":"` + strings.Repeat("x", 1<<20) + `"}`, want: http.StatusBadRequest},
		{name: "invalid state", endpoint: "worker", tokenEndpoint: "worker", nativeSession: "native-worker", body: `{"state":"completed"}`, want: http.StatusUnprocessableEntity},
		{name: "idle run", endpoint: "worker", tokenEndpoint: "worker", nativeSession: "native-worker", body: `{"state":"idle","run_id":"old-turn"}`, want: http.StatusUnprocessableEntity},
		{name: "busy without run", endpoint: "worker", tokenEndpoint: "worker", nativeSession: "native-worker", body: `{"state":"busy"}`, want: http.StatusUnprocessableEntity},
		{name: "needs user without run", endpoint: "worker", tokenEndpoint: "worker", nativeSession: "native-worker", body: `{"state":"needs_user"}`, want: http.StatusUnprocessableEntity},
		{name: "oversized run", endpoint: "worker", tokenEndpoint: "worker", nativeSession: "native-worker", body: `{"state":"busy","run_id":"` + strings.Repeat("x", 129) + `"}`, want: http.StatusUnprocessableEntity},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := newAPIFixture(t)
			binding := f.server.Broker.Snapshot().Endpoints["worker"]
			switch test.bindingChange {
			case "rebind":
				binding.SessionID = "new-native-session"
			case "disable":
				binding.Enabled = false
			case "unbind":
				binding.Enabled = false
				binding.SessionID = ""
			}
			if err := f.server.Broker.Register(binding); err != nil {
				t.Fatal(err)
			}
			before := f.server.Broker.Snapshot()
			changes, cancel := f.server.Broker.Subscribe()
			defer cancel()
			token := ""
			if test.tokenEndpoint != "" {
				token = f.clientToken(test.tokenEndpoint)
			}
			w := endpointSessionRequest(f, test.endpoint, token, test.nativeSession, "", test.body)
			checkStatus(t, w, test.want)
			if !reflect.DeepEqual(before, f.server.Broker.Snapshot()) {
				t.Fatal("rejected observation changed state")
			}
			select {
			case <-changes:
				t.Fatal("rejected observation notified subscribers")
			default:
			}
		})
	}
}

func TestEndpointSessionObservationHonorsLeaseAndPreservesAdminInterface(t *testing.T) {
	f := newAPIFixture(t)
	useClaudeDriver(t, f, "worker")
	first := acquireTestLease(t, f, "worker", "old-client")
	checkStatus(t, endpointSessionRequest(f, "worker", f.clientToken("worker"), "native-worker", "", `{"state":"busy","run_id":"actual-turn"}`), http.StatusConflict)
	checkStatus(t, endpointSessionRequest(f, "worker", f.clientToken("worker"), "native-worker", first.LeaseID, `{"state":"busy","run_id":"actual-turn"}`), http.StatusOK)
	expireTestLease(f, "worker")
	second := acquireTestLease(t, f, "worker", "new-client")
	checkStatus(t, endpointSessionRequest(f, "worker", f.clientToken("worker"), "native-worker", first.LeaseID, `{"state":"idle"}`), http.StatusConflict)
	checkStatus(t, endpointSessionRequest(f, "worker", f.clientToken("worker"), "native-worker", second.LeaseID, `{"state":"idle"}`), http.StatusOK)
	checkStatus(t, f.request(http.MethodPost, "/v1/session", f.clientToken("worker"), `{"endpoint":"worker","state":"busy","run_id":"actual-turn"}`), http.StatusUnauthorized)
	checkStatus(t, f.request(http.MethodPost, "/v1/session", f.adminToken(), `{"endpoint":"worker","state":"busy","run_id":"actual-turn"}`), http.StatusOK)
}

func TestEndpointSessionObservationThenRebindFencesOldInbox(t *testing.T) {
	f := mailboxFixture(t)
	checkStatus(t, endpointSessionRequest(f, "worker", f.clientToken("worker"), "native-worker", "", `{"state":"idle"}`), http.StatusOK)
	binding := f.server.Broker.Snapshot().Endpoints["worker"]
	binding.SessionID = "replacement-native-worker"
	if err := f.server.Broker.Register(binding); err != nil {
		t.Fatal(err)
	}
	checkStatus(t, endpointSessionRequest(f, "worker", f.clientToken("worker"), binding.SessionID, "", `{"state":"idle"}`), http.StatusOK)
	message := publishAPI(t, f, "origin", manager.MailSubmission{ID: "session-cas-before-claim", Recipient: "worker", Kind: "request", Content: "check", OriginRef: "human"})
	claim := func(nativeSession string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPost, "http://"+f.server.Config.Listen+"/v1/mail/inbox?endpoint=worker", nil)
		r.Header.Set("Authorization", "Bearer "+f.clientToken("worker"))
		r.Header.Set("X-AGI-Hub-Native-Session", nativeSession)
		w := httptest.NewRecorder()
		f.server.Handler().ServeHTTP(w, r)
		return w
	}
	checkStatus(t, claim("native-worker"), http.StatusConflict)
	if f.server.Broker.Snapshot().Messages[message.ID].Delivery != "pending" {
		t.Fatal("old native binding claimed mail after successful state observation and rebind")
	}
	w := claim(binding.SessionID)
	checkStatus(t, w, http.StatusOK)
	if !strings.Contains(w.Body.String(), message.ID) || f.server.Broker.Snapshot().Messages[message.ID].Delivery != "dispatching" {
		t.Fatal("current native binding could not claim its pending mail")
	}
}

func TestEndpointSessionObservationDoesNotGrantUnverifiedWake(t *testing.T) {
	f := mailboxFixture(t)
	binding := f.server.Broker.Snapshot().Endpoints["worker"]
	binding.IdleWakeVerified = false
	if err := f.server.Broker.Register(binding); err != nil {
		t.Fatal(err)
	}
	if err := f.server.Broker.SetSession("worker", "unknown", ""); err != nil {
		t.Fatal(err)
	}
	message := publishAPI(t, f, "origin", manager.MailSubmission{ID: "unverified-observed-idle", Recipient: "worker", Kind: "request", Content: "check", OriginRef: "human"})
	checkStatus(t, endpointSessionRequest(f, "worker", f.clientToken("worker"), binding.SessionID, "", `{"state":"idle"}`), http.StatusOK)
	if got := f.server.Broker.Snapshot().Endpoints["worker"]; !reflect.DeepEqual(binding, got) {
		t.Fatal("native state observation changed binding or verification")
	}
	r := httptest.NewRequest(http.MethodPost, "http://"+f.server.Config.Listen+"/v1/mail/inbox?endpoint=worker", nil)
	r.Header.Set("Authorization", "Bearer "+f.clientToken("worker"))
	r.Header.Set("X-AGI-Hub-Native-Session", binding.SessionID)
	w := httptest.NewRecorder()
	f.server.Handler().ServeHTTP(w, r)
	checkStatus(t, w, http.StatusOK)
	if strings.TrimSpace(w.Body.String()) != "null" || f.server.Broker.Snapshot().Messages[message.ID].Delivery != "pending" {
		t.Fatal("observed idle bypassed unverified native wake gate")
	}
}
