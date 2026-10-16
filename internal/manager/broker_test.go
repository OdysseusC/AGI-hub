package manager

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

const testDevice = "test-device"

func TestSubmissionInputsAreAccessibleOnTheDeclaredDevice(t *testing.T) {
	f := newBrokerFixture(t)
	if err := os.WriteFile(filepath.Join(f.workspace, "brief.txt"), []byte("source material"), 0600); err != nil {
		t.Fatal(err)
	}
	sub := testSubmission("input-scope", "worker")
	sub.Inputs = []Artifact{{DeviceID: "another-device", WorkspaceID: "work", Path: "brief.txt"}}
	if _, err := f.b.Submit(sub); err == nil {
		t.Fatal("remote local path was accepted as an input")
	}
	sub.Inputs[0].DeviceID = testDevice
	sub.Inputs[0].Path = "missing.txt"
	if _, err := f.b.Submit(sub); err == nil {
		t.Fatal("missing input was accepted")
	}
	sub.Inputs[0].Path = "brief.txt"
	task := submitTask(t, f.b, sub)
	if len(task.Inputs) != 1 || task.Inputs[0].Path != "brief.txt" {
		t.Fatal("input reference was not retained")
	}
	if _, err := f.b.Submit(sub); err != nil {
		t.Fatal("exact input retry was not idempotent:", err)
	}
}

func TestExecutionReceiptWinsOverLateTransportTimeout(t *testing.T) {
	f := newBrokerFixture(t)
	submission := testSubmission("receipt-race", "worker")
	submission.Resources = []string{"workspace:work"}
	task := submitTask(t, f.b, submission)
	env := dispatchTask(t, f.b, "worker", task.ID)
	if err := f.b.Report(Report{EventID: "early-accept", TaskID: task.ID, Endpoint: "worker", State: "accepted", RunID: "receipt-run"}); err != nil {
		t.Fatal(err)
	}
	before := f.b.Snapshot()
	info, err := os.Stat(filepath.Join(f.dir, journalName))
	if err != nil {
		t.Fatal(err)
	}
	for _, outcome := range []string{"uncertain", "failed"} {
		if err := f.b.Delivery(env.ID, outcome, "late transport timeout or exit failure"); err != nil {
			t.Fatalf("native-confirmed late receipt must be an acknowledged no-op: %v", err)
		}
		if !sameJSON(before, f.b.Snapshot()) {
			t.Fatal("late transport receipt changed stronger native execution evidence or locks")
		}
	}
	afterInfo, err := os.Stat(filepath.Join(f.dir, journalName))
	if err != nil || afterInfo.Size() != info.Size() {
		t.Fatal("an acknowledged no-op appended a journal mutation")
	}
	state := f.b.Snapshot()
	if state.Messages[env.ID].Delivery != "delivered" || state.Tasks[task.ID].Execution != "accepted" {
		t.Fatal("late timeout replaced execution evidence")
	}
	if err := f.b.Report(Report{EventID: "early-success", TaskID: task.ID, Endpoint: "worker", State: "succeeded", RunID: "receipt-run", Summary: "Already completed before the transport exit"}); err != nil {
		t.Fatal(err)
	}
	completed := f.b.Snapshot()
	if err := f.b.Delivery(env.ID, "uncertain", "late process timeout after completion"); err != nil {
		t.Fatal(err)
	}
	if !sameJSON(completed, f.b.Snapshot()) || len(resultMessages(f.b.Snapshot(), task.ID)) != 1 {
		t.Fatal("late receipt changed native completion or created another result")
	}
}

func TestConflictingDeliveryWithoutNativeRunEvidenceIsRejected(t *testing.T) {
	f := newBrokerFixture(t)
	task := submitTask(t, f.b, testSubmission("transport-only", "worker"))
	envelope := dispatchTask(t, f.b, "worker", task.ID)
	if err := f.b.Delivery(envelope.ID, "delivered", "transport-only queue confirmation"); err != nil {
		t.Fatal(err)
	}
	before := f.b.Snapshot()
	for _, outcome := range []string{"uncertain", "failed"} {
		if err := f.b.Delivery(envelope.ID, outcome, "contradictory transport evidence"); err == nil {
			t.Fatal("transport-only confirmation swallowed a conflicting receipt")
		}
	}
	if !sameJSON(before, f.b.Snapshot()) {
		t.Fatal("conflicting transport receipt changed state")
	}
	if err := f.b.Report(Report{EventID: "no-run-failure", TaskID: task.ID, Endpoint: "worker", State: "failed", Summary: "No accepted native execution was established"}); err != nil {
		t.Fatal(err)
	}
	if err := f.b.Delivery(envelope.ID, "uncertain", "receipt without any accepted run"); err == nil {
		t.Fatal("a report without native run evidence qualified for the late-receipt exception")
	}
	result := dispatchTask(t, f.b, "origin", task.ID)
	if result.Kind != "result" {
		t.Fatal("expected a result message")
	}
	if err := f.b.Delivery(result.ID, "delivered", "origin transport confirmation"); err != nil {
		t.Fatal(err)
	}
	if err := f.b.Delivery(result.ID, "uncertain", "conflicting result transport evidence"); err == nil {
		t.Fatal("a result receipt qualified for native task acceptance")
	}
}

type brokerFixture struct {
	b         *Broker
	dir       string
	workspace string
}

