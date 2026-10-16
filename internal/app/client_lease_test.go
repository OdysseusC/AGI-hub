package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"agihub/internal/manager"
)

func (f *apiFixture) leaseRequest(method, path, endpoint, leaseID, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "http://"+f.server.Config.Listen+path, strings.NewReader(body))
	r.Header.Set("Authorization", "Bearer "+f.clientToken(endpoint))
	r.Header.Set("X-AGI-Hub-Lease", leaseID)
	w := httptest.NewRecorder()
	f.server.Handler().ServeHTTP(w, r)
	return w
}

func useClaudeDriver(t *testing.T, f *apiFixture, endpoint string) {
	t.Helper()
	binding := f.server.Broker.Snapshot().Endpoints[endpoint]
	binding.Driver = "claude-code"
	if err := f.server.Broker.Register(binding); err != nil {
		t.Fatal(err)
	}
}

func acquireTestLease(t *testing.T, f *apiFixture, endpoint, owner string) leaseReceipt {
	t.Helper()
	w := f.leaseRequest(http.MethodPost, "/v1/client-lease?endpoint="+endpoint, endpoint, "", marshalBody(t, map[string]string{"owner_id": owner}))
	checkStatus(t, w, http.StatusOK)
	var receipt leaseReceipt
	if err := json.Unmarshal(w.Body.Bytes(), &receipt); err != nil {
		t.Fatal(err)
	}
	if len(receipt.LeaseID) != 64 || receipt.TTLMS != 60000 {
		t.Fatalf("invalid receipt: %+v", receipt)
	}
	expires, err := time.Parse(time.RFC3339Nano, receipt.ExpiresAt)
	if err != nil || time.Until(expires) < 55*time.Second || time.Until(expires) > clientLeaseTTL {
		t.Fatalf("invalid expiration: %+v", receipt)
	}
	return receipt
}

func expireTestLease(f *apiFixture, endpoint string) {
	f.server.leaseMu.Lock()
	defer f.server.leaseMu.Unlock()
	lease := f.server.leases[endpoint]
	lease.expiresAt = time.Now().Add(-time.Second)
	f.server.leases[endpoint] = lease
}

func TestClientLeaseOwnershipRenewalExpiryAndRelease(t *testing.T) {
	f := newAPIFixture(t)
	first := acquireTestLease(t, f, "worker", "desktop")
	again := acquireTestLease(t, f, "worker", "desktop")
	if again.LeaseID != first.LeaseID {
		t.Fatal("same owner acquired a second generation")
	}
	path := "/v1/client-lease?endpoint=worker"
	for _, body := range []string{
		`{"owner_id":"terminal"}`,
		`{"owner_id":"desktop","lease_id":"wrong"}`,
		`{"owner_id":"terminal","lease_id":"` + first.LeaseID + `","release":true}`,
	} {
		checkStatus(t, f.leaseRequest(http.MethodPost, path, "worker", "", body), http.StatusConflict)
	}
	checkStatus(t, f.leaseRequest(http.MethodPost, path, "worker", "", `{"owner_id":"desktop","lease_id":"`+first.LeaseID+`"}`), http.StatusOK)
	expireTestLease(f, "worker")
	checkStatus(t, f.leaseRequest(http.MethodPost, path, "worker", "", `{"owner_id":"desktop","lease_id":"`+first.LeaseID+`"}`), http.StatusConflict)
	second := acquireTestLease(t, f, "worker", "terminal")
	if first.LeaseID == second.LeaseID {
		t.Fatal("takeover reused the stale generation")
	}
	checkStatus(t, f.leaseRequest(http.MethodPost, path, "worker", "", `{"owner_id":"desktop","lease_id":"`+first.LeaseID+`","release":true}`), http.StatusConflict)
	checkStatus(t, f.leaseRequest(http.MethodPost, path, "worker", "", `{"owner_id":"terminal","lease_id":"`+second.LeaseID+`","release":true}`), http.StatusOK)
	third := acquireTestLease(t, f, "worker", "terminal")
	if third.LeaseID == second.LeaseID {
		t.Fatal("release did not fence the old generation")
	}
}

