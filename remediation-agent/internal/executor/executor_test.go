package executor

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/smhussa22/aero-agent/remediation-agent/internal/actions"
	"github.com/smhussa22/aero-agent/remediation-agent/internal/audit"
)

// ---- fakes ----

type fakeRunner struct {
	calls  [][]string
	errs   []error // consumed one per call; nil entries succeed
	output string
}

func (f *fakeRunner) Run(ctx context.Context, argv []string) (string, error) {
	f.calls = append(f.calls, argv)
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if len(f.errs) > 0 {
		err := f.errs[0]
		f.errs = f.errs[1:]
		return f.output, err
	}
	return f.output, nil
}

type fakeApprover struct {
	approved bool
	err      error
	asked    int
}

func (f *fakeApprover) Request(context.Context, string, actions.Proposal) (bool, string, error) {
	f.asked++
	return f.approved, "alice", f.err
}

type memAudit struct {
	events []audit.Event
	fail   bool
}

func (m *memAudit) Record(_ context.Context, e audit.Event) error {
	if m.fail {
		return errors.New("disk full")
	}
	m.events = append(m.events, e)
	return nil
}

func (m *memAudit) names() string {
	var n []string
	for _, e := range m.events {
		n = append(n, e.Event)
	}
	return strings.Join(n, ",")
}

type harness struct {
	ex     *Executor
	run    *fakeRunner
	appr   *fakeApprover
	audit  *memAudit
	now    time.Time
	sleeps []time.Duration
}

func setup(mode Mode, tweak func(*Config)) *harness {
	cfg := DefaultConfig()
	cfg.Mode = mode
	cfg.AllowedServices = []string{"nginx"}
	cfg.ProtectedPIDs = []int{4242}
	cfg.VerifyAttempts = 2
	if tweak != nil {
		tweak(&cfg)
	}
	h := &harness{run: &fakeRunner{}, appr: &fakeApprover{approved: true}, audit: &memAudit{}, now: time.Unix(1_700_000_000, 0)}
	h.ex = New(cfg, Deps{
		Runner: h.run, Approver: h.appr, Audit: h.audit,
		Now:   func() time.Time { return h.now },
		Sleep: func(_ context.Context, d time.Duration) { h.sleeps = append(h.sleeps, d) },
	})
	return h
}

func restart(svc string) Task {
	return Task{IncidentID: "i1", Proposal: actions.Proposal{ActionID: "restart_service", Params: map[string]string{"service": svc}}}
}

func kill(pid string) Task {
	return Task{IncidentID: "i1", Proposal: actions.Proposal{ActionID: "kill_process", Params: map[string]string{"pid": pid}, Dangerous: true}}
}

func handle(t *testing.T, h *harness, task Task) Outcome {
	t.Helper()
	out, err := h.ex.Handle(context.Background(), task)
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	return out
}

// ---- modes and approvals ----

func TestObserveNeverExecutesOrAsks(t *testing.T) {
	h := setup(Observe, nil)
	if out := handle(t, h, restart("nginx")); out.Status != StatusObserved || len(h.run.calls) != 0 || h.appr.asked != 0 {
		t.Fatalf("out=%+v calls=%d asked=%d", out, len(h.run.calls), h.appr.asked)
	}
}

func TestUnknownModeFailsSafeToObserve(t *testing.T) {
	h := setup("yolo", nil)
	if out := handle(t, h, restart("nginx")); out.Status != StatusObserved || len(h.run.calls) != 0 {
		t.Fatalf("out=%+v", out)
	}
}

func TestRecommendAlwaysNeedsApproval(t *testing.T) {
	h := setup(Recommend, nil)
	h.appr.approved = false
	if out := handle(t, h, restart("nginx")); out.Status != StatusRejected || len(h.run.calls) != 0 {
		t.Fatalf("rejected: %+v", out)
	}

	h = setup(Recommend, nil)
	if out := handle(t, h, restart("nginx")); out.Status != StatusSucceeded || len(h.run.calls) != 1 || h.appr.asked != 1 {
		t.Fatalf("approved: %+v calls=%d", out, len(h.run.calls))
	}
	if !strings.Contains(h.audit.names(), "approved") {
		t.Errorf("approval not audited: %s", h.audit.names())
	}
}

func TestAutonomousRunsSafeActionsAloneButGatesDangerousOnes(t *testing.T) {
	h := setup(Autonomous, nil)
	if out := handle(t, h, restart("nginx")); out.Status != StatusSucceeded || h.appr.asked != 0 {
		t.Fatalf("safe action: %+v asked=%d", out, h.appr.asked)
	}

	h.appr.approved = false
	if out := handle(t, h, kill("999")); out.Status != StatusRejected || h.appr.asked != 1 || len(h.run.calls) != 1 {
		t.Fatalf("dangerous action must ask and honour a no: %+v", out)
	}
}