func newBrokerFixture(t *testing.T) *brokerFixture {
	t.Helper()
	f := &brokerFixture{dir: t.TempDir(), workspace: t.TempDir()}
	var err error
	f.b, err = Open(f.dir, testDevice, map[string]string{"work": f.workspace})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() {
		if f.b != nil {
			if err := f.b.Close(); err != nil {
				t.Errorf("Close: %v", err)
			}
		}
	})
	for _, id := range []string{"origin", "other-origin", "worker", "other-worker"} {
		if err := f.b.Register(Endpoint{
			ID: id, DeviceID: testDevice, SessionID: "session-" + id,
			Enabled: true, IdleWakeVerified: true,
			Modes: []string{"queue", "steer", "interrupt"}, Driver: "manual",
		}); err != nil {
			t.Fatalf("Register(%s): %v", id, err)
		}
		if err := f.b.SetSession(id, "idle", ""); err != nil {
			t.Fatalf("SetSession(%s): %v", id, err)
		}
	}
	if err := f.b.SetLeader("origin"); err != nil {
		t.Fatalf("SetLeader: %v", err)
	}
	return f
}

func testSubmission(id, recipient string) Submission {
	return Submission{
		ID: id, Origin: "origin", Recipient: recipient,
		Goal: "Produce a verifiable local result", Mode: "queue", MaxHandoffs: 1,
		Deadline: time.Now().Add(time.Hour).UTC().Format(time.RFC3339),
	}
}

func submitTask(t *testing.T, b *Broker, s Submission) Task {
	t.Helper()
	task, err := b.Submit(s)
	if err != nil {
		t.Fatalf("Submit(%s): %v", s.ID, err)
	}
	return task
}

func dispatchTask(t *testing.T, b *Broker, endpoint, taskID string) Envelope {
	t.Helper()
	env, err := b.Next(endpoint)
	if err != nil {
		t.Fatalf("Next(%s): %v", endpoint, err)
	}
	if env == nil {
		t.Fatalf("Next(%s): expected task %s, got no message", endpoint, taskID)
	}
	if env.TaskID != taskID || env.Recipient != endpoint || env.Delivery != "dispatching" {
		t.Fatalf("unexpected dispatch: %+v", env)
	}
	return *env
}

func startTask(t *testing.T, b *Broker, task Task) string {
	t.Helper()
	env := dispatchTask(t, b, task.Recipient, task.ID)
	if err := b.Delivery(env.ID, "delivered", "native transport accepted message"); err != nil {
		t.Fatalf("Delivery: %v", err)
	}
	runID := "run-" + task.ID
	if err := b.Report(Report{
		EventID: "accepted-" + task.ID, TaskID: task.ID, Endpoint: task.Recipient,
		State: "accepted", RunID: runID,
	}); err != nil {
		t.Fatalf("Report accepted: %v", err)
	}
	return runID
}

func assertNoMessage(t *testing.T, b *Broker, endpoint string) {
	t.Helper()
	env, err := b.Next(endpoint)
	if err != nil {
		t.Fatalf("Next(%s): %v", endpoint, err)
	}
	if env != nil {
		t.Fatalf("Next(%s) unexpectedly dispatched %+v", endpoint, env)
	}
}

func resultMessages(s Snapshot, taskID string) []Envelope {
	var results []Envelope
	for _, m := range s.Messages {
		if m.Kind == "result" && m.TaskID == taskID {
			results = append(results, m)
		}
	}
	return results
}

func TestSubmissionIDIsIdempotentAndRejectsChangedContent(t *testing.T) {
	f := newBrokerFixture(t)
	s := testSubmission("same-id", "worker")
	first := submitTask(t, f.b, s)
	second := submitTask(t, f.b, s)
	if first.ID != second.ID || len(f.b.Snapshot().Tasks) != 1 || len(f.b.Snapshot().Messages) != 1 {
		t.Fatal("identical submission created duplicate work")
	}
	s.Goal = "Different work with a reused id"
	if _, err := f.b.Submit(s); err == nil {
		t.Fatal("reusing an id with changed content must be rejected")
	}
	if got := f.b.Snapshot().Tasks[first.ID].Goal; got != first.Goal {
		t.Fatalf("conflict mutated original task goal: %q", got)
	}
}

func TestDeliveryAndReceiptAreNotCompletionAndResultIsUnique(t *testing.T) {
	f := newBrokerFixture(t)
	task := submitTask(t, f.b, testSubmission("receipt", "worker"))
	env := dispatchTask(t, f.b, "worker", task.ID)
	if env.CorrelationID != task.ID || env.ReplyTo != "" {
		t.Fatalf("new task must begin its own correlation without claiming a reply: %+v", env)
	}
	var payload Submission
	if err := json.Unmarshal([]byte(env.Content), &payload); err != nil || payload.ID != task.ID || payload.Goal != task.Goal {
		t.Fatalf("task envelope must preserve the JSON submission: payload=%+v err=%v", payload, err)
	}
	if err := f.b.Delivery(env.ID, "delivered", "received by native tool"); err != nil {
		t.Fatal(err)
	}
	snapshot := f.b.Snapshot()
	if snapshot.Tasks[task.ID].Execution != "waiting" || len(resultMessages(snapshot, task.ID)) != 0 {
		t.Fatal("transport delivery was incorrectly treated as task completion")
	}
	accepted := Report{EventID: "event-accepted", TaskID: task.ID, Endpoint: "worker", State: "accepted", RunID: "run-receipt"}
	if err := f.b.Report(accepted); err != nil {
		t.Fatal(err)
	}
	if err := f.b.Report(accepted); err != nil {
		t.Fatalf("duplicate receipt must be idempotent: %v", err)
	}
	changed := accepted
	changed.RunID = "other-run"
	if err := f.b.Report(changed); err == nil {
		t.Fatal("an event id with a changed payload must be rejected")
	}
	snapshot = f.b.Snapshot()
	if snapshot.Tasks[task.ID].Execution != "accepted" || len(resultMessages(snapshot, task.ID)) != 0 {
		t.Fatal("acceptance receipt was incorrectly treated as a final result")
	}
	if err := f.b.Report(Report{EventID: "event-running", TaskID: task.ID, Endpoint: "worker", State: "running", RunID: accepted.RunID}); err != nil {
		t.Fatal(err)
	}
	done := Report{EventID: "event-done", TaskID: task.ID, Endpoint: "worker", State: "succeeded", RunID: accepted.RunID, Summary: "Verified local result"}
	if err := f.b.Report(done); err != nil {
		t.Fatal(err)
	}
	if err := f.b.Report(done); err != nil {
		t.Fatalf("duplicate completion must be idempotent: %v", err)
	}
	done.EventID = "event-done-again"
	// A terminal-state retry may be rejected or acknowledged, but cannot create
	// another return message or change the completed task.
	_ = f.b.Report(done)
	snapshot = f.b.Snapshot()
	results := resultMessages(snapshot, task.ID)
	if snapshot.Tasks[task.ID].Execution != "succeeded" || len(results) != 1 {
		t.Fatalf("expected one successful result, got task=%+v results=%+v", snapshot.Tasks[task.ID], results)
	}
	if results[0].Recipient != task.Origin || results[0].Delivery != "pending" {
		t.Fatalf("result must return to the original entry point: %+v", results[0])
	}
	if results[0].ID == env.ID || results[0].ID != resultMessageID(task.ID) || results[0].TaskID != task.ID || results[0].ReplyTo != env.ID || results[0].CorrelationID != env.CorrelationID {
		t.Fatalf("result must use a distinct stable message ID and retain task/reply/correlation links: task=%+v result=%+v", env, results[0])
	}
}

