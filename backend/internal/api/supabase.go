package api

import (
	"net/http"
	"strings"
)

// WithSupabase tells the web app where to sign people up and in. The anon key is public by design (it only lets a
// browser call Supabase Auth, and row access is not used here).
func (s *Server) WithSupabase(url, anonKey string) *Server {
	s.supabaseURL, s.supabaseAnonKey = strings.TrimRight(url, "/"), anonKey
	return s
}

// handleConfig is public: the login page needs it before anyone is signed in.
func (s *Server) handleConfig(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"supabase_url": s.supabaseURL, "supabase_anon_key": s.supabaseAnonKey})
}

// sessionRoute exchanges a Supabase token for a session. Like sign-in it is rate limited per address, and it only
// accepts requests that come from this app's own pages.
func (s *Server) sessionRoute(w http.ResponseWriter, r *http.Request) {
	if s.auth == nil {
		writeErr(w, http.StatusServiceUnavailable, "sign-in is not configured on this server")
		return
	}
	if !s.sameOrigin(r) {
		writeErr(w, http.StatusForbidden, "cross-site request refused")
		return
	}
	if !s.signin.allow(clientIP(r)) {
		writeErr(w, http.StatusTooManyRequests, "too many sign-in attempts, wait a minute")
		return
	}
	s.auth.Session(w, r)
}
