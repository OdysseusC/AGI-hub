package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"agihub/internal/manager"
)

func TestMailWaitValidatesBeforeClaimAndReturnsAvailableMail(t *testing.T) {
	f := mailboxFixture(t)
	message := publishAPI(t, f, "origin", manager.MailSubmission{ID: "mail-wait-ready", Recipient: "worker", Kind: "request", Content: "check", OriginRef: "human"})
	for _, value := range []string{"", "0", "-1", "25001", "1.0", "1&wait_ms=2", "%zz"} {
		checkStatus(t, f.request(http.MethodPost, "/v1/mail/inbox?endpoint=worker&wait_ms="+value, f.clientToken("worker"), ""), http.StatusBadRequest)
	}
	if f.server.Broker.Snapshot().Messages[message.ID].Delivery != "pending" {
		t.Fatal("invalid wait consumed mail")
	}
	w := f.request(http.MethodPost, "/v1/mail/inbox?endpoint=worker&wait_ms=25000", f.clientToken("worker"), "")
	checkStatus(t, w, http.StatusOK)
	if !strings.Contains(w.Body.String(), message.ID) {
		t.Fatal("available mail did not return immediately")
	}
}

func TestMailConcurrentWaitersClaimOnceAndCancelWithoutFurtherConsumption(t *testing.T) {
	f := mailboxFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	results := make(chan *httptest.ResponseRecorder, 2)
	for i := 0; i < 2; i++ {
		go func() {
			r := httptest.NewRequest(http.MethodPost, "http://"+f.server.Config.Listen+"/v1/mail/inbox?endpoint=worker&wait_ms=25000", nil).WithContext(ctx)
			r.Header.Set("Authorization", "Bearer "+f.clientToken("worker"))
			w := httptest.NewRecorder()
			f.server.Handler().ServeHTTP(w, r)
			results <- w
		}()
	}
	select {
	case <-results:
		t.Fatal("empty mailbox returned before work")
	case <-time.After(20 * time.Millisecond):
	}
	message := publishAPI(t, f, "origin", manager.MailSubmission{ID: "mail-wait-once", Recipient: "worker", Kind: "request", Content: "check", OriginRef: "human"})
	claims := 0
	for i := 0; i < 2; i++ {
		select {
		case w := <-results:
			checkStatus(t, w, http.StatusOK)
			if strings.Contains(w.Body.String(), message.ID) {
				claims++
				cancel()
			} else if strings.TrimSpace(w.Body.String()) != "" || ctx.Err() == nil {
				t.Fatalf("unexpected response: %s", w.Body.String())
			}
		case <-time.After(5 * time.Second):
			t.Fatal("mail long poll did not wake or cancel")
		}
	}
	if claims != 1 {
		t.Fatalf("mail claimed %d times", claims)
	}
}

func TestClaudeMailLeaseFencesClaimSendAndDelivery(t *testing.T) {
	f := mailboxFixture(t)
	useClaudeDriver(t, f, "worker")
	lease := acquireTestLease(t, f, "worker", "desktop")
	message := publishAPI(t, f, "origin", manager.MailSubmission{ID: "claude-in", Recipient: "worker", Kind: "request", Content: "check", OriginRef: "human"})
	metadata := f.request(http.MethodGet, "/v1/endpoint?endpoint=worker", f.clientToken("worker"), "")
	checkStatus(t, metadata, http.StatusOK)
	for _, capability := range []string{`"mailbox_long_poll_max_ms":25000`, `"mailbox_client_lease":true`} {
		if !strings.Contains(metadata.Body.String(), capability) {
			t.Fatal("Claude cannot negotiate mailbox safety", capability)
		}
	}
	checkStatus(t, f.request(http.MethodPost, "/v1/mail/inbox?endpoint=worker", f.clientToken("worker"), ""), http.StatusConflict)
	checkStatus(t, f.leaseRequest(http.MethodPost, "/v1/mail/inbox?endpoint=worker", "worker", lease.LeaseID, ""), http.StatusOK)
	response := manager.MailSubmission{ID: "claude-out", Recipient: "origin", Kind: "result", Content: "checked", CorrelationID: message.CorrelationID}
	expireTestLease(f, "worker")
	checkStatus(t, f.leaseRequest(http.MethodPost, "/v1/mail/send?endpoint=worker", "worker", lease.LeaseID, marshalBody(t, response)), http.StatusConflict)
	delivery := marshalBody(t, map[string]string{"message_id": message.ID, "outcome": "delivered"})
	checkStatus(t, f.leaseRequest(http.MethodPost, "/v1/mail/delivery?endpoint=worker", "worker", lease.LeaseID, delivery), http.StatusConflict)
	state := f.server.Broker.Snapshot()
	if state.Messages[message.ID].Delivery != "dispatching" || len(state.Messages) != 1 {
		t.Fatal("expired owner mutated mailbox")
	}
	current := acquireTestLease(t, f, "worker", "reloaded")
	checkStatus(t, f.leaseRequest(http.MethodPost, "/v1/mail/delivery?endpoint=worker", "worker", current.LeaseID, delivery), http.StatusOK)
	checkStatus(t, f.leaseRequest(http.MethodPost, "/v1/mail/send?endpoint=worker", "worker", current.LeaseID, marshalBody(t, response)), http.StatusOK)
	if len(f.server.Broker.Snapshot().Tasks) != 0 {
		t.Fatal("mail created a legacy task")
	}
}

func TestMailWaitRechecksLeaseAfterEventAndAtTimeout(t *testing.T) {
	for _, publish := range []bool{false, true} {
		f := mailboxFixture(t)
		useClaudeDriver(t, f, "worker")
		lease := acquireTestLease(t, f, "worker", "old")
		wait := "100"
		if publish {
			wait = "25000"
		}
		done := make(chan *httptest.ResponseRecorder, 1)
		go func() {
			done <- f.leaseRequest(http.MethodPost, "/v1/mail/inbox?endpoint=worker&wait_ms="+wait, "worker", lease.LeaseID, "")
		}()
		select {
		case <-done:
			t.Fatal("empty wait returned early")
		case <-time.After(20 * time.Millisecond):
		}
		expireTestLease(f, "worker")
		acquireTestLease(t, f, "worker", "new")
		if publish {
			publishAPI(t, f, "origin", manager.MailSubmission{ID: "after-lease-change", Recipient: "worker", Kind: "request", Content: "check", OriginRef: "human"})
		}
		select {
		case w := <-done:
			checkStatus(t, w, http.StatusConflict)
		case <-time.After(5 * time.Second):
			t.Fatal("stale waiter did not exit")
		}
		if publish && f.server.Broker.Snapshot().Messages["after-lease-change"].Delivery != "pending" {
			t.Fatal("stale lease claimed mail after waking")
		}
	}
}
