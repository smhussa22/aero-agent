package api

import (
	"context"
	"errors"
	"log/slog"
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/smhussa22/aero-agent/backend/internal/store"
)

func fail(w http.ResponseWriter, err error) {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && (pgErr.Code == "23503" || pgErr.Code == "22P02") {
		writeErr(w, http.StatusBadRequest, "references something that does not exist, or a malformed id")
		return
	}
	slog.Error("request failed", "err", err)
	writeErr(w, http.StatusInternalServerError, "internal error")
}

// audit records who did what; call it inside the same transaction as the change.
func audit(ctx context.Context, tx pgx.Tx, orgID, actor, event string, incidentID *string, details map[string]any) error {
	_, err := tx.Exec(ctx, "INSERT INTO audit_log (org_id, actor, event, incident_id, details) VALUES ($1::uuid, $2, $3, $4::uuid, $5)",
		orgID, actor, event, incidentID, details)
	return err
}

// hostInOrg is the gate for every route that names a host: a host in another organization looks exactly
// like a host that does not exist.
func (s *Server) hostInOrg(ctx context.Context, hostID, orgID string) (bool, error) {
	var ok bool
	err := s.pool.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM hosts WHERE id = $1 AND org_id = $2::uuid)", hostID, orgID).Scan(&ok)
	return ok, err
}

// requireHost writes the 404 and returns false when the host is not in the caller's organization.
func (s *Server) requireHost(w http.ResponseWriter, r *http.Request, hostID string) bool {
	ok, err := s.hostInOrg(r.Context(), hostID, userFrom(r.Context()).OrgID)
	if err != nil {
		fail(w, err)
		return false
	}
	if !ok {
		writeErr(w, http.StatusNotFound, "no such host")
	}
	return ok
}

// ---- auth ----

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if !s.login.allow(clientIP(r)) {
		writeErr(w, http.StatusTooManyRequests, "too many login attempts")
		return
	}
	var in struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if !decode(w, r, &in) {
		return
	}

	u, err := store.Authenticate(r.Context(), s.pool, in.Email, in.Password)
	if errors.Is(err, store.ErrInvalidCredentials) {
		writeErr(w, http.StatusUnauthorized, "invalid email or password")
		return
	}
	if err != nil {
		fail(w, err)
		return
	}
	token, expires, err := store.NewSession(r.Context(), s.pool, u.ID)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"token": token, "expires_at": expires, "user": u})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if err := store.DeleteSession(r.Context(), s.pool, bearer(r)); err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---- hosts and metrics ----

func (s *Server) handleListHosts(w http.ResponseWriter, r *http.Request) {
	rows, err := s.pool.Query(r.Context(), "SELECT id, name, mode, last_seen FROM hosts WHERE org_id = $1::uuid ORDER BY name", userFrom(r.Context()).OrgID)
	if err != nil {
		fail(w, err)
		return
	}
	defer rows.Close()

	type host struct {
		ID       string     `json:"id"`
		Name     string     `json:"name"`
		Mode     string     `json:"mode"`
		LastSeen *time.Time `json:"last_seen"`
	}
	out := []host{}
	for rows.Next() {
		var h host
		if err := rows.Scan(&h.ID, &h.Name, &h.Mode, &h.LastSeen); err != nil {
			fail(w, err)
			return
		}
		out = append(out, h)
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleSetHostMode(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Mode string `json:"mode"`
	}
	if !decode(w, r, &in) {
		return
	}
	if !oneOf(in.Mode, "observe", "recommend", "autonomous") {
		writeErr(w, http.StatusBadRequest, "mode must be observe, recommend or autonomous")
		return
	}

	id := r.PathValue("id")
	err := s.inTx(r.Context(), func(tx pgx.Tx) error {
		tag, err := tx.Exec(r.Context(), "UPDATE hosts SET mode = $1 WHERE id = $2 AND org_id = $3::uuid", in.Mode, id, userFrom(r.Context()).OrgID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return pgx.ErrNoRows
		}
		if err := audit(r.Context(), tx, userFrom(r.Context()).OrgID, userFrom(r.Context()).Email, "host_mode_changed", nil,
			map[string]any{"host": id, "mode": in.Mode}); err != nil {
			return err
		}
		// delivered to the orchestrator only if the transaction commits
		_, err = tx.Exec(r.Context(), "SELECT pg_notify('host_mode', $1)", id)
		return err
	})
	if errors.Is(err, pgx.ErrNoRows) {
		writeErr(w, http.StatusNotFound, "no such host")
		return
	}
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"id": id, "mode": in.Mode})
}

