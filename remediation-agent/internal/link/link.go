// Package link is the remediation agent's connection to the orchestrator. The agent dials out,
// so the machine needs no inbound port. It also implements the executor's Approver and an
// audit sink that mirrors events to the backend.
package link

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/types/known/timestamppb"

	aerov1 "github.com/smhussa22/aero-agent/backend/gen/aerov1"
	"github.com/smhussa22/aero-agent/remediation-agent/internal/actions"
	"github.com/smhussa22/aero-agent/remediation-agent/internal/audit"
	"github.com/smhussa22/aero-agent/remediation-agent/internal/executor"
	"github.com/smhussa22/aero-agent/remediation-agent/internal/investigate"
	"github.com/smhussa22/aero-agent/remediation-agent/internal/localmetrics"
)

type Config struct {
	Addr     string
	Token    string
	CACert   string // pem file; empty means system roots
	Insecure bool   // no TLS, local development only
	HostID   string
	MaxMode  executor.Mode // ceiling: the server can never grant more than this
}

type Link struct {
	cfg  Config
	inv  *investigate.Investigator
	ex   *executor.Executor
	jobs chan *aerov1.Investigate

	mu      sync.Mutex
	send    func(*aerov1.AgentMessage) bool // non-nil while connected
	pending map[string]chan *aerov1.ApprovalDecision
	audit   audit.Sink // local sink used for events that do not come from the executor
}

func New(cfg Config) *Link {
	return &Link{cfg: cfg, jobs: make(chan *aerov1.Investigate, 8), pending: map[string]chan *aerov1.ApprovalDecision{}}
}

// Bind supplies the pieces that need the link itself (the executor holds the link as approver and sink).
func (l *Link) Bind(inv *investigate.Investigator, ex *executor.Executor, local audit.Sink) {
	l.inv, l.ex, l.audit = inv, ex, local
}

var modeRank = map[executor.Mode]int{executor.Observe: 0, executor.Recommend: 1, executor.Autonomous: 2}

// EffectiveMode is the lower of what the server asks for and the local ceiling.
func EffectiveMode(server, ceiling executor.Mode) executor.Mode {
	if modeRank[server] < modeRank[ceiling] {
		return server
	}
	return ceiling
}

func fromProto(m aerov1.PermissionMode) executor.Mode {
	switch m {
	case aerov1.PermissionMode_PERMISSION_MODE_RECOMMEND:
		return executor.Recommend
	case aerov1.PermissionMode_PERMISSION_MODE_AUTONOMOUS:
		return executor.Autonomous
	}
	return executor.Observe
}

func toProto(m executor.Mode) aerov1.PermissionMode {
	switch m {
	case executor.Recommend:
		return aerov1.PermissionMode_PERMISSION_MODE_RECOMMEND
	case executor.Autonomous:
		return aerov1.PermissionMode_PERMISSION_MODE_AUTONOMOUS
	}
	return aerov1.PermissionMode_PERMISSION_MODE_OBSERVE
}

