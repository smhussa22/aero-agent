package orchestrator

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"

	aerov1 "github.com/smhussa22/aero-agent/backend/gen/aerov1"
)

// pgxTx is the transaction type used by the helpers in this package.
type pgxTx = pgx.Tx

// Listen delivers human decisions and mode changes to connected agents. The API raises a
// Postgres NOTIFY inside the same transaction as the change, so a notification always
// means the change committed. It reconnects until ctx is cancelled.
func (s *Service) Listen(ctx context.Context) {
	for ctx.Err() == nil {
		if err := s.listenOnce(ctx); err != nil && ctx.Err() == nil {
			slog.Error("notification listener stopped, retrying", "err", err)
			select {
			case <-ctx.Done():
			case <-time.After(2 * time.Second):
			}
		}
	}
}

func (s *Service) listenOnce(ctx context.Context) error {
	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()

	for _, ch := range []string{"action_decision", "host_mode"} {
		if _, err := conn.Exec(ctx, "LISTEN "+ch); err != nil {
			return err
		}
	}
	for {
		n, err := conn.Conn().WaitForNotification(ctx)
		if err != nil {
			return err
		}
		switch n.Channel {
		case "action_decision":
			err = s.deliverDecision(ctx, n.Payload)
		case "host_mode":
			s.reg.send(n.Payload, &aerov1.OrchestratorMessage{Body: &aerov1.OrchestratorMessage_ModeChange{
				ModeChange: &aerov1.ModeChange{Mode: s.hostMode(ctx, n.Payload)}}})
		}
		if err != nil {
			slog.Error("notification handling failed", "channel", n.Channel, "err", err)
		}
	}
}

// deliverDecision looks up the latest human decision for an action and forwards it to the
// agent on the action's own host, and to no one else.
func (s *Service) deliverDecision(ctx context.Context, actionID string) error {
	if !validUUID(actionID) {
		return errors.New("invalid action id in notification")
	}
	var incident, host, by string
	var approved bool
	err := s.pool.QueryRow(ctx, `SELECT a.incident_id::text, i.host_id, ap.approved, ap.decided_by
		FROM remediation_actions a
		JOIN incidents i ON i.id = a.incident_id
		JOIN approvals ap ON ap.action_id = a.id
		WHERE a.id = $1::uuid ORDER BY ap.decided_at DESC LIMIT 1`, actionID).Scan(&incident, &host, &approved, &by)
	if err != nil {
		return err
	}

	sent := s.reg.send(host, &aerov1.OrchestratorMessage{Body: &aerov1.OrchestratorMessage_Approval{
		Approval: &aerov1.ApprovalDecision{IncidentId: incident, ProposalId: actionID, Approved: approved, DecidedBy: by}}})
	slog.Info("decision delivered", "action", actionID, "host", host, "approved", approved, "delivered", sent)
	return nil
}
