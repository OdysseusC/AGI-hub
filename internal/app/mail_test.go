package app

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"agihub/internal/manager"
)

func mailboxFixture(t *testing.T) *apiFixture {
	f := catalogFixture(t)
	for index := range f.server.Config.Bindings {
		b := &f.server.Config.Bindings[index]
		b.Endpoint.MailboxProtocolVersion = 1
		if err := f.server.Broker.Register(b.Endpoint); err != nil {
			t.Fatal(err)
		}
	}
	f.server.Config.Bindings[0].AllowedPeers = []string{"worker", "other"}
	f.server.Config.Bindings[1].AllowedPeers = []string{"other", "origin"}
	f.server.Config.Bindings[2].AllowedPeers = []string{"worker", "origin"}
	return f
}

func publishAPI(t *testing.T, f *apiFixture, sender string, in manager.MailSubmission) manager.Envelope {
	t.Helper()
	w := f.request(http.MethodPost, "/v1/mail/send?endpoint="+sender, f.clientToken(sender), marshalBody(t, in))
	checkStatus(t, w, http.StatusOK)
	var message manager.Envelope
	if err := json.Unmarshal(w.Body.Bytes(), &message); err != nil {
		t.Fatal(err)
	}
	return message
}

func receiveAPI(t *testing.T, f *apiFixture, receiver string) manager.Envelope {
	t.Helper()
	w := f.request(http.MethodPost, "/v1/mail/inbox?endpoint="+receiver, f.clientToken(receiver), "")
	checkStatus(t, w, http.StatusOK)
	var message manager.Envelope
	if err := json.Unmarshal(w.Body.Bytes(), &message); err != nil || message.ID == "" {
		t.Fatal("no mail", w.Body.String(), err)
	}
	checkStatus(t, f.request(http.MethodPost, "/v1/mail/delivery?endpoint="+receiver, f.clientToken(receiver), marshalBody(t, map[string]string{"message_id": message.ID, "outcome": "delivered", "detail": "Synthetic transport completed"})), http.StatusOK)
	return message
}

func TestMailAPIExplicitRoutingDoesNotCreateWorkflowOrForceReturnChain(t *testing.T) {
	f := mailboxFixture(t)
	root := manager.MailSubmission{ID: "mail-root", CorrelationID: "mail-root", Recipient: "worker", Kind: "request", Content: "B review the input", OriginRef: "human-input-ref"}
	first := publishAPI(t, f, "origin", root)
	if first.RootOrigin.SessionID != "native-origin" || !first.Mailbox {
		t.Fatal("missing original context")
	}
	receiveAPI(t, f, "worker")
	second := publishAPI(t, f, "worker", manager.MailSubmission{ID: "b-asks-d", Recipient: "other", Kind: "request", Content: "D check one value", CorrelationID: first.CorrelationID, ReplyTo: first.ID})
	receiveAPI(t, f, "other")
	// D explicitly chooses A, even though the request came from B.
	result := publishAPI(t, f, "other", manager.MailSubmission{ID: "d-to-a", Recipient: "origin", Kind: "result", Content: "Value verified", CorrelationID: first.CorrelationID, ReplyTo: second.ID})
	got := receiveAPI(t, f, "origin")
	if got.ID != result.ID || got.Recipient != "origin" || got.ReplyTo != second.ID {
		t.Fatal("routing was replaced by a return chain")
	}
	// A result can also be addressed elsewhere; original entry is context only.
	publishAPI(t, f, "worker", manager.MailSubmission{ID: "b-result-to-d", Recipient: "other", Kind: "result", Content: "For D to integrate", CorrelationID: first.CorrelationID})
	if receiveAPI(t, f, "other").Recipient != "other" {
		t.Fatal("result was forced to root")
	}
	publishAPI(t, f, "origin", root)
	state := f.server.Broker.Snapshot()
	if len(state.Tasks) != 0 || len(state.Messages) != 4 || state.MailConversations[first.CorrelationID].UsedMessages != 4 {
		t.Fatal("mail created a task, automatic reply, or duplicate charge")
	}
}

func TestMailAPIPinsIdentityACLReceiptsAndOwnState(t *testing.T) {
	f := mailboxFixture(t)
	f.server.Config.Bindings[0].AllowedPeers = []string{"worker"}
	in := manager.MailSubmission{ID: "restricted", Recipient: "other", Kind: "request", Content: "private-message-body", OriginRef: "human-ref"}
	checkStatus(t, f.request(http.MethodPost, "/v1/mail/send?endpoint=origin", f.clientToken("origin"), marshalBody(t, in)), http.StatusUnprocessableEntity)
	in.Recipient = "worker"
	message := publishAPI(t, f, "origin", in)
	checkStatus(t, f.request(http.MethodPost, "/v1/mail/inbox?endpoint=worker", f.clientToken("origin"), ""), http.StatusUnauthorized)
	checkStatus(t, f.request(http.MethodPost, "/v1/mail/delivery?endpoint=other", f.clientToken("other"), marshalBody(t, map[string]string{"message_id": message.ID, "outcome": "delivered"})), http.StatusUnprocessableEntity)
	w := f.request(http.MethodGet, "/v1/mail/state?endpoint=origin&correlation_id="+message.CorrelationID, f.clientToken("origin"), "")
	checkStatus(t, w, http.StatusOK)
	if strings.Contains(w.Body.String(), in.Content) || strings.Contains(w.Body.String(), f.adminToken()) {
		t.Fatal("state copied full body or secret")
	}
	publishAPI(t, f, "worker", manager.MailSubmission{ID: "status-for-origin", Recipient: "origin", Kind: "status", Content: "Needs a user decision", CorrelationID: message.CorrelationID})
	publishAPI(t, f, "worker", manager.MailSubmission{ID: "status-for-other", Recipient: "other", Kind: "status", Content: "Only other may read this", CorrelationID: message.CorrelationID})
	w = f.request(http.MethodGet, "/v1/mail/state?endpoint=origin&correlation_id="+message.CorrelationID, f.clientToken("origin"), "")
	checkStatus(t, w, http.StatusOK)
	if !strings.Contains(w.Body.String(), "Needs a user decision") || strings.Contains(w.Body.String(), "Only other may read this") {
		t.Fatal("explicit status query lost own progress or disclosed another mailbox")
	}
	checkStatus(t, f.request(http.MethodPost, "/v1/mail/cancel?endpoint=worker", f.clientToken("worker"), marshalBody(t, map[string]string{"correlation_id": message.CorrelationID})), http.StatusUnprocessableEntity)
	checkStatus(t, f.request(http.MethodPost, "/v1/mail/cancel?endpoint=origin", f.clientToken("origin"), marshalBody(t, map[string]string{"correlation_id": message.CorrelationID})), http.StatusOK)
	if !f.server.Broker.Snapshot().MailConversations[message.CorrelationID].Closed {
		t.Fatal("close was not saved")
	}
}
