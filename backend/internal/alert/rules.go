package alert

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	aerov1 "github.com/smhussa22/aero-agent/backend/gen/aerov1"
)

var severities = map[string]aerov1.Severity{
	"info":     aerov1.Severity_SEVERITY_INFO,
	"warning":  aerov1.Severity_SEVERITY_WARNING,
	"critical": aerov1.Severity_SEVERITY_CRITICAL,
}

// LoadRules returns every enabled alert rule.
func LoadRules(ctx context.Context, pool *pgxpool.Pool) ([]Rule, error) {
	rows, err := pool.Query(ctx, `SELECT id::text, COALESCE(host_id, ''), metric, label, operator,
		threshold, duration_seconds, severity FROM alert_rules WHERE enabled`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var rules []Rule
	for rows.Next() {
		var r Rule
		var secs int
		var sev string
		if err := rows.Scan(&r.ID, &r.HostID, &r.Metric, &r.Label, &r.Operator, &r.Threshold, &secs, &sev); err != nil {
			return nil, err
		}
		r.Duration = time.Duration(secs) * time.Second
		r.Severity = severities[sev]
		rules = append(rules, r)
	}
	return rules, rows.Err()
}
