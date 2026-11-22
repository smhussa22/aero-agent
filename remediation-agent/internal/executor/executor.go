// Package executor decides whether an allowlisted proposal runs, runs it under strict limits,
// and checks that it helped. The AI never reaches this code except as a validated Proposal.
package executor

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/smhussa22/aero-agent/remediation-agent/internal/actions"
	"github.com/smhussa22/aero-agent/remediation-agent/internal/audit"
)

// Mode is the per-machine permission level.
type Mode string

const (
	Observe    Mode = "observe"    // investigate and report only
	Recommend  Mode = "recommend"  // every action needs a human approval
	Autonomous Mode = "autonomous" // safe actions run alone; dangerous ones still need approval
)

type Status string

const (
	StatusObserved  Status = "observed"
	StatusRejected  Status = "rejected"
	StatusExpired   Status = "expired" // approval was not given in time
	StatusBlocked   Status = "blocked" // refused by policy, cooldown or rate limit
	StatusDryRun    Status = "dry_run"
	StatusFailed    Status = "failed"
	StatusSucceeded Status = "succeeded" // ran, but no recovery check was possible
	StatusVerified  Status = "verified"  // ran, and the alert condition cleared
	StatusEscalated Status = "escalated" // ran, but the problem persists: a human must look
)

type Config struct {
	Mode            Mode
	DryRun          bool
	AllowedServices []string // restart_service only touches these; empty means none
	ProtectedPIDs   []int    // never signalled (the agent itself and its parent)
	Sudo            bool     // prefix commands with "sudo -n --" for a least-privilege service account

	Timeout         time.Duration // per attempt
	Retries         int           // attempts in total
	BaseBackoff     time.Duration // doubles after each failed attempt
	Cooldown        time.Duration // same action and params cannot repeat within this window
	MaxPerHour      int           // executions across all actions
	ApprovalTimeout time.Duration

	VerifyWait     time.Duration // settle time before the first check
	VerifyAttempts int
	VerifyInterval time.Duration
}

// DefaultConfig is deliberately conservative: it only observes until told otherwise.
func DefaultConfig() Config {
	return Config{
		Mode: Observe, DryRun: false,
		Timeout: 30 * time.Second, Retries: 3, BaseBackoff: time.Second,
		Cooldown: 5 * time.Minute, MaxPerHour: 10, ApprovalTimeout: 15 * time.Minute,
		VerifyWait: 10 * time.Second, VerifyAttempts: 3, VerifyInterval: 10 * time.Second,
	}
}

// Runner executes one command (argv, never a shell string).
type Runner interface {
	Run(ctx context.Context, argv []string) (output string, err error)
}

// Approver asks a human. It returns whether they approved and who they were.
type Approver interface {
	Request(ctx context.Context, incidentID string, p actions.Proposal) (approved bool, by string, err error)
}

// Condition is the alert condition that should clear after a successful fix.
type Condition struct {
	Operator  string
	Threshold float64
}

func (c Condition) Holds(v float64) bool {
	switch c.Operator {
	case ">":
		return v > c.Threshold
	case ">=":
		return v >= c.Threshold
	case "<":
		return v < c.Threshold
	case "<=":
		return v <= c.Threshold
	}
	return true // unknown operator: assume the problem is still there
}

// Verification re-reads the metric that fired the alert.
type Verification struct {
	Cond  Condition
	Check func(ctx context.Context) (float64, error)
}

type Task struct {
	IncidentID string
	Proposal   actions.Proposal
	Verify     *Verification // nil means recovery cannot be checked
}

type Outcome struct {
	Status Status
	Detail string
}

type Deps struct {
	Runner   Runner
	Approver Approver // may be nil only when no approval is ever needed
	Audit    audit.Sink
	Now      func() time.Time
	Sleep    func(ctx context.Context, d time.Duration)
}

type Executor struct {
	cfg  Config
	deps Deps

	mu       sync.Mutex // one action at a time: fixes must not race each other
	lastRun  map[string]time.Time
	runTimes []time.Time
}

func New(cfg Config, deps Deps) *Executor {
	if deps.Now == nil {
		deps.Now = time.Now
	}
	if deps.Sleep == nil {
		deps.Sleep = func(ctx context.Context, d time.Duration) {
			select {
			case <-ctx.Done():
			case <-time.After(d):
			}
		}
	}
	return &Executor{cfg: cfg, deps: deps, lastRun: map[string]time.Time{}}
}

var errAudit = errors.New("audit log unavailable")

func (e *Executor) record(ctx context.Context, t Task, event, actor, detail string) error {
	err := e.deps.Audit.Record(ctx, audit.Event{
		Time: e.deps.Now().UTC(), IncidentID: t.IncidentID, ActionID: t.Proposal.ActionID,
		Event: event, Actor: actor, Detail: detail, DryRun: e.cfg.DryRun,
	})
	if err != nil {
		return fmt.Errorf("%w: %v", errAudit, err)
	}
	return nil
}