func TestClientLeaseRejectsMalformedAndCrossEndpointRequests(t *testing.T) {
	f := newAPIFixture(t)
	for _, body := range []string{`{}`, `{"owner_id":"bad owner"}`, `{"owner_id":"ok","release":true}`, `{"owner_id":"ok","unknown":1}`, `{"owner_id":"ok"} {}`} {
		checkStatus(t, f.leaseRequest(http.MethodPost, "/v1/client-lease?endpoint=worker", "worker", "", body), http.StatusBadRequest)
	}
	checkStatus(t, f.leaseRequest(http.MethodPost, "/v1/client-lease?endpoint=other", "worker", "", `{"owner_id":"ok"}`), http.StatusUnauthorized)
	worker := acquireTestLease(t, f, "worker", "worker-owner")
	useClaudeDriver(t, f, "other")
	checkStatus(t, f.leaseRequest(http.MethodPost, "/v1/inbox?endpoint=other", "other", worker.LeaseID, ""), http.StatusConflict)
	acquireTestLease(t, f, "other", "other-owner")
}

func TestConcurrentClientLeaseHasExactlyOneOwner(t *testing.T) {
	f := newAPIFixture(t)
	var workers sync.WaitGroup
	codes := make(chan int, 12)
	for i := 0; i < 12; i++ {
		workers.Add(1)
		go func(owner int) {
			defer workers.Done()
			body := fmt.Sprintf(`{"owner_id":"owner-%d"}`, owner)
			codes <- f.leaseRequest(http.MethodPost, "/v1/client-lease?endpoint=worker", "worker", "", body).Code
		}(i)
	}
	workers.Wait()
	close(codes)
	winners := 0
	for code := range codes {
		if code == http.StatusOK {
			winners++
		} else if code != http.StatusConflict {
			t.Fatalf("unexpected status: %d", code)
		}
	}
	if winners != 1 {
		t.Fatalf("lease winners = %d, want 1", winners)
	}
}

func TestClaudeLeaseFencesClaimDeliveryAndReport(t *testing.T) {
	f := newAPIFixture(t)
	useClaudeDriver(t, f, "worker")
	submission := apiSubmission("leased-task")
	checkStatus(t, f.request(http.MethodPost, "/v1/tasks", f.adminToken(), marshalBody(t, submission)), http.StatusOK)
	checkStatus(t, f.leaseRequest(http.MethodPost, "/v1/inbox?endpoint=worker", "worker", "", ""), http.StatusConflict)
	first := acquireTestLease(t, f, "worker", "first")
	w := f.leaseRequest(http.MethodPost, "/v1/inbox?endpoint=worker", "worker", first.LeaseID, "")
	checkStatus(t, w, http.StatusOK)
	var envelope manager.Envelope
	if err := json.Unmarshal(w.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	expireTestLease(f, "worker")
	second := acquireTestLease(t, f, "worker", "second")
	delivery := marshalBody(t, map[string]string{"message_id": envelope.ID, "outcome": "delivered"})
	report := marshalBody(t, manager.Report{EventID: "leased-report", TaskID: submission.ID, Endpoint: "worker", State: "accepted", RunID: "native-turn"})
	for _, token := range []string{"", first.LeaseID} {
		checkStatus(t, f.leaseRequest(http.MethodPost, "/v1/inbox?endpoint=worker", "worker", token, ""), http.StatusConflict)
		checkStatus(t, f.leaseRequest(http.MethodPost, "/v1/delivery?endpoint=worker", "worker", token, delivery), http.StatusConflict)
		checkStatus(t, f.leaseRequest(http.MethodPost, "/v1/report?endpoint=worker", "worker", token, report), http.StatusConflict)
	}
	// A new owner can reconcile the existing claim, never receive it as new work.
	w = f.leaseRequest(http.MethodPost, "/v1/inbox?endpoint=worker", "worker", second.LeaseID, "")
	checkStatus(t, w, http.StatusOK)
	if strings.TrimSpace(w.Body.String()) != "null" {
		t.Fatal("takeover redelivered the claim")
	}
	checkStatus(t, f.leaseRequest(http.MethodPost, "/v1/delivery?endpoint=worker", "worker", second.LeaseID, delivery), http.StatusOK)
	checkStatus(t, f.leaseRequest(http.MethodPost, "/v1/report?endpoint=worker", "worker", second.LeaseID, report), http.StatusOK)
}

func TestLeaseGenerationCannotChangeInsideMutation(t *testing.T) {
	f := newAPIFixture(t)
	useClaudeDriver(t, f, "worker")
	lease := acquireTestLease(t, f, "worker", "first")
	r := httptest.NewRequest(http.MethodPost, "/", nil)
	r.Header.Set("X-AGI-Hub-Lease", lease.LeaseID)
	entered, finish, done := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	go func() {
		done <- f.server.withClientLease(r, "worker", func() error { close(entered); <-finish; return nil })
	}()
	<-entered
	released := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		released <- f.leaseRequest(http.MethodPost, "/v1/client-lease?endpoint=worker", "worker", "", `{"owner_id":"first","lease_id":"`+lease.LeaseID+`","release":true}`)
	}()
	select {
	case <-released:
		t.Fatal("lease released inside a guarded mutation")
	case <-time.After(20 * time.Millisecond):
	}
	close(finish)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	checkStatus(t, <-released, http.StatusOK)
}

