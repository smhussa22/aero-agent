package orchestrator

import (
	"context"
	"errors"
	"log/slog"

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
		_, err := s.pool.Exec(ctx, "UPDATE incidents SET status = 'resolved', resolved_at = now() WHERE alert_id = $1 AND status <> 'resolved'", ev.GetAlertId())
		return err
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
		// the host row normally exists already (it holds the token); this keeps the FK satisfied regardless
		if _, err := tx.Exec(ctx, "INSERT INTO hosts (id) VALUES ($1) ON CONFLICT DO NOTHING", ev.GetHostId()); err != nil {
			return err
		}
		// rule_id becomes NULL if the rule was deleted since the alert fired
		rows, err := tx.Query(ctx, `INSERT INTO incidents (host_id, rule_id, alert_id)
			VALUES ($1, (SELECT id FROM alert_rules WHERE id = NULLIF($2, '')::uuid), $3)
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
