package link

import (
	"context"
	"regexp"
	"testing"
	"time"

	aerov1 "github.com/smhussa22/aero-agent/backend/gen/aerov1"
	"github.com/smhussa22/aero-agent/remediation-agent/internal/actions"
	"github.com/smhussa22/aero-agent/remediation-agent/internal/audit"
	"github.com/smhussa22/aero-agent/remediation-agent/internal/executor"
)

func TestServerCanNeverRaiseModeAboveTheLocalCeiling(t *testing.T) {
	for _, c := range []struct{ server, ceiling, want executor.Mode }{
		{executor.Autonomous, executor.Observe, executor.Observe},
		{executor.Autonomous, executor.Recommend, executor.Recommend},
		{executor.Autonomous, executor.Autonomous, executor.Autonomous},
		{executor.Recommend, executor.Autonomous, executor.Recommend}, // the server may always lower it
		{executor.Observe, executor.Autonomous, executor.Observe},
		{"garbage", executor.Autonomous, "garbage"}, // unknown ranks lowest, and the executor treats it as observe
	} {
		if got := EffectiveMode(c.server, c.ceiling); got != c.want {
			t.Errorf("server=%s ceiling=%s got %s, want %s", c.server, c.ceiling, got, c.want)
		}
	}
}

func TestNewUUIDIsValidV4AndUnique(t *testing.T) {
	re := regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	seen := map[string]bool{}
	for range 50 {
		id := newUUID()
		if !re.MatchString(id) || seen[id] {
			t.Fatalf("bad or repeated uuid %q", id)
		}
		seen[id] = true
	}
}

func TestApprovalIsMatchedToTheWaitingProposal(t *testing.T) {
	l := New(Config{})
	done := make(chan bool, 1)
	go func() {
		ok, by, err := l.Request(context.Background(), "inc-1", "prop-1", actions.Proposal{})
		done <- ok && by == "alice" && err == nil
	}()
	time.Sleep(50 * time.Millisecond)

	l.deliver(&aerov1.ApprovalDecision{IncidentId: "inc-1", ProposalId: "someone-else", Approved: true, DecidedBy: "mallory"})
	select {
	case <-done:
		t.Fatal("a decision for another proposal must not release this one")
	case <-time.After(100 * time.Millisecond):
	}

	l.deliver(&aerov1.ApprovalDecision{IncidentId: "inc-1", ProposalId: "prop-1", Approved: true, DecidedBy: "alice"})
	if !<-done {
		t.Fatal("the matching decision should approve")
	}
}

func TestApprovalForAnotherIncidentIsRefused(t *testing.T) {
	l := New(Config{})
	done := make(chan error, 1)
	go func() {
		_, _, err := l.Request(context.Background(), "inc-1", "prop-1", actions.Proposal{})
		done <- err
	}()
	time.Sleep(50 * time.Millisecond)
	l.deliver(&aerov1.ApprovalDecision{IncidentId: "inc-OTHER", ProposalId: "prop-1", Approved: true})
	if err := <-done; err == nil {
		t.Fatal("mismatched incident must not be treated as approval")
	}
}

func TestRequestEndsWhenContextExpires(t *testing.T) {
	l := New(Config{})
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, _, err := l.Request(ctx, "i", "p", actions.Proposal{}); err == nil {
		t.Fatal("an unanswered request must end with an error, never an approval")
	}
	if len(l.pending) != 0 {
		t.Fatal("pending entry leaked")
	}
}

func TestRecordMirrorsOnlyKnownEventsWithAProposal(t *testing.T) {
	l := New(Config{})
	var got []*aerov1.ActionUpdate
	l.send = func(m *aerov1.AgentMessage) bool { got = append(got, m.GetActionUpdate()); return true }

	l.Record(context.Background(), audit.Event{Event: "running", IncidentID: "i", ProposalID: "p", ActionID: "restart_service"})
	l.Record(context.Background(), audit.Event{Event: "attempt_failed", IncidentID: "i", ProposalID: "p"}) // not mirrored
	l.Record(context.Background(), audit.Event{Event: "running", IncidentID: "i"})                         // no proposal id
	l.Record(context.Background(), audit.Event{Event: "dry_run", IncidentID: "i", ProposalID: "p", DryRun: true})

	if len(got) != 2 || got[0].Status != aerov1.ActionStatus_ACTION_STATUS_RUNNING || !got[1].DryRun {
		t.Fatalf("got %v", got)
	}
}

func TestEveryExecutorEventIsMapped(t *testing.T) {
	// every status string the executor writes into the audit log must reach the backend or be deliberately skipped
	skipped := map[string]bool{"attempt_failed": true}
	for _, ev := range []string{"proposed", "observed", "awaiting_approval", "approved", "rejected", "expired",
		"blocked", "dry_run", "running", "succeeded", "verified", "escalated", "attempt_failed"} {
		if _, ok := statusFor[ev]; !ok && !skipped[ev] {
			t.Errorf("event %q is neither mapped nor skipped", ev)
		}
	}
}
