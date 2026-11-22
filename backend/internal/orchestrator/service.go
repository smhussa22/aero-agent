package orchestrator

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/time/rate"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	aerov1 "github.com/smhussa22/aero-agent/backend/gen/aerov1"
	"github.com/smhussa22/aero-agent/backend/internal/ingest"
)

const (
	queueSize      = 32
	maxProposals   = 10
	maxParams      = 20
	maxFieldLen    = 2000
	messagesPerSec = 20
	messageBurst   = 50
)

// Service implements aerov1.RemediationServiceServer. Streams must pass ingest.AuthInterceptor
// first, which is what ties each connection to one host.
type Service struct {
	aerov1.UnimplementedRemediationServiceServer
	pool *pgxpool.Pool
	reg  *registry
}

func NewService(pool *pgxpool.Pool) *Service {
	return &Service{pool: pool, reg: newRegistry()}
}

// statusName maps the wire enum to the strings the remediation_actions CHECK constraint allows.
var statusName = map[aerov1.ActionStatus]string{
	aerov1.ActionStatus_ACTION_STATUS_PROPOSED:          "proposed",
	aerov1.ActionStatus_ACTION_STATUS_AWAITING_APPROVAL: "awaiting_approval",
	aerov1.ActionStatus_ACTION_STATUS_APPROVED:          "approved",
	aerov1.ActionStatus_ACTION_STATUS_REJECTED:          "rejected",
	aerov1.ActionStatus_ACTION_STATUS_RUNNING:           "running",
	aerov1.ActionStatus_ACTION_STATUS_SUCCEEDED:         "succeeded",
	aerov1.ActionStatus_ACTION_STATUS_FAILED:            "failed",
	aerov1.ActionStatus_ACTION_STATUS_VERIFIED:          "verified",
	aerov1.ActionStatus_ACTION_STATUS_ESCALATED:         "escalated",
}

func clip(s string) string {
	if len(s) > maxFieldLen {
		return strings.ToValidUTF8(s[:maxFieldLen], "")
	}
	return s
}

func parseModeName(m string) aerov1.PermissionMode {
	switch m {
	case "recommend":
		return aerov1.PermissionMode_PERMISSION_MODE_RECOMMEND
	case "autonomous":
		return aerov1.PermissionMode_PERMISSION_MODE_AUTONOMOUS
	}
	return aerov1.PermissionMode_PERMISSION_MODE_OBSERVE
}

func (s *Service) hostMode(ctx context.Context, host string) aerov1.PermissionMode {
	var mode string
	if err := s.pool.QueryRow(ctx, "SELECT mode FROM hosts WHERE id = $1", host).Scan(&mode); err != nil {
		return aerov1.PermissionMode_PERMISSION_MODE_OBSERVE // unknown host: least power
	}
	return parseModeName(mode)
}

// Connect serves one remediation agent for the life of its stream.
func (s *Service) Connect(stream aerov1.RemediationService_ConnectServer) error {
	ctx := stream.Context()
	host := ingest.HostFrom(ctx)
	if host == "" {
		return status.Error(codes.Unauthenticated, "no authenticated host")
	}

	// the first message must be Hello, and must name the host the token belongs to
	first, err := stream.Recv()
	if err != nil {
		return err
	}
	// who this is comes from the token; the host name inside Hello is only informational and never trusted
	if first.GetHello() == nil {
		return status.Error(codes.InvalidArgument, "first message must be hello")
	}

	out := make(chan *aerov1.OrchestratorMessage, queueSize)
	s.reg.register(host, out)
	defer s.reg.unregister(host, out)
	slog.Info("agent connected", "host", host, "reported_mode", first.GetHello().GetMode())

	// the database is the source of truth for the mode; the agent may still cap it locally
	out <- &aerov1.OrchestratorMessage{Body: &aerov1.OrchestratorMessage_ModeChange{
		ModeChange: &aerov1.ModeChange{Mode: s.hostMode(ctx, host)}}}

	// a single goroutine owns Send, because gRPC streams allow only one concurrent sender
	writeErr := make(chan error, 1)
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case m := <-out:
				if err := stream.Send(m); err != nil {
					writeErr <- err
					return
				}
			}
		}
	}()

	limiter := rate.NewLimiter(messagesPerSec, messageBurst)
	for {
		msg, err := stream.Recv()
		if err != nil {
			select {
			case werr := <-writeErr:
				return werr
			default:
			}
			if errors.Is(err, context.Canceled) {
				return nil
			}
			return err
		}
		if !limiter.Allow() {
			return status.Error(codes.ResourceExhausted, "too many messages")
		}
		if err := s.handle(ctx, host, msg); err != nil {
			slog.Error("agent message failed", "host", host, "err", err)
		}
	}
}

func (s *Service) handle(ctx context.Context, host string, msg *aerov1.AgentMessage) error {
	switch b := msg.Body.(type) {
	case *aerov1.AgentMessage_Report:
		return s.handleReport(ctx, host, b.Report)
	case *aerov1.AgentMessage_ActionUpdate:
		return s.handleUpdate(ctx, host, b.ActionUpdate)
	}
	return nil // Hello after the first one, or unknown: ignored
}

