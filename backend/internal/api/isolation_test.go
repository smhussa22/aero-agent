package api

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/smhussa22/aero-agent/backend/internal/testdb"
)

// Two organizations, each with a host, a rule, an incident, a pending approval and an audit entry. Every
// route is then used by organization B against organization A's resources. Each attempt must look like the
// resource does not exist, must change nothing, and no list may ever contain a trace of the other organization.
func TestTenantIsolation(t *testing.T) {
	pool := testdb.Open(t)
	a := testdb.Seed(t, pool, "a")
	b := testdb.Seed(t, pool, "b")

	srv := httptest.NewServer(NewServer(pool).Routes())
	defer srv.Close()

	call := func(token, method, path, body string) (int, string) {
		t.Helper()
		req, _ := http.NewRequest(method, srv.URL+path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		out, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(out)
	}
	scalar := func(q string, args ...any) string {
		t.Helper()
		var v string
		if err := pool.QueryRow(context.Background(), q, args...).Scan(&v); err != nil {
			t.Fatal(err)
		}
		return v
	}

	ruleBody := `{"name":"hijacked","metric":"cpu.usage_percent","operator":">","threshold":1,"duration_seconds":0,"severity":"info","enabled":true,"host_id":null}`

	// ---- B reaches for A's things by id ----
	attacks := []struct{ name, method, path, body string }{
		{"read A's metrics", "GET", "/api/hosts/" + a.HostID + "/metrics?metric=cpu.usage_percent", ""},
		{"read A's latest values", "GET", "/api/hosts/" + a.HostID + "/latest", ""},
		{"change A's host mode", "PATCH", "/api/hosts/" + a.HostID, `{"mode":"autonomous"}`},
		{"edit A's rule", "PUT", "/api/rules/" + a.RuleID, ruleBody},
		{"delete A's rule", "DELETE", "/api/rules/" + a.RuleID, ""},
		{"read A's incident", "GET", "/api/incidents/" + a.Incident, ""},
		{"approve A's action", "POST", "/api/actions/" + a.Action + "/decision", `{"approved":true}`},
		{"reject A's action", "POST", "/api/actions/" + a.Action + "/decision", `{"approved":false}`},
	}
	for _, c := range attacks {
		status, body := call(b.Token, c.method, c.path, c.body)
		if status != http.StatusNotFound {
			t.Errorf("%s: status %d, want 404 (body: %.120s)", c.name, status, body)
		}
		if strings.Contains(body, a.Marker) || strings.Contains(body, a.HostID) {
			t.Errorf("%s: response leaks organization A: %.200s", c.name, body)
		}
	}

	// B points a new rule at A's host
	if status, _ := call(b.Token, "POST", "/api/rules", strings.Replace(ruleBody, `"host_id":null`, `"host_id":"`+a.HostID+`"`, 1)); status != http.StatusBadRequest {
		t.Errorf("rule pointing at another organization's host: status %d, want 400", status)
	}

	// ---- nothing of A's changed ----
	if got := scalar("SELECT mode FROM hosts WHERE id = $1", a.HostID); got != "observe" {
		t.Errorf("A's host mode was changed to %q", got)
	}
	if got := scalar("SELECT name FROM alert_rules WHERE id = $1::uuid", a.RuleID); got != "rule "+a.Marker {
		t.Errorf("A's rule was edited or deleted: %q", got)
	}
	if got := scalar("SELECT status FROM remediation_actions WHERE id = $1::uuid", a.Action); got != "awaiting_approval" {
		t.Errorf("A's action was decided: %q", got)
	}
	if got := scalar("SELECT count(*)::text FROM approvals WHERE action_id = $1::uuid", a.Action); got != "0" {
		t.Errorf("an approval row was written for A's action: %s", got)
	}
	if got := scalar("SELECT count(*)::text FROM alert_rules WHERE org_id = $1::uuid AND host_id = $2", b.OrgID, a.HostID); got != "0" {
		t.Errorf("B created a rule on A's host")
	}

	// ---- lists show only the caller's own data, in both directions ----
	lists := []string{"/api/hosts", "/api/rules", "/api/incidents", "/api/approvals/pending", "/api/audit"}
	for _, path := range lists {
		_, bodyB := call(b.Token, "GET", path, "")
		for _, leak := range []string{a.Marker, a.HostID, a.RuleID, a.Incident, a.Action, a.OrgID} {
			if strings.Contains(bodyB, leak) {
				t.Errorf("GET %s as B contains A's %q", path, leak)
			}
		}
		_, bodyA := call(a.Token, "GET", path, "")
		for _, leak := range []string{b.Marker, b.HostID, b.RuleID, b.Incident, b.Action, b.OrgID} {
			if strings.Contains(bodyA, leak) {
				t.Errorf("GET %s as A contains B's %q", path, leak)
			}
		}
	}

	// ---- positive controls: the owner still gets everything (so the 404s above are not just a broken API) ----
	own := []struct{ name, path, want string }{
		{"hosts", "/api/hosts", a.HostID},
		{"rules", "/api/rules", a.RuleID},
		{"incidents", "/api/incidents", a.Incident},
		{"pending approvals", "/api/approvals/pending", a.Action},
		{"audit", "/api/audit", a.Marker},
		{"incident detail", "/api/incidents/" + a.Incident, a.Action},
		{"latest", "/api/hosts/" + a.HostID + "/latest", "cpu.usage_percent"},
		{"metrics", "/api/hosts/" + a.HostID + "/metrics?metric=cpu.usage_percent", "42"},
	}
	for _, c := range own {
		status, body := call(a.Token, "GET", c.path, "")
		if status != http.StatusOK || !strings.Contains(body, c.want) {
			t.Errorf("owner cannot read its own %s: status %d, want %q in %.160s", c.name, status, c.want, body)
		}
	}
	// and the owner can act on its own approval
	if status, body := call(a.Token, "POST", "/api/actions/"+a.Action+"/decision", `{"approved":false}`); status != http.StatusOK {
		t.Errorf("owner cannot decide its own action: %d %s", status, body)
	}
	if got := scalar("SELECT org_id::text FROM audit_log WHERE event = 'action_rejected' AND incident_id = $1::uuid", a.Incident); got != a.OrgID {
		t.Errorf("the decision was audited under organization %s, want %s", got, a.OrgID)
	}

	// garbage ids are rejected, never a server error
	for _, path := range []string{"/api/incidents/not-a-uuid", "/api/hosts/'; drop table hosts;--/latest"} {
		if status, _ := call(a.Token, "GET", path, ""); status >= 500 {
			t.Errorf("GET %s: server error %d", path, status)
		}
	}
}