func TestApprovalTimeoutAndMissingApprover(t *testing.T) {
	h := setup(Recommend, nil)
	h.appr.err = context.DeadlineExceeded
	if out := handle(t, h, restart("nginx")); out.Status != StatusExpired || len(h.run.calls) != 0 {
		t.Fatalf("timeout: %+v", out)
	}

	h = setup(Recommend, nil)
	h.ex = New(DefaultConfig(), Deps{Runner: h.run, Audit: h.audit})
	h.ex.cfg.Mode = Recommend
	if out := handle(t, h, restart("nginx")); out.Status != StatusBlocked || len(h.run.calls) != 0 {
		t.Fatalf("no approver must block, not run: %+v", out)
	}
}

// ---- policy, cooldown, rate limit, dry run ----

func TestPolicyBlocks(t *testing.T) {
	h := setup(Autonomous, nil)
	if out := handle(t, h, restart("postgresql")); out.Status != StatusBlocked {
		t.Fatalf("service not on the allowed list: %+v", out)
	}
	if out := handle(t, h, restart("nginx.service")); out.Status != StatusSucceeded {
		t.Fatalf(".service suffix should match: %+v", out)
	}
	if out := handle(t, h, kill("4242")); out.Status == StatusSucceeded || strings.Contains(h.audit.names(), "running,succeeded,running") {
		t.Fatalf("protected pid ran: %+v", out)
	}

	h = setup(Autonomous, func(c *Config) { c.AllowedServices = nil })
	if out := handle(t, h, restart("nginx")); out.Status != StatusBlocked || len(h.run.calls) != 0 {
		t.Fatalf("empty allowlist must allow nothing: %+v", out)
	}
}

func TestCooldownBlocksRepeatThenExpires(t *testing.T) {
	h := setup(Autonomous, nil)
	handle(t, h, restart("nginx"))

	h.now = h.now.Add(time.Minute)
	if out := handle(t, h, restart("nginx")); out.Status != StatusBlocked || !strings.Contains(out.Detail, "cooldown") {
		t.Fatalf("repeat within cooldown: %+v", out)
	}
	h.appr.approved = true
	if out := handle(t, h, Task{IncidentID: "i1", Proposal: actions.Proposal{ActionID: "vacuum_journal", Params: map[string]string{"days": "7"}}}); out.Status != StatusSucceeded {
		t.Fatalf("a different action is unaffected: %+v", out)
	}

	h.now = h.now.Add(10 * time.Minute)
	if out := handle(t, h, restart("nginx")); out.Status != StatusSucceeded {
		t.Fatalf("after cooldown: %+v", out)
	}
}

func TestRateLimitPerHour(t *testing.T) {
	h := setup(Autonomous, func(c *Config) { c.MaxPerHour = 2; c.Cooldown = 0 })
	handle(t, h, restart("nginx"))
	handle(t, h, restart("nginx"))
	if out := handle(t, h, restart("nginx")); out.Status != StatusBlocked || !strings.Contains(out.Detail, "rate limit") {
		t.Fatalf("third: %+v", out)
	}
	h.now = h.now.Add(61 * time.Minute)
	if out := handle(t, h, restart("nginx")); out.Status != StatusSucceeded {
		t.Fatalf("window should have slid: %+v", out)
	}
}

func TestDryRunNeverExecutesAndDoesNotConsumeLimits(t *testing.T) {
	h := setup(Autonomous, func(c *Config) { c.DryRun = true })
	out := handle(t, h, restart("nginx"))
	if out.Status != StatusDryRun || len(h.run.calls) != 0 || !strings.Contains(out.Detail, "systemctl restart -- nginx") {
		t.Fatalf("out=%+v calls=%d", out, len(h.run.calls))
	}
	if len(h.ex.runTimes) != 0 || len(h.ex.lastRun) != 0 || !h.audit.events[len(h.audit.events)-1].DryRun {
		t.Fatal("dry run must not count as an execution and must be flagged in the audit log")
	}
}

// ---- execution ----

func TestCommandsAreExactArgvNeverShellStrings(t *testing.T) {
	h := setup(Autonomous, func(c *Config) { c.Sudo = true; c.Cooldown = 0 })
	handle(t, h, restart("nginx"))
	handle(t, h, Task{IncidentID: "i", Proposal: actions.Proposal{ActionID: "vacuum_journal", Params: map[string]string{"days": "7"}}})
	handle(t, h, Task{IncidentID: "i", Proposal: actions.Proposal{ActionID: "kill_process", Params: map[string]string{"pid": "77", "signal": "KILL"}, Dangerous: true}})

	want := []string{
		"sudo -n -- systemctl restart -- nginx",
		"sudo -n -- journalctl --vacuum-time=7d",
		"sudo -n -- kill -s KILL -- 77",
	}
	for i, w := range want {
		if got := strings.Join(h.run.calls[i], " "); got != w {
			t.Errorf("call %d = %q, want %q", i, got, w)
		}
	}
}

