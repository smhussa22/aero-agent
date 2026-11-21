package alert

import (
	"testing"
	"time"

	aerov1 "github.com/smhussa22/aero-agent/backend/gen/aerov1"
	"github.com/smhussa22/aero-agent/backend/internal/store"
)

var t0 = time.Unix(1_700_000_000, 0)

func cpu(v float64) []store.Row {
	return []store.Row{{Metric: "cpu.usage_percent", Value: v}}
}

func TestFiresOnceAfterDurationThenResolves(t *testing.T) {
	e := NewEvaluator([]Rule{{ID: "r1", Metric: "cpu.usage_percent", Operator: ">", Threshold: 90,
		Duration: 30 * time.Second, Severity: aerov1.Severity_SEVERITY_CRITICAL}})

	if ev := e.Process("h", t0, cpu(95)); len(ev) != 0 {
		t.Fatal("must not fire before the duration has elapsed")
	}
	ev := e.Process("h", t0.Add(30*time.Second), cpu(96))
	if len(ev) != 1 || ev[0].Resolved || ev[0].Value != 96 || ev[0].HostId != "h" {
		t.Fatalf("want one fire event, got %v", ev)
	}
	if ev2 := e.Process("h", t0.Add(40*time.Second), cpu(97)); len(ev2) != 0 {
		t.Fatal("must not re-fire while still firing")
	}

	res := e.Process("h", t0.Add(50*time.Second), cpu(10))
	if len(res) != 1 || !res[0].Resolved || res[0].AlertId != ev[0].AlertId {
		t.Fatalf("want a resolve for the same alert, got %v", res)
	}
}

func TestBlipBelowDurationNeverFires(t *testing.T) {
	e := NewEvaluator([]Rule{{ID: "r1", Metric: "cpu.usage_percent", Operator: ">", Threshold: 90, Duration: 30 * time.Second}})
	e.Process("h", t0, cpu(95))
	e.Process("h", t0.Add(10*time.Second), cpu(5)) // recovers, timer resets
	if ev := e.Process("h", t0.Add(35*time.Second), cpu(95)); len(ev) != 0 {
		t.Fatalf("timer should have restarted, got %v", ev)
	}
}

func TestHostAndLabelScoping(t *testing.T) {
	e := NewEvaluator([]Rule{{ID: "r1", HostID: "only-this", Metric: "disk.used_percent", Label: "/", Operator: ">=", Threshold: 80}})
	rows := []store.Row{{Metric: "disk.used_percent", Label: "/data", Value: 99}, {Metric: "disk.used_percent", Label: "/", Value: 85}}

	if ev := e.Process("other", t0, rows); len(ev) != 0 {
		t.Fatal("rule for another host must not fire")
	}
	ev := e.Process("only-this", t0, rows)
	if len(ev) != 1 {
		t.Fatalf("only the / mount matches, got %d events", len(ev))
	}
}

func TestOperators(t *testing.T) {
	for _, c := range []struct {
		op   string
		v    float64
		want bool
	}{{">", 5, false}, {">=", 5, true}, {"<", 5, false}, {"<=", 5, true}, {"??", 1, false}} {
		if got := (Rule{Operator: c.op, Threshold: 5}).holds(c.v); got != c.want {
			t.Errorf("%v %s 5 = %v", c.v, c.op, got)
		}
	}
}

func TestSetRulesDropsStateOfRemovedRules(t *testing.T) {
	e := NewEvaluator([]Rule{{ID: "r1", Metric: "cpu.usage_percent", Operator: ">", Threshold: 1}})
	e.Process("h", t0, cpu(50))
	e.SetRules(nil)
	if len(e.states) != 0 {
		t.Fatal("state for a removed rule must be dropped")
	}
}
