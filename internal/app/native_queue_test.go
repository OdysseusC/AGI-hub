package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"agihub/internal/manager"
	"agihub/internal/mesh"
)

func nativeQueueFixture(t *testing.T) (*Server, *apiFixture, string) {
	t.Helper()
	s := networkFixture(t, "native-queue")
	session := "approved-routine-binding"
	body := marshalBody(t, map[string]string{"endpoint": "grok", "action": "enable", "session_id": session})
	checkStatus(t, managementCall(t, s, "/v1/manage/agent", body, s.Config.AdminToken), http.StatusOK)
	return s, &apiFixture{server: s}, session
}

func nativeQueueDecisionBody(t *testing.T, session string, verified bool, evidence string) string {
	t.Helper()
	return marshalBody(t, map[string]any{"endpoint": "grok", "native_session_id": session, "verified": verified, "evidence_ref": evidence})
}

func TestNativeQueueVerificationRejectsUnauthorizedAndMalformedDecisions(t *testing.T) {
	s, f, session := nativeQueueFixture(t)
	valid := nativeQueueDecisionBody(t, session, true, "accepted-probe")
	checkStatus(t, managementCall(t, s, "/v1/manage/native-queue", valid, f.clientToken("grok")), http.StatusUnauthorized)
	if err := mesh.ValidateManagementRequest(mesh.ManagementRequest{Method: http.MethodPost, Path: "/v1/manage/native-queue", Body: json.RawMessage(valid)}); err == nil {
		t.Fatal("native queue verification was exposed through remote management")
	}
	for _, body := range []string{
		`{}`, `{"endpoint":"grok","native_session_id":"` + session + `","evidence_ref":"accepted-probe"}`,
		`{"endpoint":"grok","native_session_id":"` + session + `","verified":null,"evidence_ref":"accepted-probe"}`,
		nativeQueueDecisionBody(t, session, true, ""), nativeQueueDecisionBody(t, session, true, " multiline\nproof"),
		nativeQueueDecisionBody(t, session, true, strings.Repeat("x", 257)), nativeQueueDecisionBody(t, "", true, "accepted-probe"),
		strings.TrimSuffix(valid, "}") + `,"idle_wake_verified":true}`, valid + ` {}`,
	} {
		before := s.Broker.Snapshot()
		checkStatus(t, managementCall(t, s, "/v1/manage/native-queue", body, s.Config.AdminToken), http.StatusBadRequest)
		if !reflect.DeepEqual(before, s.Broker.Snapshot()) {
			t.Fatal("rejected decision changed state")
		}
	}
	checkStatus(t, managementCall(t, s, "/v1/manage/native-queue", nativeQueueDecisionBody(t, "old-native-session", true, "accepted-probe"), s.Config.AdminToken), http.StatusConflict)
}