// Handle runs the full pipeline for one proposal. A returned error means the pipeline itself
// broke (for example the audit log is unwritable) and nothing further was run.
func (e *Executor) Handle(ctx context.Context, t Task) (Outcome, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	p := t.Proposal
	if err := e.record(ctx, t, "proposed", "ai", formatParams(p)); err != nil {
		return Outcome{}, err
	}

	if e.cfg.Mode == Observe || (e.cfg.Mode != Recommend && e.cfg.Mode != Autonomous) {
		return e.finish(ctx, t, StatusObserved, "observe mode: not executed")
	}

	// approval: Recommend always, Autonomous only for dangerous actions
	if e.cfg.Mode == Recommend || p.Dangerous {
		if out, done, err := e.approve(ctx, t); done || err != nil {
			return out, err
		}
	}

	if reason := e.policy(p); reason != "" {
		return e.finish(ctx, t, StatusBlocked, reason)
	}
	argv, err := e.argv(p)
	if err != nil {
		return e.finish(ctx, t, StatusBlocked, err.Error())
	}

	now := e.deps.Now()
	key := p.ActionID + "|" + canonicalParams(p)
	if last, ok := e.lastRun[key]; ok && now.Sub(last) < e.cfg.Cooldown {
		return e.finish(ctx, t, StatusBlocked, fmt.Sprintf("cooldown: ran %s ago", now.Sub(last).Round(time.Second)))
	}
	e.prune(now)
	if len(e.runTimes) >= e.cfg.MaxPerHour {
		return e.finish(ctx, t, StatusBlocked, "rate limit: too many executions this hour")
	}

	if e.cfg.DryRun {
		return e.finish(ctx, t, StatusDryRun, "would run: "+strings.Join(argv, " "))
	}

	// count the attempt before running so a crash-looping fix cannot dodge the limits
	e.lastRun[key] = now
	e.runTimes = append(e.runTimes, now)

	if err := e.record(ctx, t, "running", "agent", strings.Join(argv, " ")); err != nil {
		return Outcome{}, err
	}
	if out, ok, err := e.execute(ctx, t, argv); !ok || err != nil {
		return out, err
	}
	return e.recover(ctx, t)
}

func (e *Executor) finish(ctx context.Context, t Task, s Status, detail string) (Outcome, error) {
	if err := e.record(ctx, t, string(s), "agent", detail); err != nil {
		return Outcome{}, err
	}
	return Outcome{Status: s, Detail: detail}, nil
}

// approve returns done=true with the outcome when the action must not proceed.
func (e *Executor) approve(ctx context.Context, t Task) (Outcome, bool, error) {
	if e.deps.Approver == nil {
		out, err := e.finish(ctx, t, StatusBlocked, "approval required but no approver is configured")
		return out, true, err
	}
	if err := e.record(ctx, t, "awaiting_approval", "agent", ""); err != nil {
		return Outcome{}, true, err
	}

	actx, cancel := context.WithTimeout(ctx, e.cfg.ApprovalTimeout)
	defer cancel()
	approved, by, err := e.deps.Approver.Request(actx, t.IncidentID, t.Proposal)
	switch {
	case err != nil:
		out, ferr := e.finish(ctx, t, StatusExpired, "no approval decision: "+err.Error())
		return out, true, ferr
	case !approved:
		out, ferr := e.finish(ctx, t, StatusRejected, "rejected by "+by)
		return out, true, ferr
	}
	if err := e.record(ctx, t, "approved", by, ""); err != nil {
		return Outcome{}, true, err
	}
	return Outcome{}, false, nil
}

func (e *Executor) prune(now time.Time) {
	kept := e.runTimes[:0]
	for _, ts := range e.runTimes {
		if now.Sub(ts) < time.Hour {
			kept = append(kept, ts)
		}
	}
	e.runTimes = kept
}

// execute runs argv with a timeout, retrying failures with exponential backoff.
func (e *Executor) execute(ctx context.Context, t Task, argv []string) (Outcome, bool, error) {
	backoff := e.cfg.BaseBackoff
	var lastErr error
	for attempt := 1; attempt <= e.cfg.Retries; attempt++ {
		rctx, cancel := context.WithTimeout(ctx, e.cfg.Timeout)
		out, err := e.deps.Runner.Run(rctx, argv)
		cancel()

		if err == nil || alreadyDone(t.Proposal, out) {
			if rerr := e.record(ctx, t, "succeeded", "agent", fmt.Sprintf("attempt %d", attempt)); rerr != nil {
				return Outcome{}, false, rerr
			}
			return Outcome{}, true, nil
		}

		lastErr = err
		if rerr := e.record(ctx, t, "attempt_failed", "agent", fmt.Sprintf("attempt %d: %v", attempt, err)); rerr != nil {
			return Outcome{}, false, rerr
		}
		if attempt < e.cfg.Retries {
			e.deps.Sleep(ctx, backoff)
			backoff *= 2
		}
	}

	detail := fmt.Sprintf("failed after %d attempts: %v", e.cfg.Retries, lastErr)
	if err := e.record(ctx, t, "escalated", "agent", detail); err != nil {
		return Outcome{}, false, err
	}
	return Outcome{Status: StatusFailed, Detail: detail}, false, nil
}

// recover checks that the alert condition cleared, and escalates if it did not.
func (e *Executor) recover(ctx context.Context, t Task) (Outcome, error) {
	if t.Verify == nil || t.Verify.Check == nil {
		return e.finish(ctx, t, StatusSucceeded, "ran; recovery could not be checked")
	}

	e.deps.Sleep(ctx, e.cfg.VerifyWait)
	var last string
	for i := 0; i < e.cfg.VerifyAttempts; i++ {
		v, err := t.Verify.Check(ctx)
		switch {
		case err != nil:
			last = "check failed: " + err.Error() // an unreadable metric is never treated as recovered
		case !t.Verify.Cond.Holds(v):
			return e.finish(ctx, t, StatusVerified, fmt.Sprintf("condition cleared (value %.2f)", v))
		default:
			last = fmt.Sprintf("still firing (value %.2f)", v)
		}
		if i < e.cfg.VerifyAttempts-1 {
			e.deps.Sleep(ctx, e.cfg.VerifyInterval)
		}
	}
	return e.finish(ctx, t, StatusEscalated, "problem persists after the fix: "+last)
}
