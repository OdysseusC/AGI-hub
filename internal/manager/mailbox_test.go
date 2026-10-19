package manager

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func mailFixture(t *testing.T) *brokerFixture {
	t.Helper()
	f := newBrokerFixture(t)
	for _, endpoint := range f.b.Snapshot().Endpoints {
		endpoint.MailboxProtocolVersion = 1
		if err := f.b.Register(endpoint); err != nil {
			t.Fatal(err)
		}
	}
	return f
}
func rootMail(id, recipient string) MailSubmission {
	return MailSubmission{ID: id, Recipient: recipient, Kind: "request", Content: `{"summary":"one short native-assistant request"}`, OriginRef: "human-input-original", Deadline: time.Now().Add(time.Hour).UTC().Format(time.RFC3339)}
}
func publishMail(t *testing.T, b *Broker, sender string, request MailSubmission) Envelope {
	t.Helper()
	message, err := b.Publish(sender, request)
	if err != nil {
		t.Fatalf("Publish %s: %v", request.ID, err)
	}
	return message
}
func claimMail(t *testing.T, b *Broker, endpoint, id string) Envelope {
	t.Helper()
	message, err := b.NextMail(endpoint)
	if err != nil || message == nil {
		t.Fatalf("NextMail %s: %+v %v", endpoint, message, err)
	}
	if message.ID != id || message.TaskID != "" || !message.Mailbox || message.Mode != "queue" {
		t.Fatalf("unexpected mailbox claim: %+v", message)
	}
	return *message
}
func ackMail(t *testing.T, b *Broker, endpoint, id string) {
	t.Helper()
	claimMail(t, b, endpoint, id)
	if err := b.Delivery(id, "delivered", "native transport returned"); err != nil {
		t.Fatal(err)
	}
}
func noMail(t *testing.T, b *Broker, endpoint string) {
	t.Helper()
	message, err := b.NextMail(endpoint)
	if err != nil || message != nil {
		t.Fatalf("unexpected mail for %s: %+v %v", endpoint, message, err)
	}
}

func TestMailboxAgentsChooseRecipientsWithoutParentTasksOrAutomaticReplies(t *testing.T) {
	f := mailFixture(t)
	first := publishMail(t, f.b, "origin", rootMail("a-b", "worker"))
	// A asks C independently. B asks D, without reports, parent IDs, or waiting states.
	publishMail(t, f.b, "origin", MailSubmission{ID: "a-c", Recipient: "other-origin", Kind: "request", Content: "A asks C", CorrelationID: first.CorrelationID})
	bd := publishMail(t, f.b, "worker", MailSubmission{ID: "b-d", Recipient: "other-worker", Kind: "request", Content: "B asks D", CorrelationID: first.CorrelationID, ReplyTo: first.ID})
	// ReplyTo only links data: D may send directly to original A instead of B.
	result := publishMail(t, f.b, "other-worker", MailSubmission{ID: "d-a", Recipient: "origin", Kind: "result", Content: "D's verified result", CorrelationID: first.CorrelationID, ReplyTo: bd.ID})
	if result.RootOrigin.Endpoint != "origin" || result.RootOrigin.SessionID != "session-origin" || result.RootOrigin.RequestRef != "human-input-original" {
		t.Fatalf("original input context changed: %+v", result.RootOrigin)
	}
	ackMail(t, f.b, "worker", first.ID)
	ackMail(t, f.b, "other-origin", "a-c")
	ackMail(t, f.b, "other-worker", bd.ID)
	ackMail(t, f.b, "origin", result.ID)
	snapshot := f.b.Snapshot()
	if len(snapshot.Tasks) != 0 || len(snapshot.Messages) != 4 || snapshot.MailConversations[first.ID].UsedMessages != 4 {
		t.Fatalf("MQ created workflow state or extra reply: %+v", snapshot)
	}
	if snapshot.MailConversations[first.ID].Closed {
		t.Fatal("a result silently forced the conversation closed")
	}
	if err := f.b.Report(Report{EventID: "not-a-task", TaskID: first.ID, Endpoint: "worker", State: "succeeded", Summary: "done"}); err == nil {
		t.Fatal("mail delivery was treated as task execution")
	}
	if len(f.b.Snapshot().Messages) != 4 {
		t.Fatal("report generated an automatic reply for mail")
	}
}