func TestRestartPreservesUncertainDeliveryWithoutResending(t *testing.T) {
	f := newBrokerFixture(t)
	submission := testSubmission("in-flight", "worker")
	submission.Resources = []string{"workspace:work"}
	task := submitTask(t, f.b, submission)
	env := dispatchTask(t, f.b, "worker", task.ID)
	if err := f.b.Close(); err != nil {
		t.Fatal(err)
	}
	f.b = nil
	var err error
	f.b, err = Open(f.dir, testDevice, map[string]string{"work": f.workspace})
	if err != nil {
		t.Fatalf("restart: %v", err)
	}
	snapshot := f.b.Snapshot()
	if got := snapshot.Messages[env.ID].Delivery; got != "uncertain" {
		t.Fatalf("in-flight delivery after restart = %q, want uncertain", got)
	}
	if got := snapshot.Sessions["worker"].State; got != "unknown" {
		t.Fatalf("session after restart = %q, want unknown", got)
	}
	if got := snapshot.ResourceOwners["workspace:work"]; got != task.ID {
		t.Fatalf("restart released uncertain task's workspace reservation: %q", got)
	}
	if err := f.b.SetSession("worker", "idle", ""); err != nil {
		t.Fatal(err)
	}
	submitTask(t, f.b, testSubmission("following", "worker"))
	assertNoMessage(t, f.b, "worker")
	followingResource := testSubmission("following-resource", "other-worker")
	followingResource.Resources = []string{"workspace:work"}
	submitTask(t, f.b, followingResource)
	assertNoMessage(t, f.b, "other-worker")
	if len(f.b.Snapshot().Tasks) != 3 {
		t.Fatal("restart lost recorded work")
	}
}

func TestRestartKeepsAcceptedRunUntilExecutionIsReconciled(t *testing.T) {
	f := newBrokerFixture(t)
	s := testSubmission("accepted-before-restart", "worker")
	s.Resources = []string{"workspace:work"}
	task := submitTask(t, f.b, s)
	runID := startTask(t, f.b, task)
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
	if snapshot.Tasks[task.ID].Execution != "accepted" || snapshot.Tasks[task.ID].RunID != runID {
		t.Fatal("restart lost accepted execution identity")
	}
	if snapshot.Sessions["worker"].State != "unknown" || snapshot.Sessions["worker"].RunID != runID {
		t.Fatal("restart treated an accepted native run as an idle session")
	}
	if err := f.b.SetSession("worker", "idle", ""); err == nil {
		t.Fatal("an idle observation cannot clear an unfinished accepted execution")
	}
	if err := f.b.Report(Report{EventID: "restart-confirmed-done", TaskID: task.ID, Endpoint: "worker", State: "succeeded", RunID: runID, Summary: "Native run was reconciled and completed"}); err != nil {
		t.Fatal(err)
	}
	snapshot = f.b.Snapshot()
	if snapshot.Sessions["worker"].State != "idle" || len(snapshot.ResourceOwners) != 0 || len(resultMessages(snapshot, task.ID)) != 1 {
		t.Fatal("reconciled completion did not release the run and produce its result")
	}
}

func TestLeaderSwitchAffectsNewWorkAndKeepsOldOrigin(t *testing.T) {
	f := newBrokerFixture(t)
	s := testSubmission("before-switch", "worker")
	s.Origin = ""
	old := submitTask(t, f.b, s)
	if old.Origin != "origin" || old.Coordinator != "origin" {
		t.Fatalf("initial leader was not captured: %+v", old)
	}
	if err := f.b.SetLeader("other-origin"); err != nil {
		t.Fatal(err)
	}
	retried := submitTask(t, f.b, s)
	if retried.Origin != old.Origin || retried.LeaderEpoch != old.LeaderEpoch {
		t.Fatal("idempotent retry after switching leader migrated the old task")
	}
	s.ID = "after-switch"
	newTask := submitTask(t, f.b, s)
	if newTask.Origin != "other-origin" || newTask.Coordinator != "other-origin" || newTask.LeaderEpoch <= old.LeaderEpoch {
		t.Fatalf("new work did not use switched leader: %+v", newTask)
	}
	runID := startTask(t, f.b, old)
	if err := f.b.Report(Report{EventID: "old-done", TaskID: old.ID, Endpoint: old.Recipient, State: "succeeded", RunID: runID, Summary: "Old task finished"}); err != nil {
		t.Fatal(err)
	}
	results := resultMessages(f.b.Snapshot(), old.ID)
	if len(results) != 1 || results[0].Recipient != "origin" {
		t.Fatalf("old result did not return to its original entry point: %+v", results)
	}
	if results[0].TaskID != old.ID || results[0].ReplyTo != taskMessageID(old.ID) || results[0].CorrelationID != old.ID {
		t.Fatalf("leader switch changed old result's message links: %+v", results[0])
	}
}

