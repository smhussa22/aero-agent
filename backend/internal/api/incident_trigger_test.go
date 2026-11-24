package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/smhussa22/aero-agent/backend/internal/testdb"
)

// The incident page must say what fired: the rule, and the value against the threshold recorded when it opened.
func TestIncidentDetailShowsTrigger(t *testing.T) {
	pool := testdb.Open(t)
	a := testdb.Seed(t, pool, "trg")
	ctx := context.Background()
	if _, err := pool.Exec(ctx, "UPDATE incidents SET rule_id = $1::uuid WHERE id = $2::uuid", a.RuleID, a.Incident); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO audit_log (org_id, actor, event, incident_id, details) VALUES ($1::uuid, 'system', 'incident_opened', $2::uuid,
		'{"metric":"cpu.usage_percent","value":94.5,"threshold":90}')`, a.OrgID, a.Incident); err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(NewServer(pool).Routes())
	defer srv.Close()
	req, _ := http.NewRequest("GET", srv.URL+"/api/incidents/"+a.Incident, nil)
	req.Header.Set("Authorization", "Bearer "+a.Token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var got struct {
		Trigger trigger `json:"trigger"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	want := trigger{Rule: "rule " + a.Marker, Metric: "cpu.usage_percent", Operator: ">", Severity: "warning", Value: 94.5, Threshold: 90}
	if got.Trigger != want {
		t.Fatalf("trigger = %+v, want %+v", got.Trigger, want)
	}
}