func TestMailboxSameIDIsIdempotentAndSharedLimitIsAtomicAcrossBranches(t *testing.T) {
	f := mailFixture(t)
	request := rootMail("root", "worker")
	request.MaxMessages = 5
	first := publishMail(t, f.b, "origin", request)
	if duplicate := publishMail(t, f.b, "origin", request); duplicate.ID != first.ID {
		t.Fatal("duplicate created another message")
	}
	changed := request
	changed.Content = "different action"
	if _, err := f.b.Publish("origin", changed); err == nil {
		t.Fatal("same ID accepted changed content")
	}
	var wg sync.WaitGroup
	var mu sync.Mutex
	success := 0
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := f.b.Publish("worker", MailSubmission{ID: fmt.Sprintf("branch-%d", i), Recipient: "origin", Kind: "status", Content: "short update", CorrelationID: first.ID})
			if err == nil {
				mu.Lock()
				success++
				mu.Unlock()
			}
		}(i)
	}
	wg.Wait()
	snapshot := f.b.Snapshot()
	if success != 4 || snapshot.MailConversations[first.ID].UsedMessages != 5 || len(snapshot.Messages) != 5 {
		t.Fatalf("shared limit was forked: successes%d conversation%+v", success, snapshot.MailConversations[first.ID])
	}
	publishMail(t, f.b, "origin", request) // retries still work at the limit
}

func TestMailboxParticipantAndReplyVisibilityDoNotForceRouting(t *testing.T) {
	f := mailFixture(t)
	first := publishMail(t, f.b, "origin", rootMail("root", "worker"))
	if _, err := f.b.Publish("other-origin", MailSubmission{ID: "outsider", Recipient: "origin", Kind: "reply", Content: "unexpected", CorrelationID: first.ID}); err == nil {
		t.Fatal("nonparticipant joined conversation without receiving a message")
	}
	for _, change := range []func(*MailSubmission){func(s *MailSubmission) { s.OriginRef = "another human input" }, func(s *MailSubmission) { s.MaxMessages = 64 }, func(s *MailSubmission) { s.Deadline = time.Now().Add(2 * time.Hour).UTC().Format(time.RFC3339) }} {
		s := MailSubmission{ID: "changed", Recipient: "origin", Kind: "reply", Content: "changed", CorrelationID: first.ID}
		change(&s)
		if _, err := f.b.Publish("worker", s); err == nil {
			t.Fatal("mail replaced pinned context or limits")
		}
	}
	publishMail(t, f.b, "origin", MailSubmission{ID: "join-c", Recipient: "other-origin", Kind: "request", Content: "invite C", CorrelationID: first.ID})
	if _, err := f.b.Publish("other-origin", MailSubmission{ID: "invisible-link", Recipient: "origin", Kind: "reply", Content: "not visible to C", CorrelationID: first.ID, ReplyTo: first.ID}); err == nil {
		t.Fatal("participant linked a message it neither sent nor received")
	}
	publishMail(t, f.b, "worker", MailSubmission{ID: "b-d", Recipient: "other-worker", Kind: "reply", Content: "forward to D", CorrelationID: first.ID, ReplyTo: first.ID})
	if f.b.Snapshot().MailConversations[first.ID].UsedMessages != 3 {
		t.Fatal("rejected publication consumed shared count")
	}
}

func TestMailboxInflightTransportReservationIsIndependentOfBusinessReports(t *testing.T) {
	f := mailFixture(t)
	first := publishMail(t, f.b, "origin", rootMail("root", "worker"))
	publishMail(t, f.b, "origin", MailSubmission{ID: "second", Recipient: "worker", Kind: "request", Content: "second", CorrelationID: first.ID})
	claimMail(t, f.b, "worker", first.ID)
	noMail(t, f.b, "worker")
	if err := f.b.Delivery(first.ID, "uncertain", "native exit not observed"); err != nil {
		t.Fatal(err)
	}
	noMail(t, f.b, "worker")
	if err := f.b.Delivery(first.ID, "failed", "guessing it failed"); err == nil {
		t.Fatal("uncertain delivery was converted to a retryable failure")
	}
	if err := f.b.Delivery(first.ID, "delivered", "confirmed native CLI exit"); err != nil {
		t.Fatal(err)
	}
	claimMail(t, f.b, "worker", "second")
	if len(f.b.Snapshot().Tasks) != 0 {
		t.Fatal("transport receipt created business execution state")
	}
}