func TestQueueWaitsWhileSessionIsBusy(t *testing.T) {
	f := newBrokerFixture(t)
	if err := f.b.SetSession("worker", "busy", "native-existing-run"); err != nil {
		t.Fatal(err)
	}
	task := submitTask(t, f.b, testSubmission("queued", "worker"))
	assertNoMessage(t, f.b, "worker")
	if err := f.b.SetSession("worker", "idle", ""); err != nil {
		t.Fatal(err)
	}
	dispatchTask(t, f.b, "worker", task.ID)
	// Dispatch reservation must block another queued task before the native
	// assistant sends its acceptance receipt.
	submitTask(t, f.b, testSubmission("second-queued", "worker"))
	assertNoMessage(t, f.b, "worker")
}

func TestUnsupportedDeliveryCapabilitiesAreNotInferred(t *testing.T) {
	t.Run("unsupported-mode", func(t *testing.T) {
		f := newBrokerFixture(t)
		endpoint := f.b.Snapshot().Endpoints["worker"]
		endpoint.Modes = []string{"queue"}
		if err := f.b.Register(endpoint); err != nil {
			t.Fatal(err)
		}
		s := testSubmission("unsupported", "worker")
		s.Mode = "steer"
		s.TargetRunID = "run-not-supported"
		if _, err := f.b.Submit(s); err == nil {
			t.Fatal("unsupported steer capability must be rejected")
		}
	})
	t.Run("unverified-idle-wake", func(t *testing.T) {
		f := newBrokerFixture(t)
		endpoint := f.b.Snapshot().Endpoints["worker"]
		endpoint.IdleWakeVerified = false
		if err := f.b.Register(endpoint); err != nil {
			t.Fatal(err)
		}
		if _, err := f.b.Submit(testSubmission("unverified-wake", "worker")); err != nil {
			return
		}
		env, _ := f.b.Next("worker")
		if env != nil {
			t.Fatal("an idle native session was dispatched without verified wake capability")
		}
	})
}

func TestWorkspaceResourceIsReservedUntilExecutionCompletes(t *testing.T) {
	f := newBrokerFixture(t)
	s := testSubmission("resource-first", "worker")
	s.Resources = []string{"workspace:work"}
	first := submitTask(t, f.b, s)
	s.ID, s.Recipient = "resource-second", "other-worker"
	second := submitTask(t, f.b, s)
	env := dispatchTask(t, f.b, first.Recipient, first.ID)
	assertNoMessage(t, f.b, second.Recipient)
	if err := f.b.Delivery(env.ID, "delivered", "accepted by transport"); err != nil {
		t.Fatal(err)
	}
	if err := f.b.Report(Report{EventID: "resource-accepted", TaskID: first.ID, Endpoint: first.Recipient, State: "accepted", RunID: "resource-run"}); err != nil {
		t.Fatal(err)
	}
	assertNoMessage(t, f.b, second.Recipient)
	if err := f.b.Report(Report{EventID: "resource-done", TaskID: first.ID, Endpoint: first.Recipient, State: "succeeded", RunID: "resource-run", Summary: "Workspace edit complete"}); err != nil {
		t.Fatal(err)
	}
	dispatchTask(t, f.b, second.Recipient, second.ID)
}

func TestSteerIsAnAuditedControlAndKeepsTheOriginalRunReserved(t *testing.T) {
	f := newBrokerFixture(t)
	s := testSubmission("original-run", "worker")
	s.Resources = []string{"workspace:work"}
	original := submitTask(t, f.b, s)
	runID := startTask(t, f.b, original)
	controlSubmission := testSubmission("steer-run", "worker")
	controlSubmission.Mode = "steer"
	controlSubmission.TargetRunID = "stale-or-unrelated-run"
	if _, err := f.b.Submit(controlSubmission); err == nil {
		t.Fatal("control must reject a run id that does not match the recipient's active task")
	}
	controlSubmission.TargetRunID = runID
	control := submitTask(t, f.b, controlSubmission)
	env := dispatchTask(t, f.b, "worker", control.ID)
	if env.Kind != "control" || env.Mode != "steer" || env.TargetRunID != runID || env.CorrelationID != control.ID || env.ReplyTo != "" {
		t.Fatalf("steer envelope lost its control/run association: %+v", env)
	}
	if err := f.b.Delivery(env.ID, "delivered", "guidance accepted by native session"); err != nil {
		t.Fatal(err)
	}
	if err := f.b.Report(Report{EventID: "steer-accepted", TaskID: control.ID, Endpoint: "worker", State: "accepted", RunID: runID}); err != nil {
		t.Fatal(err)
	}
	if err := f.b.Report(Report{EventID: "steer-done", TaskID: control.ID, Endpoint: "worker", State: "succeeded", RunID: runID, Summary: "Guidance was applied"}); err != nil {
		t.Fatal(err)
	}
	snapshot := f.b.Snapshot()
	if snapshot.Tasks[original.ID].Execution != "accepted" || snapshot.Tasks[original.ID].RunID != runID {
		t.Fatal("finishing a steer control changed the original running task")
	}
	if snapshot.Sessions["worker"].State != "busy" || snapshot.Sessions["worker"].RunID != runID {
		t.Fatal("steer completion incorrectly freed the native session")
	}
	if snapshot.ResourceOwners["workspace:work"] != original.ID {
		t.Fatal("steer completion incorrectly freed the original resource lock")
	}
	if len(resultMessages(snapshot, original.ID)) != 0 || len(resultMessages(snapshot, control.ID)) != 1 {
		t.Fatal("steer result was confused with the original task's result")
	}
	result := resultMessages(snapshot, control.ID)[0]
	if result.ID == env.ID || result.ReplyTo != env.ID || result.CorrelationID != control.ID || result.TaskID != control.ID {
		t.Fatalf("control result must answer its own control message: control=%+v result=%+v", env, result)
	}
}

