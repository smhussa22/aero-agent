package api

import (
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// handleLatest returns the newest value of every metric a host reported recently. The web app
// uses it to offer only metrics that exist and to fill stat tiles without pulling history.
func (s *Server) handleLatest(w http.ResponseWriter, r *http.Request) {
	rows, err := s.pool.Query(r.Context(), `SELECT DISTINCT ON (metric, label) metric, label, value, time
		FROM metrics WHERE host_id = $1 AND time > now() - interval '15 minutes'
		ORDER BY metric, label, time DESC`, r.PathValue("id"))
	if err != nil {
		fail(w, err)
		return
	}
	defer rows.Close()

	type latest struct {
		Metric string    `json:"metric"`
		Label  string    `json:"label"`
		Value  float64   `json:"value"`
		Time   time.Time `json:"time"`
	}
	out := []latest{}
	for rows.Next() {
		var l latest
		if err := rows.Scan(&l.Metric, &l.Label, &l.Value, &l.Time); err != nil {
			fail(w, err)
			return
		}
		out = append(out, l)
	}
	writeJSON(w, http.StatusOK, out)
}

// handlePending lists actions waiting for a human, newest first, with enough context to decide.
func (s *Server) handlePending(w http.ResponseWriter, r *http.Request) {
	rows, err := s.pool.Query(r.Context(), `SELECT a.id::text, a.incident_id::text, i.host_id, a.action_id, a.params,
			COALESCE(a.rationale, ''), a.dangerous, COALESCE(i.summary, ''), a.created_at
		FROM remediation_actions a JOIN incidents i ON i.id = a.incident_id
		WHERE a.status = 'awaiting_approval' ORDER BY a.created_at DESC LIMIT 100`)
	if err != nil {
		fail(w, err)
		return
	}
	defer rows.Close()

	type pending struct {
		ID         string         `json:"id"`
		IncidentID string         `json:"incident_id"`
		HostID     string         `json:"host_id"`
		ActionID   string         `json:"action_id"`
		Params     map[string]any `json:"params"`
		Rationale  string         `json:"rationale"`
		Dangerous  bool           `json:"dangerous"`
		Summary    string         `json:"summary"`
		CreatedAt  time.Time      `json:"created_at"`
	}
	out := []pending{}
	for rows.Next() {
		var p pending
		if err := rows.Scan(&p.ID, &p.IncidentID, &p.HostID, &p.ActionID, &p.Params, &p.Rationale, &p.Dangerous, &p.Summary, &p.CreatedAt); err != nil {
			fail(w, err)
			return
		}
		out = append(out, p)
	}
	writeJSON(w, http.StatusOK, out)
}

// handleAudit returns the newest audit entries. The log is append-only, so this is read-only by construction.
func (s *Server) handleAudit(w http.ResponseWriter, r *http.Request) {
	limit := 50
	if v := r.URL.Query().Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 500 {
			writeErr(w, http.StatusBadRequest, "limit must be 1 to 500")
			return
		}
		limit = n
	}
	rows, err := s.pool.Query(r.Context(), `SELECT id, at, actor, event, COALESCE(incident_id::text, ''), details
		FROM audit_log ORDER BY id DESC LIMIT $1`, limit)
	if err != nil {
		fail(w, err)
		return
	}
	defer rows.Close()

	type entry struct {
		ID         int64          `json:"id"`
		At         time.Time      `json:"at"`
		Actor      string         `json:"actor"`
		Event      string         `json:"event"`
		IncidentID string         `json:"incident_id"`
		Details    map[string]any `json:"details"`
	}
	out := []entry{}
	for rows.Next() {
		var e entry
		if err := rows.Scan(&e.ID, &e.At, &e.Actor, &e.Event, &e.IncidentID, &e.Details); err != nil {
			fail(w, err)
			return
		}
		out = append(out, e)
	}
	writeJSON(w, http.StatusOK, out)
}

// WithWebApp makes the server also serve the built web app from dir. connectExtra lists extra
// origins the page may call (a local model, for example); the AI providers are always allowed.
func (s *Server) WithWebApp(dir string, connectExtra []string) *Server {
	s.webDir, s.connectExtra = dir, connectExtra
	return s
}

// csp locks the app down so injected script could not send a pasted AI key to a stranger:
// scripts only from this origin, and fetches only to this origin and the AI providers.
func (s *Server) csp() string {
	connect := append([]string{"'self'", "https://api.anthropic.com", "https://api.openai.com"}, s.connectExtra...)
	return "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; " +
		"connect-src " + strings.Join(connect, " ") + "; base-uri 'none'; form-action 'self'; frame-ancestors 'none'; object-src 'none'"
}

// webApp serves files from webDir and falls back to index.html so client-side routes survive a reload.
func (s *Server) webApp() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", s.csp())
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Frame-Options", "DENY")

		// Clean on a rooted path removes any ".." so a request can never leave webDir
		file := filepath.Join(s.webDir, filepath.FromSlash(filepath.Clean("/"+r.URL.Path)))
		if info, err := os.Stat(file); err == nil && !info.IsDir() {
			if strings.HasPrefix(r.URL.Path, "/assets/") {
				w.Header().Set("Cache-Control", "public, max-age=31536000, immutable") // hashed filenames
			} else {
				w.Header().Set("Cache-Control", "no-cache")
			}
			http.ServeFile(w, r, file)
			return
		}
		w.Header().Set("Cache-Control", "no-cache")
		http.ServeFile(w, r, filepath.Join(s.webDir, "index.html"))
	})
}