func TestMailboxWakeUsesNativeCapacityAndStatusNeverNeedsModel(t *testing.T) {
	f := mailFixture(t)
	if err := f.b.SetSession("worker", "unknown", ""); err != nil {
		t.Fatal(err)
	}
	first := publishMail(t, f.b, "origin", rootMail("root", "worker"))
	publishMail(t, f.b, "origin", MailSubmission{ID: "status", Recipient: "worker", Kind: "status", Content: "one metadata update", CorrelationID: first.ID})
	claimMail(t, f.b, "worker", "status")
	if err := f.b.Delivery("status", "delivered", "stored locally without native wake"); err != nil {
		t.Fatal(err)
	}
	noMail(t, f.b, "worker")
	if err := f.b.SetSession("worker", "busy", "human-native-turn"); err != nil {
		t.Fatal(err)
	}
	noMail(t, f.b, "worker")
	endpoint := f.b.Snapshot().Endpoints["worker"]
	endpoint.MailboxWhileBusyVerified = true
	if err := f.b.Register(endpoint); err != nil {
		t.Fatal(err)
	}
	claimMail(t, f.b, "worker", first.ID)
	if f.b.Snapshot().Sessions["worker"].RunID != "human-native-turn" {
		t.Fatal("MQ replaced native capacity with business task state")
	}
}

func TestMailboxCancelWithdrawsOnlyUnsentMessagesAndNeverInventsExecutionStop(t *testing.T) {
	f := mailFixture(t)
	request := rootMail("root", "worker")
	first := publishMail(t, f.b, "origin", request)
	publishMail(t, f.b, "worker", MailSubmission{ID: "unsent", Recipient: "origin", Kind: "reply", Content: "reply", CorrelationID: first.ID})
	claimMail(t, f.b, "worker", first.ID)
	if err := f.b.CancelMail("worker", first.ID); err == nil {
		t.Fatal("peer closed another input's conversation")
	}
	if err := f.b.CancelMail("origin", first.ID); err != nil {
		t.Fatal(err)
	}
	snapshot := f.b.Snapshot()
	if snapshot.Messages[first.ID].Delivery != "dispatching" || snapshot.Messages["unsent"].Delivery != "failed" || len(snapshot.Tasks) != 0 {
		t.Fatal("cancel lied about actual native execution")
	}
	if _, err := f.b.Publish("origin", MailSubmission{ID: "after-cancel", Recipient: "worker", Kind: "request", Content: "new action", CorrelationID: first.ID}); err == nil {
		t.Fatal("closed conversation accepted new publication")
	}
	publishMail(t, f.b, "origin", request) // stable retry remains a receipt, not a resend
}

func TestMailboxJournalVersion3ReplayPreservesLegacyAndUncertainMail(t *testing.T) {
	f := mailFixture(t)
	legacy := submitTask(t, f.b, testSubmission("legacy", "other-worker"))
	if f.b.Snapshot().Version != 1 {
		t.Fatal("legacy operation changed journal version")
	}
	first := publishMail(t, f.b, "origin", rootMail("root", "worker"))
	claimMail(t, f.b, "worker", first.ID)
	if f.b.Snapshot().Version != 3 {
		t.Fatal("mail journal did not guard against old readers")
	}
	if err := f.b.Close(); err != nil {
		t.Fatal(err)
	}
	f.b = nil
	var err error
	f.b, err = Open(f.dir, testDevice, map[string]string{"work": f.workspace})
	if err != nil {
		t.Fatal(err)
	}
	snapshot := f.b.Snapshot()
	if snapshot.Version != 3 || snapshot.Tasks[legacy.ID].ID != legacy.ID || snapshot.Messages[first.ID].Delivery != "uncertain" || snapshot.MailConversations[first.ID].UsedMessages != 1 {
		t.Fatal("replay dropped existing task or mail state")
	}
	if err := f.b.SetSession("worker", "idle", ""); err != nil {
		t.Fatal(err)
	}
	noMail(t, f.b, "worker")
	snapshot.MailConversations[first.ID].Participants["malicious"] = true
	snapshot.Messages[first.ID].RootOrigin.Endpoint = "changed-copy"
	if f.b.Snapshot().MailConversations[first.ID].Participants["malicious"] || f.b.Snapshot().Messages[first.ID].RootOrigin.Endpoint != "origin" {
		t.Fatal("external snapshot mutation changed persistent mail context")
	}
}

func TestMailboxJournalRejectsUnknownVersionWithoutRewriting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, journalName)
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		t.Fatal(err)
	}
	future := emptySnapshot()
	future.Version = 4
	if err = appendSnapshot(file, future); err != nil {
		file.Close()
		t.Fatal(err)
	}
	if err = file.Close(); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	broker, err := Open(dir, testDevice, nil)
	if err == nil {
		broker.Close()
		t.Fatal("unknown journal version opened")
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("unknown version was modified")
	}
}