func TestInterruptOnlyEndsOriginalRunAfterAnInterruptedReceipt(t *testing.T) {
	f := newBrokerFixture(t)
	s := testSubmission("interrupt-original", "worker")
	s.Resources = []string{"workspace:work"}
	original := submitTask(t, f.b, s)
	runID := startTask(t, f.b, original)
	controlSubmission := testSubmission("interrupt-request", "worker")
	controlSubmission.Mode = "interrupt"
	controlSubmission.TargetRunID = runID
	control := submitTask(t, f.b, controlSubmission)
	env := dispatchTask(t, f.b, "worker", control.ID)
	if err := f.b.Delivery(env.ID, "delivered", "native interrupt requested"); err != nil {
		t.Fatal(err)
	}
	if err := f.b.Report(Report{EventID: "interrupt-generic-success", TaskID: control.ID, Endpoint: "worker", State: "succeeded", RunID: runID, Summary: "Tool accepted the request"}); err == nil {
		t.Fatal("generic control success cannot confirm that the original execution stopped")
	}
	snapshot := f.b.Snapshot()
	if snapshot.Tasks[original.ID].Execution != "accepted" || snapshot.ResourceOwners["workspace:work"] != original.ID {
		t.Fatal("an unconfirmed interrupt freed the active task or its resources")
	}
	if err := f.b.Report(Report{EventID: "interrupt-confirmed", TaskID: control.ID, Endpoint: "worker", State: "interrupted", RunID: runID, Summary: "Native execution has stopped"}); err != nil {
		t.Fatal(err)
	}
	snapshot = f.b.Snapshot()
	if snapshot.Tasks[original.ID].Execution != "interrupted" || snapshot.Tasks[control.ID].Execution != "interrupted" {
		t.Fatal("confirmed interrupt must terminate and audit both the original task and its control")
	}
	if snapshot.Sessions["worker"].State != "idle" || snapshot.Sessions["worker"].RunID != "" {
		t.Fatal("confirmed interrupt did not free the native session")
	}
	if _, locked := snapshot.ResourceOwners["workspace:work"]; locked {
		t.Fatal("confirmed interrupt did not release the original workspace")
	}
	if len(resultMessages(snapshot, original.ID)) != 1 || len(resultMessages(snapshot, control.ID)) != 1 {
		t.Fatal("confirmed interrupt did not create one audited result for each task")
	}
}

func TestSubmissionRequiresExplicitBoundedExecution(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*Submission)
	}{
		{name: "missing-deadline", change: func(s *Submission) { s.Deadline = "" }},
		{name: "malformed-deadline", change: func(s *Submission) { s.Deadline = "later" }},
		{name: "no-handoff-limit", change: func(s *Submission) { s.MaxHandoffs = 0 }},
		{name: "unsupported-additional-handoffs", change: func(s *Submission) { s.MaxHandoffs = 2 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newBrokerFixture(t)
			s := testSubmission("bounds", "worker")
			tc.change(&s)
			if _, err := f.b.Submit(s); err == nil {
				t.Fatal("unbounded or unsupported submission must be rejected")
			}
			if len(f.b.Snapshot().Tasks) != 0 {
				t.Fatal("rejected submission persisted a task")
			}
		})
	}
}

func TestArtifactMustExistAndMatchItsContentHash(t *testing.T) {
	f := newBrokerFixture(t)
	s := testSubmission("artifact", "worker")
	s.RequireArtifact = true
	task := submitTask(t, f.b, s)
	runID := startTask(t, f.b, task)
	done := Report{EventID: "artifact-missing", TaskID: task.ID, Endpoint: task.Recipient, State: "succeeded", RunID: runID, Summary: "Result with an artifact"}
	if err := f.b.Report(done); err == nil {
		t.Fatal("required artifact cannot be omitted")
	}
	done.Artifacts = []Artifact{{DeviceID: testDevice, WorkspaceID: "work", Path: "answer.txt"}}
	done.EventID = "artifact-nonexistent"
	if err := f.b.Report(done); err == nil {
		t.Fatal("nonexistent artifact must be rejected")
	}
	contents := []byte("Reviewed local result\n")
	if err := os.WriteFile(filepath.Join(f.workspace, "answer.txt"), contents, 0600); err != nil {
		t.Fatal(err)
	}
	done.EventID = "artifact-wrong-hash"
	done.Artifacts[0].SHA256 = strings.Repeat("0", 64)
	if err := f.b.Report(done); err == nil {
		t.Fatal("artifact whose hash differs from its actual contents must be rejected")
	}
	if f.b.Snapshot().Tasks[task.ID].Execution == "succeeded" || len(resultMessages(f.b.Snapshot(), task.ID)) != 0 {
		t.Fatal("invalid artifact completion changed task or generated a result")
	}
	done.EventID = "artifact-verified"
	done.Artifacts[0].SHA256 = ""
	if err := f.b.Report(done); err != nil {
		t.Fatalf("existing artifact: %v", err)
	}
	sum := sha256.Sum256(contents)
	stored := f.b.Snapshot().Tasks[task.ID].Artifacts
	if len(stored) != 1 || stored[0].SHA256 != hex.EncodeToString(sum[:]) {
		t.Fatalf("artifact did not record actual content hash: %+v", stored)
	}
}

