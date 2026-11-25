package orchestrator

import (
	"context"
	"testing"

	"google.golang.org/protobuf/proto"

	aerov1 "github.com/smhussa22/aero-agent/backend/gen/aerov1"
	"github.com/smhussa22/aero-agent/backend/internal/testdb"
)

func alertBytes(t *testing.T, ev *aerov1.AlertEvent) []byte {
	t.Helper()
	b, err := proto.Marshal(ev)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// An incident's organization comes from the host's own record. Nothing an alert message says about
// organizations or rules can move an incident into, or link it to, another organization.
func TestIncidentsStayInTheirHostsOrganization(t *testing.T) {
	pool := testdb.Open(t)
	a := testdb.Seed(t, pool, "a")
	b := testdb.Seed(t, pool, "b")
	svc := NewService(pool)
	ctx := context.Background()

	// an alert for A's host that claims to belong to B and cites B's rule
	forged := &aerov1.AlertEvent{AlertId: "forged-" + a.Marker, HostId: a.HostID, OrgId: b.OrgID, RuleId: b.RuleID,
		Metric: "cpu.usage_percent", Value: 99, Threshold: 90}
	if err := svc.HandleAlert(ctx, alertBytes(t, forged)); err != nil {
		t.Fatal(err)
	}
	var org string
	var rule *string
	if err := pool.QueryRow(ctx, "SELECT org_id::text, rule_id::text FROM incidents WHERE alert_id = $1", forged.AlertId).Scan(&org, &rule); err != nil {
		t.Fatalf("incident was not created: %v", err)
	}
	if org != a.OrgID {
		t.Errorf("incident landed in organization %s, want its host's organization %s", org, a.OrgID)
	}
	if rule != nil {
		t.Errorf("incident was linked to another organization's rule %s", *rule)
	}
	var audited string
	if err := pool.QueryRow(ctx, "SELECT org_id::text FROM audit_log WHERE event = 'incident_opened' AND incident_id = (SELECT id FROM incidents WHERE alert_id = $1)", forged.AlertId).Scan(&audited); err != nil || audited != a.OrgID {
		t.Errorf("audit entry organization = %q (%v), want %s", audited, err, a.OrgID)
	}

	// an alert for a host that does not exist opens nothing
	ghost := &aerov1.AlertEvent{AlertId: "ghost-" + a.Marker, HostId: "no-such-host", OrgId: a.OrgID}
	if err := svc.HandleAlert(ctx, alertBytes(t, ghost)); err != nil {
		t.Fatal(err)
	}
	var n int
	pool.QueryRow(ctx, "SELECT count(*) FROM incidents WHERE alert_id = $1", ghost.AlertId).Scan(&n)
	if n != 0 {
		t.Errorf("an alert for an unknown host created %d incidents", n)
	}

	// a resolve for A's alert closes A's incident only
	if err := svc.HandleAlert(ctx, alertBytes(t, &aerov1.AlertEvent{AlertId: forged.AlertId, HostId: a.HostID, Resolved: true})); err != nil {
		t.Fatal(err)
	}
	var status, other string
	pool.QueryRow(ctx, "SELECT status FROM incidents WHERE alert_id = $1", forged.AlertId).Scan(&status)
	pool.QueryRow(ctx, "SELECT status FROM incidents WHERE id = $1::uuid", b.Incident).Scan(&other)
	if status != "resolved" || other == "resolved" {
		t.Errorf("resolve: A's incident %q (want resolved), B's incident %q (want untouched)", status, other)
	}
}

// An incident that opened before its agent connected is handed to the agent when it connects, once, and only
// while nobody has reported on it.
func TestAgentGetsIncidentsItMissed(t *testing.T) {
	pool := testdb.Open(t)
	a := testdb.Seed(t, pool, "replay")
	svc := NewService(pool)
	ctx := context.Background()

	ev := &aerov1.AlertEvent{AlertId: "missed-" + a.Marker, HostId: a.HostID, Metric: "cpu.usage_percent", Value: 99, Threshold: 90}
	if err := svc.HandleAlert(ctx, alertBytes(t, ev)); err != nil { // no agent connected: nothing is sent
		t.Fatal(err)
	}

	out := make(chan *aerov1.OrchestratorMessage, queueSize)
	svc.reg.register(a.HostID, out)
	svc.replayOpen(ctx, a.HostID)
	select {
	case m := <-out:
		inv := m.GetInvestigate()
		if inv == nil || inv.GetAlert().GetAlertId() != ev.AlertId || inv.GetAlert().GetMetric() != "cpu.usage_percent" || inv.GetAlert().GetValue() != 99 {
			t.Fatalf("replayed message is wrong: %v", m)
		}
	default:
		t.Fatal("the missed incident was not replayed")
	}

	// once reported on, it is not sent again
	if _, err := pool.Exec(ctx, "UPDATE incidents SET status = 'investigating' WHERE alert_id = $1", ev.AlertId); err != nil {
		t.Fatal(err)
	}
	svc.replayOpen(ctx, a.HostID)
	select {
	case m := <-out:
		t.Fatalf("an incident already under investigation was replayed: %v", m)
	default:
	}
}
