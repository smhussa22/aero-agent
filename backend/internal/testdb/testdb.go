// Package testdb gives integration tests a real database and fully populated tenants to attack each other with.
// Set AERO_TEST_DATABASE_URL to a Postgres with TimescaleDB (CI starts one); without it tests skip.
package testdb

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/smhussa22/aero-agent/backend/internal/migrate"
	"github.com/smhussa22/aero-agent/backend/internal/store"
	"github.com/smhussa22/aero-agent/backend/migrations"
)

// Open connects, applies migrations (idempotent) and returns the pool, or skips the test.
func Open(t *testing.T) *pgxpool.Pool {
	t.Helper()
	url := os.Getenv("AERO_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("set AERO_TEST_DATABASE_URL to run database integration tests")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	if err := migrate.Run(ctx, pool, migrations.FS); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return pool
}

// Tenant is one organization with one of everything, each carrying a marker string so a leak is visible in output.
type Tenant struct {
	Name     string
	OrgID    string
	Email    string
	Token    string // dashboard session
	HostID   string
	HostName string
	RuleID   string
	Incident string
	Action   string
	Marker   string // appears in the incident summary, the action rationale and the audit event
}

// Seed creates a tenant and removes it when the test ends.
func Seed(t *testing.T, pool *pgxpool.Pool, name string) Tenant {
	t.Helper()
	ctx := context.Background()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}

	suffix := make([]byte, 4)
	rand.Read(suffix)
	name += "-" + hex.EncodeToString(suffix) // unique per run, so a crashed earlier run can never collide
	tn := Tenant{Name: name, Email: name + "@iso.example", HostName: "web-" + name, Marker: "MARKER-" + name}
	var err error
	tn.OrgID, err = store.CreateOrg(ctx, pool, "org "+name)
	must(err)
	var uid string
	must(pool.QueryRow(ctx, `INSERT INTO users (org_id, email, role, idp_issuer, idp_subject) VALUES ($1::uuid, $2, 'admin', 'https://idp.test', $3) RETURNING id::text`,
		tn.OrgID, tn.Email, "sub-"+name).Scan(&uid))
	tn.Token, _, err = store.NewSession(ctx, pool, uid)
	must(err)

	tn.HostID, _, err = store.IssueToken(ctx, pool, tn.OrgID, tn.HostName)
	must(err)
	_, err = pool.Exec(ctx, `INSERT INTO metrics (time, host_id, metric, label, value) VALUES (now(), $1, 'cpu.usage_percent', '', 42)`, tn.HostID)
	must(err)
	must(pool.QueryRow(ctx, `INSERT INTO alert_rules (org_id, host_id, name, metric, operator, threshold, severity)
		VALUES ($1::uuid, $2, $3, 'cpu.usage_percent', '>', 90, 'warning') RETURNING id::text`, tn.OrgID, tn.HostID, "rule "+tn.Marker).Scan(&tn.RuleID))
	must(pool.QueryRow(ctx, `INSERT INTO incidents (host_id, org_id, alert_id, summary) VALUES ($1, $2::uuid, $3, $4) RETURNING id::text`,
		tn.HostID, tn.OrgID, "alert-"+tn.Marker, "summary "+tn.Marker).Scan(&tn.Incident))
	must(pool.QueryRow(ctx, `INSERT INTO remediation_actions (incident_id, action_id, params, rationale, status, dangerous)
		VALUES ($1::uuid, 'kill_process', '{"pid":"123"}', $2, 'awaiting_approval', true) RETURNING id::text`, tn.Incident, "rationale "+tn.Marker).Scan(&tn.Action))
	_, err = pool.Exec(ctx, `INSERT INTO audit_log (org_id, actor, event, incident_id) VALUES ($1::uuid, 'tester', $2, $3::uuid)`,
		tn.OrgID, "event "+tn.Marker, tn.Incident)
	must(err)

	t.Cleanup(func() {
		// deleting the organization cascades to users, hosts, rules, incidents, actions and sessions;
		// metrics have no foreign key, and the append-only audit rows are left behind by design
		pool.Exec(context.Background(), "DELETE FROM metrics WHERE host_id = $1", tn.HostID)
		pool.Exec(context.Background(), "DELETE FROM orgs WHERE id = $1::uuid", tn.OrgID)
	})
	return tn
}