func TestArtifactRejectsWorkspaceTraversalAndForeignFiles(t *testing.T) {
	for _, tc := range []struct {
		name     string
		artifact Artifact
	}{
		{name: "parent-traversal", artifact: Artifact{DeviceID: testDevice, WorkspaceID: "work", Path: "../outside.txt"}},
		{name: "unregistered-workspace", artifact: Artifact{DeviceID: testDevice, WorkspaceID: "other", Path: "outside.txt"}},
		{name: "foreign-device", artifact: Artifact{DeviceID: "other-computer", WorkspaceID: "work", Path: "outside.txt"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newBrokerFixture(t)
			for _, filename := range []string{filepath.Join(f.workspace, "outside.txt"), filepath.Join(f.workspace, "..", "outside.txt")} {
				if err := os.WriteFile(filename, []byte("existing file must still be rejected"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			s := testSubmission("escape", "worker")
			s.RequireArtifact = true
			task := submitTask(t, f.b, s)
			runID := startTask(t, f.b, task)
			if err := f.b.Report(Report{EventID: "escape-done", TaskID: task.ID, Endpoint: task.Recipient, State: "succeeded", RunID: runID, Summary: "Untrusted file", Artifacts: []Artifact{tc.artifact}}); err == nil {
				t.Fatalf("unsafe artifact must be rejected: %+v", tc.artifact)
			}
		})
	}
	t.Run("absolute-path", func(t *testing.T) {
		f := newBrokerFixture(t)
		filename := filepath.Join(f.workspace, "answer.txt")
		if err := os.WriteFile(filename, []byte("answer"), 0600); err != nil {
			t.Fatal(err)
		}
		task := submitTask(t, f.b, testSubmission("absolute", "worker"))
		runID := startTask(t, f.b, task)
		if err := f.b.Report(Report{EventID: "absolute-done", TaskID: task.ID, Endpoint: task.Recipient, State: "succeeded", RunID: runID, Summary: "Absolute path", Artifacts: []Artifact{{DeviceID: testDevice, WorkspaceID: "work", Path: filename}}}); err == nil {
			t.Fatal("absolute artifact path must be rejected even inside a workspace")
		}
	})
	t.Run("symlink-escape", func(t *testing.T) {
		f := newBrokerFixture(t)
		outside := filepath.Join(t.TempDir(), "outside.txt")
		if err := os.WriteFile(outside, []byte("private external content"), 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(outside, filepath.Join(f.workspace, "linked.txt")); err != nil {
			if runtime.GOOS == "windows" {
				t.Skipf("Windows host cannot create symlinks without the necessary privileges: %v", err)
			}
			t.Fatal(err)
		}
		task := submitTask(t, f.b, testSubmission("symlink", "worker"))
		runID := startTask(t, f.b, task)
		if err := f.b.Report(Report{EventID: "symlink-done", TaskID: task.ID, Endpoint: task.Recipient, State: "succeeded", RunID: runID, Summary: "Symlink file", Artifacts: []Artifact{{DeviceID: testDevice, WorkspaceID: "work", Path: "linked.txt"}}}); err == nil {
			t.Fatal("artifact symlink escaping the configured workspace must be rejected")
		}
	})
}

func TestOpenRejectsConcurrentBrokerForSameDirectory(t *testing.T) {
	f := newBrokerFixture(t)
	second, err := Open(f.dir, testDevice, map[string]string{"work": f.workspace})
	if err == nil {
		_ = second.Close()
		t.Fatal("same data directory was opened by two brokers")
	}
}

func TestJournalRepairsTornTailButRejectsCompleteCorruption(t *testing.T) {
	t.Run("torn-tail", func(t *testing.T) {
		f := newBrokerFixture(t)
		task := submitTask(t, f.b, testSubmission("persisted", "worker"))
		if err := f.b.Close(); err != nil {
			t.Fatal(err)
		}
		f.b = nil
		journalPath := filepath.Join(f.dir, "state.jsonl")
		original, err := os.ReadFile(journalPath)
		if err != nil {
			t.Fatal(err)
		}
		if len(original) == 0 || original[len(original)-1] != '\n' {
			t.Fatal("journal lacks complete newline-terminated records")
		}
		if err := os.WriteFile(journalPath, append(original, []byte(`{"snapshot":{"tasks":`)...), 0600); err != nil {
			t.Fatal(err)
		}
		f.b, err = Open(f.dir, testDevice, map[string]string{"work": f.workspace})
		if err != nil {
			t.Fatalf("torn final record should be repaired: %v", err)
		}
		if _, ok := f.b.Snapshot().Tasks[task.ID]; !ok {
			t.Fatal("repair discarded a fully committed task")
		}
		repaired, err := os.ReadFile(journalPath)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.HasPrefix(repaired, original) {
			t.Fatal("repair did not preserve all complete journal records")
		}
		for _, line := range bytes.Split(bytes.TrimSuffix(repaired, []byte{'\n'}), []byte{'\n'}) {
			if !json.Valid(line) {
				t.Fatal("repair retained incomplete bytes in the journal")
			}
		}
		newTask := submitTask(t, f.b, testSubmission("after-repair", "worker"))
		if err := f.b.Close(); err != nil {
			t.Fatal(err)
		}
		f.b = nil
		f.b, err = Open(f.dir, testDevice, map[string]string{"work": f.workspace})
		if err != nil {
			t.Fatalf("journal appended after repair should reopen: %v", err)
		}
		for _, id := range []string{task.ID, newTask.ID} {
			if _, ok := f.b.Snapshot().Tasks[id]; !ok {
				t.Fatalf("reopened journal lost task %s after appending to the repaired tail", id)
			}
		}
	})
	t.Run("complete-checksum-corruption", func(t *testing.T) {
		f := newBrokerFixture(t)
		submitTask(t, f.b, testSubmission("persisted", "worker"))
		if err := f.b.Close(); err != nil {
			t.Fatal(err)
		}
		f.b = nil
		journalPath := filepath.Join(f.dir, "state.jsonl")
		contents, err := os.ReadFile(journalPath)
		if err != nil {
			t.Fatal(err)
		}
		lines := bytes.Split(contents, []byte{'\n'})
		var record struct {
			Snapshot json.RawMessage `json:"snapshot"`
			Checksum string          `json:"checksum"`
		}
		if err := json.Unmarshal(lines[0], &record); err != nil {
			t.Fatal(err)
		}
		if record.Checksum == strings.Repeat("0", 64) {
			record.Checksum = strings.Repeat("1", 64)
		} else {
			record.Checksum = strings.Repeat("0", 64)
		}
		lines[0], err = json.Marshal(record)
		if err != nil {
			t.Fatal(err)
		}
		corrupt := bytes.Join(lines, []byte{'\n'})
		if err := os.WriteFile(journalPath, corrupt, 0600); err != nil {
			t.Fatal(err)
		}
		opened, err := Open(f.dir, testDevice, map[string]string{"work": f.workspace})
		if err == nil {
			_ = opened.Close()
			t.Fatal("a complete record with invalid checksum must fail closed")
		}
		unchanged, readErr := os.ReadFile(journalPath)
		if readErr != nil || !bytes.Equal(corrupt, unchanged) {
			t.Fatal("rejected corrupt journal was silently modified")
		}
	})
}

func TestLegacyJournalWithoutMessageLinksLoadsWithoutMigration(t *testing.T) {
	f := newBrokerFixture(t)
	task := submitTask(t, f.b, testSubmission("legacy-message", "worker"))
	legacy := f.b.Snapshot()
	for id, envelope := range legacy.Messages {
		envelope.ReplyTo = ""
		envelope.CorrelationID = ""
		legacy.Messages[id] = envelope
	}
	// Already unknown sessions need no ordinary restart recovery record, so
	// this specifically checks that omitted optional links do not migrate data.
	for id, session := range legacy.Sessions {
		session.State = "unknown"
		legacy.Sessions[id] = session
	}
	if err := f.b.Close(); err != nil {
		t.Fatal(err)
	}
	f.b = nil
	path := filepath.Join(f.dir, journalName)
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_TRUNC|os.O_APPEND, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if err = appendSnapshot(file, legacy); err != nil {
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
	if bytes.Contains(before, []byte(`"reply_to":`)) || bytes.Contains(before, []byte(`"correlation_id":`)) {
		t.Fatal("legacy fixture contains new optional fields")
	}
	f.b, err = Open(f.dir, testDevice, map[string]string{"work": f.workspace})
	if err != nil {
		t.Fatalf("legacy journal failed validation: %v", err)
	}
	loaded := f.b.Snapshot().Messages[taskMessageID(task.ID)]
	if loaded.TaskID != task.ID || loaded.ReplyTo != "" || loaded.CorrelationID != "" {
		t.Fatalf("loading changed legacy message links: %+v", loaded)
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("loading optional fields rewrote the legacy ledger")
	}
	if err := f.b.SetSession("worker", "idle", ""); err != nil {
		t.Fatal(err)
	}
	runID := startTask(t, f.b, task)
	if err := f.b.Report(Report{EventID: "legacy-done", TaskID: task.ID, Endpoint: task.Recipient, State: "succeeded", RunID: runID, Summary: "Legacy task completed"}); err != nil {
		t.Fatal(err)
	}
	results := resultMessages(f.b.Snapshot(), task.ID)
	if len(results) != 1 || results[0].ID != resultMessageID(task.ID) || results[0].ReplyTo != loaded.ID || results[0].CorrelationID != task.ID {
		t.Fatalf("new legacy-task result lost its reply links: %+v", results)
	}
}

func TestSubscribeWakesOnNewWorkWithoutPolling(t *testing.T) {
	f := newBrokerFixture(t)
	changes, unsubscribe := f.b.Subscribe()
	defer unsubscribe()
	submitTask(t, f.b, testSubmission("event-wakeup", "worker"))
	select {
	case <-changes:
	case <-time.After(2 * time.Second):
		t.Fatal("new work did not wake an event subscriber")
	}
	// The event is a wakeup hint; consumers read durable broker state to avoid
	// treating a coalesced notification as the task payload itself.
	if len(f.b.Snapshot().Tasks) != 1 {
		t.Fatal("event did not correspond to durable queued work")
	}
}

func TestVerifiedBusyResultReturnDoesNotDispatchOrdinaryTasks(t *testing.T) {
	for _, sessionState := range []string{"busy", "needs_user"} {
		t.Run(sessionState, func(t *testing.T) {
			f := newBrokerFixture(t)
			origin := f.b.Snapshot().Endpoints["origin"]
			origin.IdleWakeVerified = false
			origin.ResultQueueWhileBusyVerified = true
			if err := f.b.Register(origin); err != nil {
				t.Fatal(err)
			}
			if err := f.b.SetLeader("origin"); err != nil {
				t.Fatalf("verified busy return path must qualify as an entry: %v", err)
			}
			if err := f.b.SetSession("origin", sessionState, "observed-origin-run"); err != nil {
				t.Fatal(err)
			}
			queuedSubmission := testSubmission("ordinary-origin-task", "origin")
			queuedSubmission.Resources = []string{"desktop:origin"}
			queued := submitTask(t, f.b, queuedSubmission)
			workerTask := submitTask(t, f.b, testSubmission("busy-return-task", "worker"))
			runID := startTask(t, f.b, workerTask)
			if err := f.b.Report(Report{EventID: "busy-return-done", TaskID: workerTask.ID, Endpoint: "worker", State: "succeeded", RunID: runID, Summary: "Result for the busy original entry"}); err != nil {
				t.Fatal(err)
			}
			envelope := dispatchTask(t, f.b, "origin", workerTask.ID)
			if envelope.Kind != "result" || envelope.Recipient != workerTask.Origin {
				t.Fatalf("busy entry may receive only the result: %+v", envelope)
			}
			snapshot := f.b.Snapshot()
			if snapshot.Messages[taskMessageID(queued.ID)].Delivery != "pending" || snapshot.Tasks[queued.ID].Execution != "waiting" {
				t.Fatal("busy-result verification dispatched ordinary work")
			}
			if snapshot.Sessions["origin"] != (Session{State: sessionState, RunID: "observed-origin-run"}) {
				t.Fatal("result dispatch changed the observed native run")
			}
			if _, locked := snapshot.ResourceOwners["desktop:origin"]; locked {
				t.Fatal("result dispatch acquired resources for waiting work")
			}
			if err := f.b.Delivery(envelope.ID, "delivered", "verified native busy result queue"); err != nil {
				t.Fatal(err)
			}
			assertNoMessage(t, f.b, "origin")
		})
	}
}

func TestBusyResultReturnRequiresTheVerifiedModeAndKnownSession(t *testing.T) {
	for _, tc := range []struct {
		name               string
		state              string
		idleVerified       bool
		busyResultVerified bool
	}{
		{name: "busy-without-result-verification", state: "busy", idleVerified: true},
		{name: "unknown-with-result-verification", state: "unknown", busyResultVerified: true},
		{name: "busy-verification-does-not-prove-idle-wake", state: "idle", busyResultVerified: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newBrokerFixture(t)
			origin := f.b.Snapshot().Endpoints["origin"]
			origin.IdleWakeVerified = tc.idleVerified
			origin.ResultQueueWhileBusyVerified = tc.busyResultVerified
			if err := f.b.Register(origin); err != nil {
				t.Fatal(err)
			}
			run := ""
			if tc.state == "busy" {
				run = "observed-origin-run"
			}
			if err := f.b.SetSession("origin", tc.state, run); err != nil {
				t.Fatal(err)
			}
			workerTask := submitTask(t, f.b, testSubmission("blocked-result", "worker"))
			runID := startTask(t, f.b, workerTask)
			if err := f.b.Report(Report{EventID: "blocked-result-done", TaskID: workerTask.ID, Endpoint: "worker", State: "succeeded", RunID: runID, Summary: "The result remains available"}); err != nil {
				t.Fatal(err)
			}
			assertNoMessage(t, f.b, "origin")
			if f.b.Snapshot().Messages[resultMessageID(workerTask.ID)].Delivery != "pending" {
				t.Fatal("unverified return path claimed a result")
			}
		})
	}
}

func TestOriginAndLeaderRequireAtLeastOneVerifiedReturnPath(t *testing.T) {
	f := newBrokerFixture(t)
	origin := f.b.Snapshot().Endpoints["origin"]
	origin.IdleWakeVerified = false
	origin.ResultQueueWhileBusyVerified = false
	if err := f.b.Register(origin); err != nil {
		t.Fatal(err)
	}
	if err := f.b.SetLeader("origin"); err == nil {
		t.Fatal("unverified endpoint became leader")
	}
	if _, err := f.b.Submit(testSubmission("unverified-origin", "worker")); err == nil {
		t.Fatal("unverified origin accepted work without a return path")
	}
	if len(f.b.Snapshot().Tasks) != 0 {
		t.Fatal("rejected unverified-origin task was stored")
	}
}

func TestBusyResultVerificationDoesNotBypassUncertainDispatch(t *testing.T) {
	f := newBrokerFixture(t)
	origin := f.b.Snapshot().Endpoints["origin"]
	origin.ResultQueueWhileBusyVerified = true
	if err := f.b.Register(origin); err != nil {
		t.Fatal(err)
	}
	uncertainSubmission := testSubmission("uncertain-origin-work", "origin")
	uncertainSubmission.Resources = []string{"desktop:origin"}
	uncertain := submitTask(t, f.b, uncertainSubmission)
	dispatch := dispatchTask(t, f.b, "origin", uncertain.ID)
	if err := f.b.Delivery(dispatch.ID, "uncertain", "native acceptance must be reconciled"); err != nil {
		t.Fatal(err)
	}
	if err := f.b.SetSession("origin", "busy", "observed-origin-run"); err != nil {
		t.Fatal(err)
	}
	workerTask := submitTask(t, f.b, testSubmission("return-behind-uncertain", "worker"))
	runID := startTask(t, f.b, workerTask)
	if err := f.b.Report(Report{EventID: "return-behind-uncertain-done", TaskID: workerTask.ID, Endpoint: "worker", State: "succeeded", RunID: runID, Summary: "Result awaits safe delivery"}); err != nil {
		t.Fatal(err)
	}
	assertNoMessage(t, f.b, "origin")
	snapshot := f.b.Snapshot()
	if snapshot.ResourceOwners["desktop:origin"] != uncertain.ID || snapshot.Messages[dispatch.ID].Delivery != "uncertain" || snapshot.Messages[resultMessageID(workerTask.ID)].Delivery != "pending" {
		t.Fatal("busy result path bypassed an unresolved dispatch reservation")
	}
}
