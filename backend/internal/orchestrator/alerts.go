package orchestrator

import (
	"context"
	"errors"
	"log/slog"

	"github.com/jackc/pgx/v5"
	"google.golang.org/protobuf/proto"

	aerov1 "github.com/smhussa22/aero-agent/backend/gen/aerov1"
)

// HandleAlert is the callback for one message on the alerts topic. A returned error stops the
// consumer without committing, so the alert is redelivered; bad messages are dropped instead.
func (s *Service) HandleAlert(ctx context.Context, value []byte) error {
	var ev aerov1.AlertEvent
	if err := proto.Unmarshal(value, &ev); err != nil || ev.GetHostId() == "" || ev.GetAlertId() == "" {
		slog.Error("dropping malformed alert", "err", err)
		return nil
	}

	if ev.GetResolved() {
		return s.resolveIncident(ctx, ev.GetAlertId())
	}

	incident, created, err := s.openIncident(ctx, &ev)
	if err != nil {
		return err
	}
	if !created {
		return nil // redelivery of an alert we already handled
	}

	sent := s.reg.send(ev.GetHostId(), &aerov1.OrchestratorMessage{Body: &aerov1.OrchestratorMessage_Investigate{
		Investigate: &aerov1.Investigate{Alert: &ev, IncidentId: incident}}})
	slog.Info("incident opened", "incident", incident, "host", ev.GetHostId(), "metric", ev.GetMetric(), "dispatched", sent)
	return nil
}

// openIncident creates the incident for an alert once; created is false for a duplicate.
func (s *Service) openIncident(ctx context.Context, ev *aerov1.AlertEvent) (id string, created bool, err error) {
	rule := ev.GetRuleId()
	if rule != "" && !validUUID(rule) {
		return "", false, errors.New("alert has an invalid rule id")
	}

	err = s.inTx(ctx, func(tx pgxTx) error {
		// the organization comes from the host's own record. The rule is linked only if it belongs to that same
		// organization (and becomes NULL if it was deleted since the alert fired). A host that no longer exists
		// inserts nothing, so the alert is dropped.
		rows, err := tx.Query(ctx, `INSERT INTO incidents (host_id, org_id, rule_id, alert_id)
			SELECT h.id, h.org_id,
			       (SELECT r.id FROM alert_rules r WHERE r.id = NULLIF($2, '')::uuid AND r.org_id = h.org_id), $3
			FROM hosts h WHERE h.id = $1
			ON CONFLICT (alert_id) DO NOTHING RETURNING id::text`, ev.GetHostId(), rule, ev.GetAlertId())
		if err != nil {
			return err
		}
		if rows.Next() {
			created = true
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return err
			}
		}
		rows.Close()
		if err := rows.Err(); err != nil || !created {
			return err
		}
		return auditTx(ctx, tx, "system", "incident_opened", id,
			map[string]any{"alert": ev.GetAlertId(), "metric": ev.GetMetric(), "value": ev.GetValue(), "threshold": ev.GetThreshold()})
	})
	return id, created, err
}

// resolveIncident closes the incident when its alert clears on its own. Fixes still waiting for a
// human are cancelled, so nobody approves a dangerous action for a problem that no longer exists,
// and the agent waiting on them is told straight away.
func (s *Service) resolveIncident(ctx context.Context, alertID string) error {
	type cancelled struct{ id, host, incident string }
	var stale []cancelled

	err := s.inTx(ctx, func(tx pgxTx) error {
		var incident, host string
		err := tx.QueryRow(ctx, `UPDATE incidents SET status = 'resolved', resolved_at = now()
			WHERE alert_id = $1 AND status <> 'resolved' RETURNING id::text, host_id`, alertID).Scan(&incident, &host)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil // already resolved, or unknown
		}
		if err != nil {
			return err
		}

		rows, err := tx.Query(ctx, `UPDATE remediation_actions SET status = 'rejected', updated_at = now()
			WHERE incident_id = $1::uuid AND status = 'awaiting_approval' RETURNING id::text`, incident)
		if err != nil {
			return err
		}
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return err
			}
			stale = append(stale, cancelled{id, host, incident})
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		for _, c := range stale {
			if err := auditTx(ctx, tx, "system", "action_cancelled", incident,
				map[string]any{"action": c.id, "reason": "the alert resolved on its own before anyone approved it"}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return err
	}

	for _, c := range stale {
		s.reg.send(c.host, &aerov1.OrchestratorMessage{Body: &aerov1.OrchestratorMessage_Approval{
			Approval: &aerov1.ApprovalDecision{IncidentId: c.incident, ProposalId: c.id, Approved: false, DecidedBy: "system (alert resolved)"}}})
	}
	return nil
}