func (s *Service) inTx(ctx context.Context, fn func(pgx.Tx) error) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func auditTx(ctx context.Context, tx pgx.Tx, actor, event, incident string, details map[string]any) error {
	_, err := tx.Exec(ctx, `INSERT INTO audit_log (org_id, actor, event, incident_id, details)
		VALUES ((SELECT org_id FROM incidents WHERE id = $3::uuid), $1, $2, $3::uuid, $4)`,
		actor, event, incident, details)
	return err
}

// ownsIncident reports whether the incident belongs to host. Agents may only touch their own.
func ownsIncident(ctx context.Context, tx pgx.Tx, incident, host string) (bool, error) {
	var owner string
	err := tx.QueryRow(ctx, "SELECT host_id FROM incidents WHERE id = $1::uuid", incident).Scan(&owner)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	return owner == host, err
}

func validUUID(s string) bool {
	_, err := uuid.Parse(s)
	return err == nil
}

func (s *Service) handleReport(ctx context.Context, host string, r *aerov1.IncidentReport) error {
	if !validUUID(r.GetIncidentId()) {
		return errors.New("report: invalid incident id")
	}
	if len(r.GetProposals()) > maxProposals {
		return errors.New("report: too many proposals")
	}

	return s.inTx(ctx, func(tx pgx.Tx) error {
		if ok, err := ownsIncident(ctx, tx, r.GetIncidentId(), host); err != nil || !ok {
			return errors.New("report: incident does not belong to this host")
		}
		if _, err := tx.Exec(ctx, "UPDATE incidents SET summary = $1, status = 'investigating' WHERE id = $2::uuid AND status IN ('open', 'investigating')",
			clip(r.GetSummary()), r.GetIncidentId()); err != nil {
			return err
		}

		for _, p := range r.GetProposals() {
			if !validUUID(p.GetProposalId()) || p.GetActionId() == "" || len(p.GetActionId()) > 64 || len(p.GetParams()) > maxParams {
				return errors.New("report: malformed proposal")
			}
			params, _ := json.Marshal(p.GetParams())
			// DO NOTHING on conflict: an agent cannot overwrite a proposal id that already exists
			if _, err := tx.Exec(ctx, `INSERT INTO remediation_actions (id, incident_id, action_id, params, rationale, dangerous)
				VALUES ($1::uuid, $2::uuid, $3, $4, $5, $6) ON CONFLICT (id) DO NOTHING`,
				p.GetProposalId(), r.GetIncidentId(), p.GetActionId(), params, clip(p.GetRationale()), p.GetDangerous()); err != nil {
				return err
			}
		}
		return auditTx(ctx, tx, "agent:"+host, "incident_report", r.GetIncidentId(),
			map[string]any{"proposals": len(r.GetProposals())})
	})
}

func (s *Service) handleUpdate(ctx context.Context, host string, u *aerov1.ActionUpdate) error {
	name, ok := statusName[u.GetStatus()]
	if !ok || !validUUID(u.GetProposalId()) || !validUUID(u.GetIncidentId()) {
		return errors.New("update: malformed")
	}

	return s.inTx(ctx, func(tx pgx.Tx) error {
		if ok, err := ownsIncident(ctx, tx, u.GetIncidentId(), host); err != nil || !ok {
			return errors.New("update: incident does not belong to this host")
		}

		// the agent may report progress, but it can never approve for itself: an action that
		// is awaiting approval only leaves that state through a human decision in the API
		tag, err := tx.Exec(ctx, `UPDATE remediation_actions SET status = $1, dry_run = $2, updated_at = now()
			WHERE id = $3::uuid AND incident_id = $4::uuid
			AND NOT (status = 'awaiting_approval' AND $1 IN ('approved', 'rejected', 'running', 'succeeded', 'verified'))`,
			name, u.GetDryRun(), u.GetProposalId(), u.GetIncidentId())
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return errors.New("update: unknown proposal or illegal transition")
		}

		switch u.GetStatus() {
		case aerov1.ActionStatus_ACTION_STATUS_RUNNING:
			_, err = tx.Exec(ctx, "UPDATE incidents SET status = 'remediating' WHERE id = $1::uuid AND status <> 'resolved'", u.GetIncidentId())
		case aerov1.ActionStatus_ACTION_STATUS_VERIFIED:
			_, err = tx.Exec(ctx, "UPDATE incidents SET status = 'resolved', resolved_at = now() WHERE id = $1::uuid", u.GetIncidentId())
		case aerov1.ActionStatus_ACTION_STATUS_ESCALATED:
			_, err = tx.Exec(ctx, "UPDATE incidents SET status = 'escalated' WHERE id = $1::uuid AND status <> 'resolved'", u.GetIncidentId())
		}
		if err != nil {
			return err
		}
		return auditTx(ctx, tx, "agent:"+host, "action_"+name, u.GetIncidentId(),
			map[string]any{"proposal": u.GetProposalId(), "action": u.GetActionId(), "detail": clip(u.GetDetail()), "dry_run": u.GetDryRun()})
	})
}