const maxMetricPoints = 5000

func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	if !s.requireHost(w, r, r.PathValue("id")) {
		return
	}
	q := r.URL.Query()
	metric := q.Get("metric")
	if metric == "" || len(metric) > 100 {
		writeErr(w, http.StatusBadRequest, "metric is required")
		return
	}
	to, from := time.Now(), time.Now().Add(-time.Hour)
	var err error
	if v := q.Get("to"); v != "" {
		if to, err = time.Parse(time.RFC3339, v); err != nil {
			writeErr(w, http.StatusBadRequest, "to must be RFC3339")
			return
		}
	}
	if v := q.Get("from"); v != "" {
		if from, err = time.Parse(time.RFC3339, v); err != nil {
			writeErr(w, http.StatusBadRequest, "from must be RFC3339")
			return
		}
	}

	// bucket (seconds) averages points into time_bucket slots so a long range stays under the point cap;
	// 0 returns raw points
	bucket := 0
	if v := q.Get("bucket"); v != "" {
		if bucket, err = strconv.Atoi(v); err != nil || bucket < 0 || bucket > 86400 {
			writeErr(w, http.StatusBadRequest, "bucket must be 0 to 86400 seconds")
			return
		}
	}
	rows, err := s.pool.Query(r.Context(), `SELECT t, v FROM (
			SELECT CASE WHEN $6 > 0 THEN time_bucket($6 * interval '1 second', time) ELSE time END AS t,
			       CASE WHEN $6 > 0 THEN avg(value) ELSE max(value) END AS v
			FROM metrics WHERE host_id = $1 AND metric = $2 AND ($3 = '' OR label = $3) AND time >= $4 AND time <= $5
			GROUP BY t ORDER BY t LIMIT $7) x ORDER BY t`,
		r.PathValue("id"), metric, q.Get("label"), from, to, bucket, maxMetricPoints)
	if err != nil {
		fail(w, err)
		return
	}
	defer rows.Close()

	type point struct {
		Time  time.Time `json:"time"`
		Value float64   `json:"value"`
	}
	out := []point{}
	for rows.Next() {
		var p point
		if err := rows.Scan(&p.Time, &p.Value); err != nil {
			fail(w, err)
			return
		}
		out = append(out, p)
	}
	writeJSON(w, http.StatusOK, out)
}

// ---- alert rules ----

type rule struct {
	ID              string  `json:"id"`
	HostID          *string `json:"host_id"`
	Name            string  `json:"name"`
	Metric          string  `json:"metric"`
	Label           string  `json:"label"`
	Operator        string  `json:"operator"`
	Threshold       float64 `json:"threshold"`
	DurationSeconds int     `json:"duration_seconds"`
	Severity        string  `json:"severity"`
	Enabled         bool    `json:"enabled"`
}

func oneOf(v string, options ...string) bool {
	for _, o := range options {
		if v == o {
			return true
		}
	}
	return false
}

func (r rule) validate() string {
	switch {
	case r.Name == "" || len(r.Name) > 200:
		return "name is required (max 200 chars)"
	case r.Metric == "" || len(r.Metric) > 100 || len(r.Label) > 200:
		return "metric is required (max 100 chars), label max 200"
	case !oneOf(r.Operator, ">", ">=", "<", "<="):
		return "operator must be >, >=, < or <="
	case math.IsNaN(r.Threshold) || math.IsInf(r.Threshold, 0):
		return "threshold must be a finite number"
	case r.DurationSeconds < 0 || r.DurationSeconds > 86400:
		return "duration_seconds must be between 0 and 86400"
	case !oneOf(r.Severity, "info", "warning", "critical"):
		return "severity must be info, warning or critical"
	}
	return ""
}

const ruleCols = "id::text, host_id, name, metric, label, operator, threshold, duration_seconds, severity, enabled"

func scanRule(row pgx.Row) (rule, error) {
	var r rule
	err := row.Scan(&r.ID, &r.HostID, &r.Name, &r.Metric, &r.Label, &r.Operator, &r.Threshold, &r.DurationSeconds, &r.Severity, &r.Enabled)
	return r, err
}

