package manager

import "testing"

func TestNativeQueueEligibilityDoesNotGrantBusyOrApprovalBypass(t *testing.T) {
	for _, test := range []struct {
		state                 string
		busyVerified, allowed bool
	}{
		{"unknown", false, true}, {"idle", false, true}, {"busy", false, false}, {"needs_user", false, false},
		{"busy", true, true}, {"needs_user", true, false},
	} {
		t.Run(test.state+map[bool]string{false: "-queue-only", true: "-separately-busy-verified"}[test.busyVerified], func(t *testing.T) {
			f := mailFixture(t)
			e := f.b.Snapshot().Endpoints["worker"]
			e.Modes = []string{"queue"}
			e.IdleWakeVerified = false
			e.MailboxWhileBusyVerified = test.busyVerified
			e.MailboxNativeQueueVerified = true
			if err := f.b.Register(e); err != nil {
				t.Fatal(err)
			}
			run := ""
			if test.state == "busy" || test.state == "needs_user" {
				run = "actual-native-turn"
			}
			if err := f.b.SetSession("worker", test.state, run); err != nil {
				t.Fatal(err)
			}
			message := publishMail(t, f.b, "origin", rootMail("native-queue-safety", "worker"))
			got, err := f.b.NextMail("worker")
			if err != nil {
				t.Fatal(err)
			}
			if test.allowed {
				if got == nil || got.ID != message.ID {
					t.Fatal("verified queue did not retain its permitted delivery behavior")
				}
			} else if got != nil || f.b.Snapshot().Messages[message.ID].Delivery != "pending" {
				t.Fatal("native queue grant bypassed known busy or needs-user gate")
			}
			if state := f.b.Snapshot().Sessions["worker"]; state.State != test.state || state.RunID != run {
				t.Fatal("native queue eligibility fabricated native state")
			}
		})
	}
}
