package manager

import (
	"strings"
	"testing"
)

func TestFederatedMailRoundTripDedupBudgetAndSession(t *testing.T) {
	a, b := mailFixture(t), mailFixture(t)
	ha, hb := strings.Repeat("a", 64), strings.Repeat("b", 64)
	req := rootMail("same-client-id", hb+"/worker")
	req.MaxMessages = 4
	sent, err := a.b.PublishRemote(ha, "origin", req)
	if err != nil {
		t.Fatal(err)
	}
	if sent.Network == nil || sent.ID == req.ID || sent.Delivery != "network_pending" {
		t.Fatalf("bad outbound: %+v", sent)
	}
	dup, err := a.b.PublishRemote(ha, "origin", req)
	if err != nil || dup.ID != sent.ID {
		t.Fatalf("retry: %+v %v", dup, err)
	}
	if _, err := b.b.ImportRemote(hb, *sent.Network); err != nil {
		t.Fatal(err)
	}
	if _, err := b.b.ImportRemote(hb, *sent.Network); err != nil {
		t.Fatal(err)
	}
	if len(b.b.Snapshot().Messages) != 1 {
		t.Fatal("retry duplicated mail")
	}
	in := claimMail(t, b.b, "worker", sent.ID)
	if in.Sender != ha+"/origin" || in.Recipient != "worker" || in.RootOrigin.SessionID != "" {
		t.Fatalf("remote address or session privacy: %+v", in)
	}
	reply := MailSubmission{ID: "reply", Recipient: ha + "/origin", Kind: "result", Content: "done", CorrelationID: sent.CorrelationID, ReplyTo: sent.ID}
	result, err := b.b.PublishRemote(hb, "worker", reply)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = a.b.ImportRemote(ha, *result.Network); err != nil {
		t.Fatal(err)
	}
	if got := a.b.Snapshot().Messages[result.ID].RootOrigin.SessionID; got != "session-origin" {
		t.Fatalf("lost origin binding: %q", got)
	}
	// Each side owns a fixed share of a global conversation cap; sending never
	// requires an automatic response and concurrent sides cannot mint permits.
	for _, f := range []struct {
		b                      *Broker
		hub, sender, recipient string
	}{{a.b, ha, "origin", hb + "/worker"}, {b.b, hb, "worker", ha + "/origin"}} {
		_, err = f.b.PublishRemote(f.hub, f.sender, MailSubmission{ID: "second", Recipient: f.recipient, Kind: "status", Content: "status", CorrelationID: sent.CorrelationID})
		if err != nil {
			t.Fatal(err)
		}
		_, err = f.b.PublishRemote(f.hub, f.sender, MailSubmission{ID: "third", Recipient: f.recipient, Kind: "status", Content: "over cap", CorrelationID: sent.CorrelationID})
		if err == nil {
			t.Fatal("minted extra message permit")
		}
	}
	changed := *sent.Network
	changed.Content = "different"
	if _, err := b.b.ImportRemote(hb, changed); err == nil {
		t.Fatal("conflicting replay accepted")
	}
	// A peer cannot rebind the original native session by sending a response.
	e := a.b.Snapshot().Endpoints["origin"]
	e.SessionID = "different-session"
	if err := a.b.Register(e); err == nil {
		t.Fatal("rebound with unresolved incoming mail")
	}
}

func TestFederationRejectsSpoofingAndDoesNotCrossLocalConversation(t *testing.T) {
	f := mailFixture(t)
	ha, hb := strings.Repeat("a", 64), strings.Repeat("b", 64)
	local := publishMail(t, f.b, "origin", rootMail("local", "worker"))
	if _, err := f.b.PublishRemote(ha, "origin", MailSubmission{ID: "remote", Recipient: hb + "/worker", Kind: "request", Content: "x", CorrelationID: local.ID}); err == nil {
		t.Fatal("silently federated local context")
	}
	sent, err := f.b.PublishRemote(ha, "origin", rootMail("remote", hb+"/worker"))
	if err != nil {
		t.Fatal(err)
	}
	other := mailFixture(t)
	wire := *sent.Network
	wire.ToHub = ha
	if _, err := other.b.ImportRemote(hb, wire); err == nil {
		t.Fatal("wrong destination")
	}
	wire = *sent.Network
	wire.Sequence = 2
	if _, err := other.b.ImportRemote(hb, wire); err == nil {
		t.Fatal("missing initial message")
	}
	wire = *sent.Network
	wire.ID = "unqualified"
	if _, err := other.b.ImportRemote(hb, wire); err == nil {
		t.Fatal("unbound ID")
	}
}

func TestFederatedJournalRetainsOutboxAndDisablesLocalReinterpretation(t *testing.T) {
	f := mailFixture(t)
	ha, hb := strings.Repeat("a", 64), strings.Repeat("b", 64)
	sent, err := f.b.PublishRemote(ha, "origin", rootMail("persistent", hb+"/worker"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.b.Publish("origin", MailSubmission{ID: "local-next", Recipient: "worker", Kind: "request", Content: "x", CorrelationID: sent.CorrelationID}); err == nil {
		t.Fatal("federation bypassed using local publish")
	}
	if f.b.Snapshot().Version != 4 {
		t.Fatal("older binaries would drop federation state")
	}
	if err := f.b.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(f.dir, testDevice, map[string]string{"work": f.workspace})
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	f.b = reopened
	if f.b.Snapshot().Messages[sent.ID].Delivery != "network_pending" {
		t.Fatal("lost durable outbox")
	}
	if err := f.b.NetworkReceipt(sent.ID, "received"); err != nil {
		t.Fatal(err)
	}
	if got := f.b.Snapshot().Messages[sent.ID].Delivery; got != "remote_received" {
		t.Fatalf("receipt claimed native delivery: %s", got)
	}
	if err := f.b.NetworkReceipt(sent.ID, "received"); err != nil {
		t.Fatal(err)
	}
}