func (s *Server) handleListRules(w http.ResponseWriter, r *http.Request) {
	rows, err := s.pool.Query(r.Context(), "SELECT "+ruleCols+" FROM alert_rules WHERE org_id = $1::uuid ORDER BY created_at", userFrom(r.Context()).OrgID)
	if err != nil {
		fail(w, err)
		return
	}
	defer rows.Close()
	out := []rule{}
	for rows.Next() {
		ru, err := scanRule(rows)
		if err != nil {
			fail(w, err)
			return
		}
		out = append(out, ru)
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) saveRule(w http.ResponseWriter, r *http.Request, id string) {
	var in rule
	if !decode(w, r, &in) {
		return
	}
	if msg := in.validate(); msg != "" {
		writeErr(w, http.StatusBadRequest, msg)
		return
	}

	org := userFrom(r.Context()).OrgID
	// a rule may only point at one of this organization's own hosts
	if in.HostID != nil {
		if ok, err := s.hostInOrg(r.Context(), *in.HostID, org); err != nil {
			fail(w, err)
			return
		} else if !ok {
			writeErr(w, http.StatusBadRequest, "unknown host")
			return
		}
	}

	var row pgx.Row
	if id == "" {
		row = s.pool.QueryRow(r.Context(), `INSERT INTO alert_rules (org_id, host_id, name, metric, label, operator, threshold, duration_seconds, severity, enabled)
			VALUES ($1::uuid,$2,$3,$4,$5,$6,$7,$8,$9,$10) RETURNING `+ruleCols,
			org, in.HostID, in.Name, in.Metric, in.Label, in.Operator, in.Threshold, in.DurationSeconds, in.Severity, in.Enabled)
	} else {
		row = s.pool.QueryRow(r.Context(), `UPDATE alert_rules SET host_id=$1, name=$2, metric=$3, label=$4, operator=$5,
			threshold=$6, duration_seconds=$7, severity=$8, enabled=$9 WHERE id=$10 AND org_id=$11::uuid RETURNING `+ruleCols,
			in.HostID, in.Name, in.Metric, in.Label, in.Operator, in.Threshold, in.DurationSeconds, in.Severity, in.Enabled, id, org)
	}
	saved, err := scanRule(row)
	if errors.Is(err, pgx.ErrNoRows) {
		writeErr(w, http.StatusNotFound, "no such rule")
		return
	}
	if err != nil {
		fail(w, err)
		return
	}
	status := http.StatusOK
	if id == "" {
		status = http.StatusCreated
	}
	writeJSON(w, status, saved)
}

func (s *Server) handleCreateRule(w http.ResponseWriter, r *http.Request) { s.saveRule(w, r, "") }
func (s *Server) handleUpdateRule(w http.ResponseWriter, r *http.Request) {
	s.saveRule(w, r, r.PathValue("id"))
}

func (s *Server) handleDeleteRule(w http.ResponseWriter, r *http.Request) {
	tag, err := s.pool.Exec(r.Context(), "DELETE FROM alert_rules WHERE id = $1 AND org_id = $2::uuid", r.PathValue("id"), userFrom(r.Context()).OrgID)
	if err != nil {
		fail(w, err)
		return
	}
	if tag.RowsAffected() == 0 {
		writeErr(w, http.StatusNotFound, "no such rule")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---- incidents and approvals ----

func (s *Server) handleListIncidents(w http.ResponseWriter, r *http.Request) {
	status := r.URL.Query().Get("status")
	if status != "" && !oneOf(status, "open", "investigating", "remediating", "resolved", "escalated") {
		writeErr(w, http.StatusBadRequest, "unknown status")
		return
	}
	limit := 100
	if v := r.URL.Query().Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 500 {
			writeErr(w, http.StatusBadRequest, "limit must be 1 to 500")
			return
		}
		limit = n
	}

	rows, err := s.pool.Query(r.Context(), `SELECT i.id::text, i.host_id, h.name, i.alert_id, i.status, COALESCE(i.summary, ''), i.opened_at, i.resolved_at
		FROM incidents i JOIN hosts h ON h.id = i.host_id
		WHERE i.org_id = $1::uuid AND ($2 = '' OR i.status = $2) ORDER BY i.opened_at DESC LIMIT $3`, userFrom(r.Context()).OrgID, status, limit)
	if err != nil {
		fail(w, err)
		return
	}
	defer rows.Close()

	type incident struct {
		ID         string     `json:"id"`
		HostID     string     `json:"host_id"`
		HostName   string     `json:"host_name"`
		AlertID    string     `json:"alert_id"`
		Status     string     `json:"status"`
		Summary    string     `json:"summary"`
		OpenedAt   time.Time  `json:"opened_at"`
		ResolvedAt *time.Time `json:"resolved_at"`
	}
	out := []incident{}
	for rows.Next() {
		var i incident
		if err := rows.Scan(&i.ID, &i.HostID, &i.HostName, &i.AlertID, &i.Status, &i.Summary, &i.OpenedAt, &i.ResolvedAt); err != nil {
			fail(w, err)
			return
		}
		out = append(out, i)
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleGetIncident(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var inc struct {
		ID       string    `json:"id"`
		HostID   string    `json:"host_id"`
		HostName string    `json:"host_name"`
		AlertID  string    `json:"alert_id"`
		Status   string    `json:"status"`
		Summary  string    `json:"summary"`
		OpenedAt time.Time `json:"opened_at"`
		Actions  []action  `json:"actions"`
	}
	err := s.pool.QueryRow(r.Context(), `SELECT i.id::text, i.host_id, h.name, i.alert_id, i.status, COALESCE(i.summary, ''), i.opened_at
		FROM incidents i JOIN hosts h ON h.id = i.host_id WHERE i.id = $1 AND i.org_id = $2::uuid`, id, userFrom(r.Context()).OrgID).
		Scan(&inc.ID, &inc.HostID, &inc.HostName, &inc.AlertID, &inc.Status, &inc.Summary, &inc.OpenedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		writeErr(w, http.StatusNotFound, "no such incident")
		return
	}
	if err != nil {
		fail(w, err)
		return
	}

	rows, err := s.pool.Query(r.Context(), `SELECT id::text, action_id, params, COALESCE(rationale, ''), dangerous, dry_run, status, created_at
		FROM remediation_actions WHERE incident_id = $1 ORDER BY created_at`, id)
	if err != nil {
		fail(w, err)
		return
	}
	defer rows.Close()
	inc.Actions = []action{}
	for rows.Next() {
		var a action
		if err := rows.Scan(&a.ID, &a.ActionID, &a.Params, &a.Rationale, &a.Dangerous, &a.DryRun, &a.Status, &a.CreatedAt); err != nil {
			fail(w, err)
			return
		}
		inc.Actions = append(inc.Actions, a)
	}
	writeJSON(w, http.StatusOK, inc)
}

type action struct {
	ID        string         `json:"id"`
	ActionID  string         `json:"action_id"`
	Params    map[string]any `json:"params"`
	Rationale string         `json:"rationale"`
	Dangerous bool           `json:"dangerous"`
	DryRun    bool           `json:"dry_run"`
	Status    string         `json:"status"`
	CreatedAt time.Time      `json:"created_at"`
}

// handleDecision approves or rejects a proposed action. Only an action that is awaiting
// approval can be decided, and the decision, status change and audit entry commit together.
func (s *Server) handleDecision(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Approved bool `json:"approved"`
	}
	if !decode(w, r, &in) {
		return
	}
	id := r.PathValue("id")
	actor := userFrom(r.Context()).Email
	org := userFrom(r.Context()).OrgID

	errNotPending := errors.New("not awaiting approval")
	err := s.inTx(r.Context(), func(tx pgx.Tx) error {
		var status string
		var incident string
		// the join to the organization's incident is what keeps another organization's action unreachable
		err := tx.QueryRow(r.Context(), `SELECT a.status, a.incident_id::text FROM remediation_actions a
			JOIN incidents i ON i.id = a.incident_id WHERE a.id = $1 AND i.org_id = $2::uuid FOR UPDATE OF a`, id, org).Scan(&status, &incident)
		if err != nil {
			return err
		}
		if status != "awaiting_approval" {
			return errNotPending
		}

		next := "rejected"
		if in.Approved {
			next = "approved"
		}
		if _, err := tx.Exec(r.Context(), "INSERT INTO approvals (action_id, approved, decided_by) VALUES ($1, $2, $3)", id, in.Approved, actor); err != nil {
			return err
		}
		if _, err := tx.Exec(r.Context(), "UPDATE remediation_actions SET status = $1, updated_at = now() WHERE id = $2", next, id); err != nil {
			return err
		}
		if err := audit(r.Context(), tx, org, actor, "action_"+next, &incident, map[string]any{"action": id}); err != nil {
			return err
		}
		_, err = tx.Exec(r.Context(), "SELECT pg_notify('action_decision', $1)", id)
		return err
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		writeErr(w, http.StatusNotFound, "no such action")
	case errors.Is(err, errNotPending):
		writeErr(w, http.StatusConflict, "action is not awaiting approval")
	case err != nil:
		fail(w, err)
	default:
		writeJSON(w, http.StatusOK, map[string]any{"id": id, "approved": in.Approved})
	}
}

func (s *Server) inTx(ctx context.Context, fn func(tx pgx.Tx) error) error {
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