func TestOutstandingMessagesRecoverLostClaimsWithoutLeakingPeers(t *testing.T) {
	f := newAPIFixture(t)
	useClaudeDriver(t, f, "worker")
	lease := acquireTestLease(t, f, "worker", "first")
	for _, endpoint := range []string{"worker", "other"} {
		submission := apiSubmission("lost-claim-" + endpoint)
		submission.Recipient = endpoint
		checkStatus(t, f.request(http.MethodPost, "/v1/tasks", f.adminToken(), marshalBody(t, submission)), http.StatusOK)
		// Intentionally discard the response as if the transport lost it.
		leaseID := ""
		if endpoint == "worker" {
			leaseID = lease.LeaseID
		}
		checkStatus(t, f.leaseRequest(http.MethodPost, "/v1/inbox?endpoint="+endpoint, endpoint, leaseID, ""), http.StatusOK)
	}
	lostResponse := f.request(http.MethodGet, "/v1/endpoint?endpoint=worker", f.clientToken("worker"), "")
	checkStatus(t, lostResponse, http.StatusOK)
	if !strings.Contains(lostResponse.Body.String(), `"delivery":"dispatching"`) || strings.Contains(lostResponse.Body.String(), "lost-claim-other") {
		t.Fatal("lost claim response is not visible within its endpoint scope")
	}
	checkStatus(t, f.request(http.MethodPost, "/v1/delivery?endpoint=worker", f.clientToken("worker"), `{"message_id":"task:lost-claim-worker","outcome":"uncertain"}`), http.StatusConflict)
	checkStatus(t, f.leaseRequest(http.MethodPost, "/v1/delivery?endpoint=worker", "worker", lease.LeaseID, `{"message_id":"task:lost-claim-worker","outcome":"uncertain"}`), http.StatusOK)
	w := f.request(http.MethodGet, "/v1/endpoint?endpoint=worker", f.clientToken("worker"), "")
	checkStatus(t, w, http.StatusOK)
	var metadata struct {
		Outstanding []manager.Envelope `json:"outstanding"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &metadata); err != nil {
		t.Fatal(err)
	}
	if len(metadata.Outstanding) != 1 || metadata.Outstanding[0].TaskID != "lost-claim-worker" || metadata.Outstanding[0].Delivery != "uncertain" || metadata.Outstanding[0].Content == "" {
		t.Fatalf("wrong outstanding messages: %+v", metadata)
	}
	if strings.Contains(w.Body.String(), "lost-claim-other") {
		t.Fatal("peer claim leaked")
	}
	if f.server.Broker.Snapshot().Messages["task:lost-claim-worker"].Delivery != "uncertain" {
		t.Fatal("metadata read modified the claim")
	}
}

func TestLeaseIsRevalidatedAfterLongPollWake(t *testing.T) {
	f := newAPIFixture(t)
	useClaudeDriver(t, f, "worker")
	lease := acquireTestLease(t, f, "worker", "first")
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		done <- f.leaseRequest(http.MethodPost, "/v1/inbox?endpoint=worker&wait_ms=25000", "worker", lease.LeaseID, "")
	}()
	select {
	case <-done:
		t.Fatal("empty long poll returned before new work")
	case <-time.After(20 * time.Millisecond):
	}
	// Renewal/acquisition must remain available while another request waits.
	expireTestLease(f, "worker")
	acquireTestLease(t, f, "worker", "second")
	checkStatus(t, f.request(http.MethodPost, "/v1/tasks", f.adminToken(), marshalBody(t, apiSubmission("after-takeover"))), http.StatusOK)
	select {
	case w := <-done:
		checkStatus(t, w, http.StatusConflict)
	case <-time.After(time.Second):
		t.Fatal("old long poll did not stop")
	}
	if f.server.Broker.Snapshot().Messages["task:after-takeover"].Delivery != "pending" {
		t.Fatal("stale long poll claimed new work")
	}
}

func TestLongPollHonorsCancellationBeforeClaim(t *testing.T) {
	f := newAPIFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	r := httptest.NewRequest(http.MethodPost, "http://"+f.server.Config.Listen+"/v1/inbox?endpoint=worker&wait_ms=25000", nil).WithContext(ctx)
	r.Header.Set("Authorization", "Bearer "+f.clientToken("worker"))
	done := make(chan struct{})
	go func() { f.server.Handler().ServeHTTP(httptest.NewRecorder(), r); close(done) }()
	select {
	case <-done:
		t.Fatal("empty long poll returned before cancellation")
	case <-time.After(20 * time.Millisecond):
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("cancelled long poll did not exit")
	}
	checkStatus(t, f.request(http.MethodPost, "/v1/tasks", f.adminToken(), marshalBody(t, apiSubmission("after-cancel"))), http.StatusOK)
	if f.server.Broker.Snapshot().Messages["task:after-cancel"].Delivery != "pending" {
		t.Fatal("cancelled request continued consuming")
	}
}

func TestLeaseExpiryRejectsAnEmptyLongPollResponse(t *testing.T) {
	f := newAPIFixture(t)
	useClaudeDriver(t, f, "worker")
	lease := acquireTestLease(t, f, "worker", "first")
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		done <- f.leaseRequest(http.MethodPost, "/v1/inbox?endpoint=worker&wait_ms=50", "worker", lease.LeaseID, "")
	}()
	select {
	case <-done:
		t.Fatal("empty long poll returned before timeout")
	case <-time.After(20 * time.Millisecond):
	}
	expireTestLease(f, "worker")
	select {
	case w := <-done:
		checkStatus(t, w, http.StatusConflict)
	case <-time.After(time.Second):
		t.Fatal("expired long poll did not finish")
	}
}

func TestServerRestartInvalidatesLeaseWithoutRequeueingClaim(t *testing.T) {
	f := newAPIFixture(t)
	useClaudeDriver(t, f, "worker")
	lease := acquireTestLease(t, f, "worker", "before-restart")
	checkStatus(t, f.request(http.MethodPost, "/v1/tasks", f.adminToken(), marshalBody(t, apiSubmission("before-restart"))), http.StatusOK)
	checkStatus(t, f.leaseRequest(http.MethodPost, "/v1/inbox?endpoint=worker", "worker", lease.LeaseID, ""), http.StatusOK)
	f.server = &Server{Broker: f.server.Broker, Config: f.server.Config}
	checkStatus(t, f.leaseRequest(http.MethodPost, "/v1/inbox?endpoint=worker", "worker", lease.LeaseID, ""), http.StatusConflict)
	newLease := acquireTestLease(t, f, "worker", "after-restart")
	w := f.leaseRequest(http.MethodPost, "/v1/inbox?endpoint=worker", "worker", newLease.LeaseID, "")
	checkStatus(t, w, http.StatusOK)
	if strings.TrimSpace(w.Body.String()) != "null" {
		t.Fatal("server restart requeued an unresolved claim")
	}
	w = f.request(http.MethodGet, "/v1/endpoint?endpoint=worker", f.clientToken("worker"), "")
	checkStatus(t, w, http.StatusOK)
	if !strings.Contains(w.Body.String(), `"delivery":"dispatching"`) {
		t.Fatal("restart hid the unresolved claim")
	}
}

func TestRebindingInvalidatesExistingLeaseGeneration(t *testing.T) {
	f := newAPIFixture(t)
	useClaudeDriver(t, f, "worker")
	lease := acquireTestLease(t, f, "worker", "old-binding-owner")
	binding := f.server.Broker.Snapshot().Endpoints["worker"]
	binding.SessionID = "new-native-session"
	if err := f.server.Broker.Register(binding); err != nil {
		t.Fatal(err)
	}
	checkStatus(t, f.leaseRequest(http.MethodPost, "/v1/inbox?endpoint=worker", "worker", lease.LeaseID, ""), http.StatusConflict)
	checkStatus(t, f.leaseRequest(http.MethodPost, "/v1/client-lease?endpoint=worker", "worker", "", `{"owner_id":"old-binding-owner","lease_id":"`+lease.LeaseID+`"}`), http.StatusConflict)
	updated := acquireTestLease(t, f, "worker", "new-binding-owner")
	if updated.LeaseID == lease.LeaseID {
		t.Fatal("new binding reused old generation")
	}
	binding.Enabled = false
	if err := f.server.Broker.Register(binding); err != nil {
		t.Fatal(err)
	}
	checkStatus(t, f.leaseRequest(http.MethodPost, "/v1/client-lease?endpoint=worker", "worker", "", `{"owner_id":"new-binding-owner"}`), http.StatusUnprocessableEntity)
	checkStatus(t, f.leaseRequest(http.MethodPost, "/v1/inbox?endpoint=worker", "worker", updated.LeaseID, ""), http.StatusConflict)
}