func (l *Link) dialOptions() ([]grpc.DialOption, error) {
	if l.cfg.Insecure {
		return []grpc.DialOption{grpc.WithTransportCredentials(insecure.NewCredentials())}, nil
	}
	tlsCfg := &tls.Config{MinVersion: tls.VersionTLS12}
	if l.cfg.CACert != "" {
		pem, err := os.ReadFile(l.cfg.CACert)
		if err != nil {
			return nil, err
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(pem) {
			return nil, errors.New("no certificates found in the CA file")
		}
		tlsCfg.RootCAs = pool
	}
	return []grpc.DialOption{grpc.WithTransportCredentials(credentials.NewTLS(tlsCfg))}, nil
}

// Run keeps a connection to the orchestrator until ctx ends, and works through investigations.
func (l *Link) Run(ctx context.Context) error {
	opts, err := l.dialOptions()
	if err != nil {
		return err
	}
	conn, err := grpc.NewClient(l.cfg.Addr, opts...)
	if err != nil {
		return err
	}
	defer conn.Close()

	go l.worker(ctx)

	backoff := time.Second
	for ctx.Err() == nil {
		started := time.Now()
		err := l.session(ctx, aerov1.NewRemediationServiceClient(conn))
		if ctx.Err() != nil {
			return nil
		}
		if time.Since(started) > time.Minute {
			backoff = time.Second // it was healthy for a while, so start the backoff over
		}
		slog.Warn("orchestrator connection ended, reconnecting", "err", err, "in", backoff)
		select {
		case <-ctx.Done():
		case <-time.After(backoff):
		}
		if backoff *= 2; backoff > 30*time.Second {
			backoff = 30 * time.Second
		}
	}
	return nil
}

func (l *Link) session(ctx context.Context, client aerov1.RemediationServiceClient) error {
	sctx, cancel := context.WithCancel(metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+l.cfg.Token))
	defer cancel()

	stream, err := client.Connect(sctx)
	if err != nil {
		return err
	}

	var sendMu sync.Mutex // gRPC allows one concurrent Send per stream
	send := func(m *aerov1.AgentMessage) bool {
		sendMu.Lock()
		defer sendMu.Unlock()
		return stream.Send(m) == nil
	}
	if !send(&aerov1.AgentMessage{Body: &aerov1.AgentMessage_Hello{Hello: &aerov1.Hello{HostId: l.cfg.HostID, Mode: toProto(l.ex.Mode())}}}) {
		return errors.New("could not send hello")
	}

	l.mu.Lock()
	l.send = send
	l.mu.Unlock()
	defer func() {
		l.mu.Lock()
		l.send = nil
		l.mu.Unlock()
	}()
	slog.Info("connected to orchestrator", "addr", l.cfg.Addr, "mode", l.ex.Mode())

	for {
		msg, err := stream.Recv()
		if err != nil {
			return err
		}
		switch b := msg.Body.(type) {
		case *aerov1.OrchestratorMessage_ModeChange:
			mode := EffectiveMode(fromProto(b.ModeChange.GetMode()), l.cfg.MaxMode)
			l.ex.SetMode(mode)
			slog.Info("mode set", "mode", mode, "server_requested", fromProto(b.ModeChange.GetMode()), "ceiling", l.cfg.MaxMode)
		case *aerov1.OrchestratorMessage_Investigate:
			select {
			case l.jobs <- b.Investigate:
			default:
				slog.Warn("investigation queue full, dropping", "incident", b.Investigate.GetIncidentId())
			}
		case *aerov1.OrchestratorMessage_Approval:
			l.deliver(b.Approval)
		}
	}
}

func (l *Link) deliver(d *aerov1.ApprovalDecision) {
	l.mu.Lock()
	ch, ok := l.pending[d.GetProposalId()]
	l.mu.Unlock()
	if !ok {
		slog.Warn("approval for a proposal this agent is not waiting on", "proposal", d.GetProposalId())
		return
	}
	select {
	case ch <- d:
	default:
	}
}

func (l *Link) sendMsg(m *aerov1.AgentMessage) bool {
	l.mu.Lock()
	send := l.send
	l.mu.Unlock()
	return send != nil && send(m)
}

// Request implements executor.Approver: it waits for a human decision relayed by the orchestrator.
func (l *Link) Request(ctx context.Context, incidentID, proposalID string, _ actions.Proposal) (bool, string, error) {
	ch := make(chan *aerov1.ApprovalDecision, 1)
	l.mu.Lock()
	l.pending[proposalID] = ch
	l.mu.Unlock()
	defer func() {
		l.mu.Lock()
		delete(l.pending, proposalID)
		l.mu.Unlock()
	}()

	select {
	case d := <-ch:
		if d.GetIncidentId() != incidentID {
			return false, "", errors.New("approval is for a different incident")
		}
		return d.GetApproved(), d.GetDecidedBy(), nil
	case <-ctx.Done():
		return false, "", ctx.Err()
	}
}

var statusFor = map[string]aerov1.ActionStatus{
	"proposed":          aerov1.ActionStatus_ACTION_STATUS_PROPOSED,
	"observed":          aerov1.ActionStatus_ACTION_STATUS_PROPOSED,
	"awaiting_approval": aerov1.ActionStatus_ACTION_STATUS_AWAITING_APPROVAL,
	"approved":          aerov1.ActionStatus_ACTION_STATUS_APPROVED,
	"rejected":          aerov1.ActionStatus_ACTION_STATUS_REJECTED,
	"expired":           aerov1.ActionStatus_ACTION_STATUS_REJECTED,
	"blocked":           aerov1.ActionStatus_ACTION_STATUS_FAILED,
	"dry_run":           aerov1.ActionStatus_ACTION_STATUS_SUCCEEDED,
	"running":           aerov1.ActionStatus_ACTION_STATUS_RUNNING,
	"succeeded":         aerov1.ActionStatus_ACTION_STATUS_SUCCEEDED,
	"verified":          aerov1.ActionStatus_ACTION_STATUS_VERIFIED,
	"escalated":         aerov1.ActionStatus_ACTION_STATUS_ESCALATED,
}

