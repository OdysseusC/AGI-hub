package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestInboxWaitRejectsInvalidParametersBeforeClaim(t *testing.T) {
	f := newAPIFixture(t)
	checkStatus(t, f.request(http.MethodPost, "/v1/tasks", f.adminToken(), marshalBody(t, apiSubmission("not-claimed"))), http.StatusOK)
	for _, value := range []string{"", "0", "-1", "25001", "1.0", "+1", "1e3", "no", "999999999999999999999", "1&wait_ms=2", "%20", "%zz"} {
		checkStatus(t, f.request(http.MethodPost, "/v1/inbox?endpoint=worker&wait_ms="+value, f.clientToken("worker"), ""), http.StatusBadRequest)
	}
	if f.server.Broker.Snapshot().Messages["task:not-claimed"].Delivery != "pending" {
		t.Fatal("invalid wait claimed work")
	}
}

func TestInboxWaitReturnsExistingMessageImmediately(t *testing.T) {
	f := newAPIFixture(t)
	checkStatus(t, f.request(http.MethodPost, "/v1/tasks", f.adminToken(), marshalBody(t, apiSubmission("ready"))), http.StatusOK)
	started := time.Now()
	w := f.request(http.MethodPost, "/v1/inbox?endpoint=worker&wait_ms=25000", f.clientToken("worker"), "")
	checkStatus(t, w, http.StatusOK)
	if time.Since(started) > time.Second || !strings.Contains(w.Body.String(), `"task_id":"ready"`) {
		t.Fatal("available message did not return immediately")
	}
}

func TestInboxWaitTimesOutWithoutRedispatching(t *testing.T) {
	f := newAPIFixture(t)
	for _, queued := range []bool{false, true} {
		if queued {
			checkStatus(t, f.request(http.MethodPost, "/v1/tasks", f.adminToken(), marshalBody(t, apiSubmission("claimed"))), http.StatusOK)
			checkStatus(t, f.request(http.MethodPost, "/v1/inbox?endpoint=worker", f.clientToken("worker"), ""), http.StatusOK)
		}
		started := time.Now()
		w := f.request(http.MethodPost, "/v1/inbox?endpoint=worker&wait_ms=25", f.clientToken("worker"), "")
		checkStatus(t, w, http.StatusOK)
		if strings.TrimSpace(w.Body.String()) != "null" || time.Since(started) < 20*time.Millisecond {
			t.Fatal("empty/claimed inbox did not wait then return null")
		}
	}
}

func TestConcurrentLongPollsWakeAndOnlyOneClaims(t *testing.T) {
	f := newAPIFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	results := make(chan *httptest.ResponseRecorder, 2)
	for i := 0; i < 2; i++ {
		go func() {
			// Disk persistence can exceed 150 ms on Windows CI. Keep both
			// consumers waiting until one wins, then cancel the losing request.
			r := httptest.NewRequest(http.MethodPost, "http://"+f.server.Config.Listen+"/v1/inbox?endpoint=worker&wait_ms=25000", nil).WithContext(ctx)
			r.Header.Set("Authorization", "Bearer "+f.clientToken("worker"))
			w := httptest.NewRecorder()
			f.server.Handler().ServeHTTP(w, r)
			results <- w
		}()
	}
	select {
	case <-results:
		t.Fatal("empty long poll returned before submission")
	case <-time.After(20 * time.Millisecond):
	}
	checkStatus(t, f.request(http.MethodPost, "/v1/tasks", f.adminToken(), marshalBody(t, apiSubmission("one-claim"))), http.StatusOK)
	claimed := 0
	for i := 0; i < 2; i++ {
		select {
		case w := <-results:
			checkStatus(t, w, http.StatusOK)
			if strings.Contains(w.Body.String(), `"task_id":"one-claim"`) {
				claimed++
				cancel()
			} else if strings.TrimSpace(w.Body.String()) != "" || ctx.Err() == nil {
				t.Fatalf("unexpected inbox: %s", w.Body.String())
			}
		case <-time.After(5 * time.Second):
			t.Fatal("long poll failed to return")
		}
	}
	if claimed != 1 {
		t.Fatalf("claimed %d times, want 1", claimed)
	}
}
