package manager

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestSessionObservationIdenticalConcurrentReportsAreNoOp(t *testing.T) {
	f := newBrokerFixture(t)
	changes, cancel := f.b.Subscribe()
	defer cancel()
	journalSize := func() int64 {
		t.Helper()
		info, err := os.Stat(filepath.Join(f.dir, journalName))
		if err != nil {
			t.Fatal(err)
		}
		return info.Size()
	}
	before := journalSize()
	var wg sync.WaitGroup
	results := make(chan error, 32)
	for i := 0; i < cap(results); i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results <- f.b.ObserveSession("worker", "session-worker", "idle", "")
		}()
	}
	wg.Wait()
	close(results)
	for err := range results {
		if err != nil {
			t.Fatal(err)
		}
	}
	if journalSize() != before {
		t.Fatal("identical observations appended journal snapshots")
	}
	select {
	case <-changes:
		t.Fatal("identical observations broadcast a change")
	default:
	}
	if err := f.b.ObserveSession("worker", "session-worker", "busy", "actual-turn"); err != nil {
		t.Fatal(err)
	}
	if journalSize() <= before {
		t.Fatal("changed observation was not durable")
	}
	select {
	case <-changes:
	default:
		t.Fatal("changed observation did not notify subscribers")
	}
	before = journalSize()
	if err := f.b.ObserveSession("worker", "session-worker", "busy", "actual-turn"); err != nil {
		t.Fatal(err)
	}
	if journalSize() != before {
		t.Fatal("identical busy/run observation appended a snapshot")
	}
	select {
	case <-changes:
		t.Fatal("identical busy/run observation broadcast a change")
	default:
	}
}

func reopenSessionFixture(t *testing.T, f *brokerFixture) {
	t.Helper()
	if err := f.b.Close(); err != nil {
		t.Fatal(err)
	}
	var err error
	f.b, err = Open(f.dir, testDevice, map[string]string{"work": f.workspace})
	if err != nil {
		t.Fatal(err)
	}
}

func TestSessionObservationRecoversPendingMailAfterRestart(t *testing.T) {
	f := mailFixture(t)
	message := publishMail(t, f.b, "origin", rootMail("restart-session-pending", "worker"))
	reopenSessionFixture(t, f)
	if f.b.Snapshot().Sessions["worker"].State != "unknown" {
		t.Fatal("restart retained an unconfirmed native state")
	}
	if got, err := f.b.NextMail("worker"); err != nil || got != nil {
		t.Fatalf("unknown native state claimed mail: %+v %v", got, err)
	}
	before := f.b.Snapshot()
	if err := f.b.ObserveSession("worker", "session-worker", "idle", ""); err != nil {
		t.Fatal(err)
	}
	before.Sessions["worker"] = Session{State: "idle"}
	if !sameJSON(before, f.b.Snapshot()) {
		t.Fatal("observing the native session changed binding, capability, tasks or delivery")
	}
	claimMail(t, f.b, "worker", message.ID)
	if err := f.b.ObserveSession("worker", "session-worker", "idle", ""); err != nil {
		t.Fatal(err)
	}
	if got, err := f.b.NextMail("worker"); err != nil || got != nil {
		t.Fatalf("observed idle cleared an in-flight delivery: %+v %v", got, err)
	}
}

func TestSessionObservationPreservesUncertainDeliveryAfterRestart(t *testing.T) {
	f := mailFixture(t)
	first := publishMail(t, f.b, "origin", rootMail("restart-session-inflight", "worker"))
	second := publishMail(t, f.b, "origin", rootMail("restart-session-next", "worker"))
	claimMail(t, f.b, "worker", first.ID)
	reopenSessionFixture(t, f)
	before := f.b.Snapshot()
	if before.Messages[first.ID].Delivery != "uncertain" || before.Messages[second.ID].Delivery != "pending" {
		t.Fatal("restart lost delivery evidence")
	}
	if err := f.b.ObserveSession("worker", "session-worker", "idle", ""); err != nil {
		t.Fatal(err)
	}
	before.Sessions["worker"] = Session{State: "idle"}
	if !sameJSON(before, f.b.Snapshot()) {
		t.Fatal("native idle observation cleared uncertain delivery evidence")
	}
	if got, err := f.b.NextMail("worker"); err != nil || got != nil {
		t.Fatalf("uncertain delivery no longer fenced the next claim: %+v %v", got, err)
	}
}

func TestSessionObservationCannotReconcileUnfinishedManagedRun(t *testing.T) {
	f := newBrokerFixture(t)
	submission := testSubmission("observe-unfinished-legacy", "worker")
	submission.Resources = []string{"workspace:work"}
	task := submitTask(t, f.b, submission)
	dispatchTask(t, f.b, "worker", task.ID)
	if err := f.b.Report(Report{EventID: "observe-accepted", TaskID: task.ID, Endpoint: "worker", State: "accepted", RunID: "actual-managed-turn"}); err != nil {
		t.Fatal(err)
	}
	reopenSessionFixture(t, f)
	before := f.b.Snapshot()
	for _, pair := range []Session{{State: "idle"}, {State: "busy", RunID: "different-turn"}} {
		if err := f.b.ObserveSession("worker", "session-worker", pair.State, pair.RunID); err == nil {
			t.Fatal("observation cleared an unfinished managed run")
		}
		if !sameJSON(before, f.b.Snapshot()) {
			t.Fatal("rejected observation changed managed execution or resources")
		}
	}
	if err := f.b.ObserveSession("worker", "session-worker", "busy", "actual-managed-turn"); err != nil {
		t.Fatal(err)
	}
	before.Sessions["worker"] = Session{State: "busy", RunID: "actual-managed-turn"}
	if !sameJSON(before, f.b.Snapshot()) {
		t.Fatal("matching native run observation synthesized a report or released resources")
	}
}
