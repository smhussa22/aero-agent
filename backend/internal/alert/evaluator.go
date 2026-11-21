// Package alert evaluates rules against incoming metric rows and emits fire/resolve events.
package alert

import (
	"crypto/rand"
	"encoding/hex"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	aerov1 "github.com/smhussa22/aero-agent/backend/gen/aerov1"
	"github.com/smhussa22/aero-agent/backend/internal/store"
)

// Rule mirrors a row of alert_rules.
type Rule struct {
	ID        string
	HostID    string // empty applies to every host
	Metric    string
	Label     string // empty matches any label
	Operator  string // > >= < <=
	Threshold float64
	Duration  time.Duration // condition must hold this long before firing
	Severity  aerov1.Severity
}

func (r Rule) holds(v float64) bool {
	switch r.Operator {
	case ">":
		return v > r.Threshold
	case ">=":
		return v >= r.Threshold
	case "<":
		return v < r.Threshold
	case "<=":
		return v <= r.Threshold
	}
	return false
}

type key struct{ rule, host, label string }

type state struct {
	since   time.Time // when the condition first held
	firing  bool
	alertID string // shared by the fire event and its later resolve event
}

// Evaluator keeps per-(rule, host, label) state. It is not safe for concurrent use;
// feed it from one consumer goroutine.
type Evaluator struct {
	rules  []Rule
	states map[key]*state
}

func NewEvaluator(rules []Rule) *Evaluator {
	return &Evaluator{rules: rules, states: map[key]*state{}}
}

// SetRules swaps the rule set. State for rules that no longer exist is dropped without
// a resolve event, since the rule that would have owned it is gone.
func (e *Evaluator) SetRules(rules []Rule) {
	live := map[string]bool{}
	for _, r := range rules {
		live[r.ID] = true
	}
	for k := range e.states {
		if !live[k.rule] {
			delete(e.states, k)
		}
	}
	e.rules = rules
}

func newID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// Process evaluates one host's rows sampled at ts and returns the events that result.
func (e *Evaluator) Process(host string, ts time.Time, rows []store.Row) []*aerov1.AlertEvent {
	var events []*aerov1.AlertEvent

	for _, r := range e.rules {
		if r.HostID != "" && r.HostID != host {
			continue
		}
		for _, row := range rows {
			if row.Metric != r.Metric || (r.Label != "" && row.Label != r.Label) {
				continue
			}

			k := key{r.ID, host, row.Label}
			st := e.states[k]
			event := func(resolved bool, id string) *aerov1.AlertEvent {
				return &aerov1.AlertEvent{
					AlertId: id, RuleId: r.ID, HostId: host, Severity: r.Severity,
					Metric: r.Metric, Value: row.Value, Threshold: r.Threshold,
					Resolved: resolved, FiredAt: timestamppb.New(ts),
				}
			}

			if !r.holds(row.Value) {
				if st != nil && st.firing {
					events = append(events, event(true, st.alertID))
				}
				delete(e.states, k)
				continue
			}

			if st == nil {
				st = &state{since: ts}
				e.states[k] = st
			}
			if !st.firing && ts.Sub(st.since) >= r.Duration {
				st.firing = true
				st.alertID = newID()
				events = append(events, event(false, st.alertID))
			}
		}
	}
	return events
}