func TestMailboxPreservesArchivedPrototypeV2WithoutRevivingItsWorkflow(t *testing.T) {
	f := mailFixture(t)
	legacy := submitTask(t, f.b, testSubmission("prototype", "worker"))
	origin := OriginContext{Endpoint: "origin", DeviceID: testDevice, SessionID: "session-origin", RequestRef: "old-test"}
	snapshot := f.b.Snapshot()
	snapshot.Version = 2
	task := snapshot.Tasks[legacy.ID]
	task.RootOrigin = &origin
	task.CorrelationID = legacy.ID
	task.Execution = "needs_user"
	task.RunID = "old-prototype-run"
	snapshot.Tasks[legacy.ID] = task
	message := snapshot.Messages[taskMessageID(legacy.ID)]
	message.RootOrigin = &origin
	snapshot.Messages[message.ID] = message
	snapshot.Collaborations[legacy.ID] = Collaboration{ID: legacy.ID, RootTaskID: legacy.ID, RootOrigin: origin, MaxHandoffs: 2, UsedHandoffs: 1, Closed: true}
	f.b.mu.Lock()
	err := f.b.commit(snapshot)
	f.b.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if err = f.b.SetSession("worker", "idle", ""); err != nil {
		t.Fatalf("archived prototype prevented physical capacity reconciliation: %v", err)
	}
	assertNoMessage(t, f.b, "worker")
	if err = f.b.Report(Report{EventID: "revive-old-prototype", TaskID: legacy.ID, Endpoint: "worker", State: "running", RunID: task.RunID}); err == nil {
		t.Fatal("archived prototype resumed via legacy Report")
	}
	fresh := publishMail(t, f.b, "origin", rootMail("new-mail", "worker"))
	ackMail(t, f.b, "worker", fresh.ID)
	saved := f.b.Snapshot()
	if saved.Version != 3 || saved.Tasks[legacy.ID].Execution != "needs_user" || !saved.Collaborations[legacy.ID].Closed || saved.Tasks[legacy.ID].RunID != task.RunID {
		t.Fatal("mail commit rewrote archived diagnostic task or dropped v2 metadata")
	}
}

func TestMailboxOriginalEndpointDriftFailsWithoutForcingAnotherRecipient(t *testing.T) {
	f := mailFixture(t)
	first := publishMail(t, f.b, "origin", rootMail("root", "worker"))
	ackMail(t, f.b, "worker", first.ID)
	endpoint := f.b.Snapshot().Endpoints["origin"]
	endpoint.SessionID = "different-native-chat"
	if err := f.b.Register(endpoint); err != nil {
		t.Fatal(err)
	}
	if _, err := f.b.Publish("worker", MailSubmission{ID: "wrong-chat", Recipient: "origin", Kind: "result", Content: "would reach a different native chat", CorrelationID: first.ID}); err == nil {
		t.Fatal("root endpoint drift silently delivered to another native session")
	}
	selected := publishMail(t, f.b, "worker", MailSubmission{ID: "chosen-c", Recipient: "other-origin", Kind: "result", Content: "B explicitly selected C", CorrelationID: first.ID})
	if selected.RootOrigin.SessionID != "session-origin" {
		t.Fatal("original context was replaced")
	}
	ackMail(t, f.b, "other-origin", selected.ID)
	if f.b.Snapshot().MailConversations[first.ID].UsedMessages != 2 {
		t.Fatal("refused drift consumed message budget")
	}
}

func TestMailboxPendingOriginalEndpointDriftIsNotClaimed(t *testing.T) {
	f := mailFixture(t)
	first := publishMail(t, f.b, "origin", rootMail("root", "worker"))
	publishMail(t, f.b, "worker", MailSubmission{ID: "result", Recipient: "origin", Kind: "result", Content: "for original session", CorrelationID: first.ID})
	// Register itself normally refuses rebinding unresolved mail. Simulate a
	// future binding migration to verify the independent claim-time guard.
	state := f.b.Snapshot()
	endpoint := state.Endpoints["origin"]
	endpoint.SessionID = "different-native-chat"
	state.Endpoints["origin"] = endpoint
	f.b.mu.Lock()
	err := f.b.commit(state)
	f.b.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if message, err := f.b.NextMail("origin"); err == nil || message != nil {
		t.Fatal("pending mail was claimed by a different original-entry session")
	}
	if f.b.Snapshot().Messages["result"].Delivery != "pending" {
		t.Fatal("drift guard changed delivery without native evidence")
	}
}