// Record implements audit.Sink by mirroring executor events to the backend (best effort).
func (l *Link) Record(_ context.Context, e audit.Event) error {
	st, ok := statusFor[e.Event]
	if !ok || e.ProposalID == "" {
		return nil
	}
	l.sendMsg(&aerov1.AgentMessage{Body: &aerov1.AgentMessage_ActionUpdate{ActionUpdate: &aerov1.ActionUpdate{
		IncidentId: e.IncidentID, ProposalId: e.ProposalID, ActionId: e.ActionID,
		Status: st, DryRun: e.DryRun, Detail: e.Detail, At: timestamppb.New(e.Time),
	}}})
	return nil
}

func newUUID() string {
	var b [16]byte
	rand.Read(b[:])
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}

// worker handles investigations one at a time; fixes must not race each other.
func (l *Link) worker(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case job := <-l.jobs:
			l.handle(ctx, job)
		}
	}
}

func (l *Link) note(ctx context.Context, incident, event, detail string) {
	if l.audit != nil {
		l.audit.Record(ctx, audit.Event{Time: time.Now().UTC(), IncidentID: incident, Event: event, Actor: "agent", Detail: detail})
	}
}

func (l *Link) handle(ctx context.Context, job *aerov1.Investigate) {
	ev := job.GetAlert()
	incident := job.GetIncidentId()

	rep, err := l.inv.Investigate(ctx, investigate.Alert{
		HostID: ev.GetHostId(), Metric: ev.GetMetric(), Value: ev.GetValue(),
		Threshold: ev.GetThreshold(), Severity: ev.GetSeverity().String(),
	})
	if err != nil {
		slog.Error("investigation failed", "incident", incident, "err", err)
		l.note(ctx, incident, "investigation_failed", err.Error())
		rep = investigate.Report{Summary: "The AI investigation failed: " + err.Error()}
	}

	// give each proposal an id, then tell the backend before acting so its rows exist for updates
	report := &aerov1.IncidentReport{IncidentId: incident, AlertId: ev.GetAlertId(), Summary: rep.Summary}
	ids := make([]string, len(rep.Proposals))
	for i, p := range rep.Proposals {
		ids[i] = newUUID()
		report.Proposals = append(report.Proposals, &aerov1.ProposedAction{
			ActionId: p.ActionID, Params: p.Params, Rationale: p.Rationale, Dangerous: p.Dangerous, ProposalId: ids[i]})
	}
	l.sendMsg(&aerov1.AgentMessage{Body: &aerov1.AgentMessage_Report{Report: report}})
	for _, r := range rep.Rejected {
		l.note(ctx, incident, "proposal_refused", r.ActionID+": "+r.Reason)
	}

	for i, p := range rep.Proposals {
		task := executor.Task{IncidentID: incident, ProposalID: ids[i], Proposal: p, Verify: verification(ev)}
		out, err := l.ex.Handle(ctx, task)
		if err != nil {
			slog.Error("executor stopped", "incident", incident, "err", err)
			return // the audit log is unwritable; do not continue with later proposals
		}
		slog.Info("action handled", "incident", incident, "action", p.ActionID, "status", out.Status, "detail", out.Detail)
	}
}

// verification re-reads the alert's metric locally, when it is one the agent can read.
func verification(ev *aerov1.AlertEvent) *executor.Verification {
	if ev.GetOperator() == "" || !localmetrics.Supported(ev.GetMetric()) {
		return nil
	}
	return &executor.Verification{
		Cond: executor.Condition{Operator: ev.GetOperator(), Threshold: ev.GetThreshold()},
		Check: func(ctx context.Context) (float64, error) {
			return localmetrics.Read(ctx, ev.GetMetric(), ev.GetLabel())
		},
	}
}