func TestNativeQueueVerificationRevokeAndSameBindingToggle(t *testing.T) {
	s, _, session := nativeQueueFixture(t)
	evidence := filepath.Join(t.TempDir(), "absent-evidence-ref")
	checkStatus(t, managementCall(t, s, "/v1/manage/native-queue", nativeQueueDecisionBody(t, session, true, evidence), s.Config.AdminToken), http.StatusOK)
	if _, err := os.Stat(evidence); !os.IsNotExist(err) {
		t.Fatal("recording an evidence reference accessed or created its path")
	}
	for _, action := range []string{"disable", "enable"} {
		checkStatus(t, managementCall(t, s, "/v1/manage/agent", marshalBody(t, map[string]string{"endpoint": "grok", "action": action, "session_id": session}), s.Config.AdminToken), http.StatusOK)
		if !s.Broker.Snapshot().Endpoints["grok"].MailboxNativeQueueVerified {
			t.Fatal("same-binding toggle lost accepted native queue verification")
		}
	}
	checkStatus(t, managementCall(t, s, "/v1/manage/native-queue", nativeQueueDecisionBody(t, session, false, "explicit-revocation"), s.Config.AdminToken), http.StatusOK)
	s = reopenConfiguredRecoveryFixture(t, s)
	if s.Broker.Snapshot().Endpoints["grok"].MailboxNativeQueueVerified {
		t.Fatal("restart resurrected explicitly revoked native queue verification")
	}
	cfg, err := LoadConfig(s.management.dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, b := range cfg.Bindings {
		if b.Endpoint.ID == "grok" && (b.NativeQueueVerification == nil || b.NativeQueueVerification.EvidenceRef != "explicit-revocation") {
			t.Fatal("revocation lost its local evidence reference")
		}
	}
}

func TestNativeQueueVerificationNewBindingAndStaleConfigProofFailClosed(t *testing.T) {
	for _, field := range []string{"managed-session", "session", "driver", "device"} {
		t.Run(field, func(t *testing.T) {
			s, _, session := nativeQueueFixture(t)
			checkStatus(t, managementCall(t, s, "/v1/manage/native-queue", nativeQueueDecisionBody(t, session, true, "accepted-probe"), s.Config.AdminToken), http.StatusOK)
			if field == "managed-session" {
				checkStatus(t, managementCall(t, s, "/v1/manage/agent", `{"endpoint":"grok","action":"enable","session_id":"replacement-routine"}`, s.Config.AdminToken), http.StatusOK)
				s = reopenConfiguredRecoveryFixture(t, s)
				if s.Broker.Snapshot().Endpoints["grok"].MailboxNativeQueueVerified {
					t.Fatal("managed rebind retained native queue verification")
				}
				return
			}
			cfg, err := LoadConfig(s.management.dir)
			if err != nil {
				t.Fatal(err)
			}
			for i := range cfg.Bindings {
				b := &cfg.Bindings[i]
				if field == "device" {
					b.Endpoint.DeviceID = "replacement-device"
				}
				if b.Endpoint.ID != "grok" {
					continue
				}
				if field == "session" {
					b.Endpoint.SessionID = "replacement-routine"
				}
				if field == "driver" {
					b.Endpoint.Driver = "different-native-driver"
				}
			}
			if field == "device" {
				cfg.DeviceID = "replacement-device"
			}
			if err := writePrivateJSON(filepath.Join(s.management.dir, "config.json"), cfg); err != nil {
				t.Fatal(err)
			}
			for attempt := 0; attempt < 2; attempt++ {
				s = reopenConfiguredRecoveryFixture(t, s)
				if s.Broker.Snapshot().Endpoints["grok"].MailboxNativeQueueVerified {
					t.Fatal("stale proof resurrected native queue verification on a changed binding")
				}
			}
		})
	}
}

func TestNativeQueueVerificationSaveFailureRestoresCurrentRecord(t *testing.T) {
	s, _, session := nativeQueueFixture(t)
	path := filepath.Join(s.management.dir, "config.json")
	beforeFile, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	before := s.Broker.Snapshot()
	verified := true
	wanted := errors.New("synthetic private config write failure")
	err = s.recordNativeQueueDecision(nativeQueueDecision{"grok", session, &verified, "accepted-probe"}, func(string, any) error { return wanted })
	if !errors.Is(err, wanted) {
		t.Fatalf("missing transaction failure: %v", err)
	}
	afterFile, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(beforeFile, afterFile) || !reflect.DeepEqual(before, s.Broker.Snapshot()) {
		t.Fatal("failed verification save left a partial grant or changed config")
	}
}

func TestNativeQueueManagementSaveCannotResurrectConfigRevocation(t *testing.T) {
	s, _, session := nativeQueueFixture(t)
	checkStatus(t, managementCall(t, s, "/v1/manage/native-queue", nativeQueueDecisionBody(t, session, true, "accepted-probe"), s.Config.AdminToken), http.StatusOK)
	cfg, err := LoadConfig(s.management.dir)
	if err != nil {
		t.Fatal(err)
	}
	for i := range cfg.Bindings {
		if cfg.Bindings[i].Endpoint.ID == "grok" {
			cfg.Bindings[i].Endpoint.MailboxNativeQueueVerified = false
		}
	}
	if err := writePrivateJSON(filepath.Join(s.management.dir, "config.json"), cfg); err != nil {
		t.Fatal(err)
	}
	for _, action := range []string{"disable", "enable"} {
		checkStatus(t, managementCall(t, s, "/v1/manage/agent", marshalBody(t, map[string]string{"endpoint": "grok", "action": action, "session_id": session}), s.Config.AdminToken), http.StatusOK)
		if s.Broker.Snapshot().Endpoints["grok"].MailboxNativeQueueVerified {
			t.Fatal("management save resurrected a revoked configuration from stale runtime verification")
		}
	}
	s = reopenConfiguredRecoveryFixture(t, s)
	if s.Broker.Snapshot().Endpoints["grok"].MailboxNativeQueueVerified {
		t.Fatal("restart resurrected revoked configuration")
	}
}

func TestNativeQueueLanguageSaveCannotResurrectConfigRevocation(t *testing.T) {
	s, _, session := nativeQueueFixture(t)
	checkStatus(t, managementCall(t, s, "/v1/manage/native-queue", nativeQueueDecisionBody(t, session, true, "accepted-probe"), s.Config.AdminToken), http.StatusOK)
	cfg, err := LoadConfig(s.management.dir)
	if err != nil {
		t.Fatal(err)
	}
	for i := range cfg.Bindings {
		if cfg.Bindings[i].Endpoint.ID == "grok" {
			cfg.Bindings[i].Endpoint.MailboxNativeQueueVerified = false
		}
	}
	if err := writePrivateJSON(filepath.Join(s.management.dir, "config.json"), cfg); err != nil {
		t.Fatal(err)
	}
	checkStatus(t, managementCall(t, s, "/v1/manage/agent", `{"endpoint":"grok","action":"language","communication_language":"en"}`, s.Config.AdminToken), http.StatusOK)
	s = reopenConfiguredRecoveryFixture(t, s)
	if s.Broker.Snapshot().Endpoints["grok"].MailboxNativeQueueVerified {
		t.Fatal("language save resurrected revoked native queue verification")
	}
}

func TestNativeQueueVerificationRejectsMixedSendingModes(t *testing.T) {
	s, _, session := nativeQueueFixture(t)
	cfg, err := LoadConfig(s.management.dir)
	if err != nil {
		t.Fatal(err)
	}
	for i := range cfg.Bindings {
		if cfg.Bindings[i].Endpoint.ID == "grok" {
			cfg.Bindings[i].Endpoint.Modes = []string{"queue", "steer"}
			if err := s.Broker.Register(cfg.Bindings[i].Endpoint); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := writePrivateJSON(filepath.Join(s.management.dir, "config.json"), cfg); err != nil {
		t.Fatal(err)
	}
	before := s.Broker.Snapshot()
	checkStatus(t, managementCall(t, s, "/v1/manage/native-queue", nativeQueueDecisionBody(t, session, true, "accepted-probe"), s.Config.AdminToken), http.StatusUnprocessableEntity)
	if !reflect.DeepEqual(before, s.Broker.Snapshot()) {
		t.Fatal("rejected mixed-mode verification changed current state")
	}
}

func TestNativeQueueEligibilityStillRequiresBindingLeaseAndNoInFlight(t *testing.T) {
	s, f, session := nativeQueueFixture(t)
	checkStatus(t, endpointSessionRequest(f, "grok", f.clientToken("grok"), session, "", `{"state":"unknown","mailbox_native_queue_verified":true}`), http.StatusBadRequest)
	checkStatus(t, managementCall(t, s, "/v1/manage/native-queue", nativeQueueDecisionBody(t, session, true, "accepted-probe"), s.Config.AdminToken), http.StatusOK)
	message, err := s.Broker.Publish("codex", manager.MailSubmission{ID: "native-queue-fenced", Recipient: "grok", Kind: "request", Content: "synthetic check", OriginRef: "human", Deadline: time.Now().Add(time.Hour).UTC().Format(time.RFC3339)})
	if err != nil {
		t.Fatal(err)
	}
	first := acquireTestLease(t, f, "grok", "first")
	expireTestLease(f, "grok")
	current := acquireTestLease(t, f, "grok", "second")
	claim := func(native, lease string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPost, "http://"+s.Config.Listen+"/v1/mail/inbox?endpoint=grok", nil)
		r.Header.Set("Authorization", "Bearer "+f.clientToken("grok"))
		r.Header.Set("X-AGI-Hub-Native-Session", native)
		r.Header.Set("X-AGI-Hub-Lease", lease)
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		return w
	}
	checkStatus(t, claim("old-native-session", current.LeaseID), http.StatusConflict)
	checkStatus(t, claim(session, first.LeaseID), http.StatusConflict)
	if s.Broker.Snapshot().Messages[message.ID].Delivery != "pending" {
		t.Fatal("native queue verification bypassed binding or lease fencing")
	}
	checkStatus(t, claim(session, current.LeaseID), http.StatusOK)
	if err := s.Broker.Delivery(message.ID, "uncertain", "synthetic interrupted delivery"); err != nil {
		t.Fatal(err)
	}
	second, err := s.Broker.Publish("codex", manager.MailSubmission{ID: "native-queue-second", Recipient: "grok", Kind: "request", Content: "synthetic next", OriginRef: "human", Deadline: time.Now().Add(time.Hour).UTC().Format(time.RFC3339)})
	if err != nil {
		t.Fatal(err)
	}
	if w := claim(session, current.LeaseID); w.Code != http.StatusOK || strings.TrimSpace(w.Body.String()) != "null" || s.Broker.Snapshot().Messages[second.ID].Delivery != "pending" {
		t.Fatal("native queue eligibility cleared uncertain in-flight evidence")
	}
}

func TestNativeQueueVerificationPersistsWithoutInventingIdle(t *testing.T) {
	s, f, session := nativeQueueFixture(t)
	message, err := s.Broker.Publish("codex", manager.MailSubmission{ID: "approved-native-queue", Recipient: "grok", Kind: "request", Content: "synthetic check", OriginRef: "human", Deadline: time.Now().Add(time.Hour).UTC().Format(time.RFC3339)})
	if err != nil {
		t.Fatal(err)
	}
	before := s.Broker.Snapshot().Endpoints["grok"]
	checkStatus(t, f.request(http.MethodPost, "/v1/mail/inbox?endpoint=grok", f.clientToken("grok"), ""), http.StatusOK)
	if s.Broker.Snapshot().Messages[message.ID].Delivery != "pending" {
		t.Fatal("unverified native queue consumed mail")
	}
	body := marshalBody(t, map[string]any{"endpoint": "grok", "native_session_id": session, "verified": true, "evidence_ref": "probe:offline-queue-acceptance"})
	checkStatus(t, managementCall(t, s, "/v1/manage/native-queue", body, s.Config.AdminToken), http.StatusOK)
	s = reopenConfiguredRecoveryFixture(t, s)
	f.server = s
	e := s.Broker.Snapshot().Endpoints["grok"]
	if s.Broker.Snapshot().Sessions["grok"].State != "unknown" || e.IdleWakeVerified != before.IdleWakeVerified || e.MailboxWhileBusyVerified != before.MailboxWhileBusyVerified || e.ResultQueueWhileBusyVerified != before.ResultQueueWhileBusyVerified {
		t.Fatal("native queue verification invented idle or changed existing wake flags")
	}
	w := f.request(http.MethodGet, "/v1/endpoint?endpoint=grok", f.clientToken("grok"), "")
	checkStatus(t, w, http.StatusOK)
	var metadata struct {
		Endpoint map[string]any `json:"endpoint"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &metadata); err != nil || metadata.Endpoint["mailbox_native_queue_verified"] != true {
		t.Fatalf("restart lost explicitly recorded native queue eligibility: %v", err)
	}
	w = f.request(http.MethodPost, "/v1/mail/inbox?endpoint=grok", f.clientToken("grok"), "")
	checkStatus(t, w, http.StatusOK)
	var claimed manager.Envelope
	if err := json.Unmarshal(w.Body.Bytes(), &claimed); err != nil || claimed.ID != message.ID {
		t.Fatalf("verified native queue could not consume pending mail while native state stays unknown: %v", err)
	}
}