func TestRetriesWithExponentialBackoff(t *testing.T) {
	h := setup(Autonomous, nil)
	h.run.errs = []error{errors.New("boom"), errors.New("boom"), nil}
	out := handle(t, h, restart("nginx"))
	if out.Status != StatusSucceeded || len(h.run.calls) != 3 {
		t.Fatalf("out=%+v calls=%d", out, len(h.run.calls))
	}
	if len(h.sleeps) != 2 || h.sleeps[0] != time.Second || h.sleeps[1] != 2*time.Second {
		t.Fatalf("backoff = %v", h.sleeps)
	}
}

func TestExhaustedRetriesFailAndEscalate(t *testing.T) {
	h := setup(Autonomous, nil)
	h.run.errs = []error{errors.New("a"), errors.New("b"), errors.New("c")}
	out := handle(t, h, restart("nginx"))
	if out.Status != StatusFailed || len(h.run.calls) != 3 || !strings.Contains(h.audit.names(), "escalated") {
		t.Fatalf("out=%+v audit=%s", out, h.audit.names())
	}
}

func TestKillOfAlreadyGoneProcessIsIdempotentSuccess(t *testing.T) {
	h := setup(Autonomous, nil)
	h.run.errs = []error{errors.New("exit status 1")}
	h.run.output = "kill: (999): No such process"
	h.appr.approved = true
	if out := handle(t, h, kill("999")); out.Status != StatusSucceeded || len(h.run.calls) != 1 {
		t.Fatalf("%+v calls=%d", out, len(h.run.calls))
	}
}

func TestTimeoutIsAppliedPerAttempt(t *testing.T) {
	h := setup(Autonomous, func(c *Config) { c.Timeout = -time.Second; c.Retries = 1 }) // already expired
	if out := handle(t, h, restart("nginx")); out.Status != StatusFailed {
		t.Fatalf("expired context must fail the attempt: %+v", out)
	}
}

// ---- recovery verification ----

func verification(values ...float64) *Verification {
	i := 0
	return &Verification{
		Cond: Condition{Operator: ">", Threshold: 90},
		Check: func(context.Context) (float64, error) {
			v := values[min(i, len(values)-1)]
			i++
			return v, nil
		},
	}
}

func TestVerifiedWhenConditionClears(t *testing.T) {
	h := setup(Autonomous, nil)
	task := restart("nginx")
	task.Verify = verification(95, 40) // first check still bad, second clear
	out := handle(t, h, task)
	if out.Status != StatusVerified || h.sleeps[len(h.sleeps)-1] != 10*time.Second {
		t.Fatalf("out=%+v sleeps=%v", out, h.sleeps)
	}
}

func TestEscalatesWhenProblemPersists(t *testing.T) {
	h := setup(Autonomous, nil)
	task := restart("nginx")
	task.Verify = verification(97)
	if out := handle(t, h, task); out.Status != StatusEscalated || !strings.Contains(out.Detail, "persists") {
		t.Fatalf("%+v", out)
	}
}

func TestUnreadableMetricIsNeverTreatedAsRecovered(t *testing.T) {
	h := setup(Autonomous, nil)
	task := restart("nginx")
	task.Verify = &Verification{Cond: Condition{Operator: ">", Threshold: 90}, Check: func(context.Context) (float64, error) {
		return 0, errors.New("metrics api down")
	}}
	if out := handle(t, h, task); out.Status != StatusEscalated {
		t.Fatalf("%+v", out)
	}
}

func TestNoVerificationMeansSucceededNotVerified(t *testing.T) {
	h := setup(Autonomous, nil)
	if out := handle(t, h, restart("nginx")); out.Status != StatusSucceeded {
		t.Fatalf("%+v", out)
	}
}

// ---- audit trail ----

func TestAuditTrailTellsTheWholeStory(t *testing.T) {
	h := setup(Autonomous, nil)
	task := restart("nginx")
	task.Verify = verification(10)
	handle(t, h, task)
	if got := h.audit.names(); got != "proposed,running,succeeded,verified" {
		t.Fatalf("trail = %s", got)
	}
}

func TestFailsClosedWhenAuditLogIsUnwritable(t *testing.T) {
	h := setup(Autonomous, nil)
	h.audit.fail = true
	_, err := h.ex.Handle(context.Background(), restart("nginx"))
	if err == nil || !errors.Is(err, errAudit) || len(h.run.calls) != 0 {
		t.Fatalf("err=%v calls=%d: nothing may run without an audit record", err, len(h.run.calls))
	}
}
