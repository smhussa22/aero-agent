// Package api is the REST API the web app talks to.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"

	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/time/rate"

	"github.com/smhussa22/aero-agent/backend/internal/auth"
	"github.com/smhussa22/aero-agent/backend/internal/mail"
	"github.com/smhussa22/aero-agent/backend/internal/store"
)

const maxBodyBytes = 1 << 20

// Sessions resolves a bearer token to a user.
type Sessions interface {
	UserForSession(ctx context.Context, token string) (store.User, error)
}

type Server struct {
	pool     *pgxpool.Pool
	sessions Sessions
	signin   *limiters // sign-in starts and returns, per client IP
	perUser  *limiters // per authenticated user

	auth            *auth.Service // nil until WithAuth: sign-in is then unavailable
	publicURL       string        // where browsers reach this app
	publicOrigin    string        // scheme and host of publicURL: the only origin allowed to change things with a cookie
	mailer          mail.Sender   // nil: invitations are not emailed
	gateway         string        // host:port new servers stream metrics to; empty disables enrollment
	releasesDir     string        // agent binaries served at /releases/; empty: not served

	webDir       string   // built web app to serve, empty for API only
	connectExtra []string // extra origins the web app may call
}

func NewServer(pool *pgxpool.Pool) *Server {
	return &Server{
		pool:     pool,
		sessions: store.SessionStore{Pool: pool},
		signin:   newLimiters(rate.Limit(20.0/60), 20), // about 20 sign-in steps a minute per address
		perUser:  newLimiters(20, 40),
	}
}

// WithAuth turns on identity-provider sign-in. publicURL is how browsers reach this app.
func (s *Server) WithAuth(a *auth.Service, publicURL string) *Server {
	s.auth, s.publicURL = a, strings.TrimRight(publicURL, "/")
	if u, err := url.Parse(s.publicURL); err == nil && u.Scheme != "" && u.Host != "" {
		s.publicOrigin = u.Scheme + "://" + u.Host
	}
	return s
}

// WithMailer lets the server email invitations.
func (s *Server) WithMailer(m mail.Sender) *Server {
	s.mailer = m
	return s
}

// limiters hands out one rate limiter per key.
type limiters struct {
	mu    sync.Mutex
	m     map[string]*rate.Limiter
	limit rate.Limit
	burst int
}

func newLimiters(l rate.Limit, burst int) *limiters {
	return &limiters{m: map[string]*rate.Limiter{}, limit: l, burst: burst}
}

func (l *limiters) allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	// shortcut: wipe the table when it grows large instead of expiring entries, replace with a TTL cache
	if len(l.m) > 10_000 {
		l.m = map[string]*rate.Limiter{}
	}
	lim, ok := l.m[key]
	if !ok {
		lim = rate.NewLimiter(l.limit, l.burst)
		l.m[key] = lim
	}
	return lim.Allow()
}

type userKey struct{}

func userFrom(ctx context.Context) store.User {
	u, _ := ctx.Value(userKey{}).(store.User)
	return u
}

func bearer(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if !strings.HasPrefix(h, "Bearer ") {
		return ""
	}
	return strings.TrimSpace(h[len("Bearer "):])
}

// sessionToken finds the session: the browser's cookie, or a bearer token for non-browser clients.
func sessionToken(r *http.Request) (token string, fromCookie bool) {
	if c, err := r.Cookie(auth.SessionCookie); err == nil && c.Value != "" {
		return c.Value, true
	}
	return bearer(r), false
}

// authed requires a valid session; admin additionally requires the admin role.
func (s *Server) authed(admin bool, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token, fromCookie := sessionToken(r)
		if token == "" {
			writeErr(w, http.StatusUnauthorized, "not signed in")
			return
		}
		// A browser attaches its cookie to requests another site triggers. Anything that changes data must therefore
		// prove it came from this app's own pages, on top of the cookie's SameSite setting.
		if fromCookie && r.Method != http.MethodGet && r.Method != http.MethodHead && !s.sameOrigin(r) {
			writeErr(w, http.StatusForbidden, "cross-site request refused")
			return
		}
		u, err := s.sessions.UserForSession(r.Context(), token)
		if errors.Is(err, store.ErrNoSession) {
			writeErr(w, http.StatusUnauthorized, "invalid or expired session")
			return
		}
		if err != nil {
			slog.Error("session lookup failed", "err", err)
			writeErr(w, http.StatusServiceUnavailable, "try again")
			return
		}
		if admin && u.Role != "admin" {
			writeErr(w, http.StatusForbidden, "admin role required")
			return
		}
		if !s.perUser.allow(u.ID) {
			writeErr(w, http.StatusTooManyRequests, "rate limit exceeded")
			return
		}
		next(w, r.WithContext(context.WithValue(r.Context(), userKey{}, u)))
	}
}

// sameOrigin is true only when the request names this app's own origin. No Origin header is refused:
// every browser sends one on a cross-site request that changes data.
func (s *Server) sameOrigin(r *http.Request) bool {
	return s.publicOrigin != "" && r.Header.Get("Origin") == s.publicOrigin
}

// signinRoute rate-limits the sign-in steps per client address and reports a missing provider plainly.
func (s *Server) signinRoute(h func(*auth.Service, http.ResponseWriter, *http.Request)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.auth == nil {
			writeErr(w, http.StatusServiceUnavailable, "sign-in is not configured on this server")
			return
		}
		if !s.signin.allow(clientIP(r)) {
			writeErr(w, http.StatusTooManyRequests, "too many sign-in attempts, wait a minute")
			return
		}
		h(s.auth, w, r)
	}
}

// Routes returns the full handler. Every route except the sign-in steps requires a session.
func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /auth/login", s.signinRoute((*auth.Service).Login))
	mux.HandleFunc("GET /auth/callback", s.signinRoute((*auth.Service).Callback))
	mux.HandleFunc("POST /auth/logout", func(w http.ResponseWriter, r *http.Request) {
		if s.auth == nil {
			writeErr(w, http.StatusServiceUnavailable, "sign-in is not configured on this server")
			return
		}
		if !s.sameOrigin(r) {
			writeErr(w, http.StatusForbidden, "cross-site request refused")
			return
		}
		s.auth.Logout(w, r)
	})

	mux.HandleFunc("GET /install.sh", s.handleInstallScript)
	if s.releasesDir != "" {
		mux.HandleFunc("GET /releases/{file}", s.handleReleases)
	}
	mux.HandleFunc("POST /enroll", s.handleEnroll)
	mux.HandleFunc("POST /api/enrollments", s.authed(true, s.handleCreateEnrollment))
	mux.HandleFunc("GET /api/me", s.authed(false, s.handleMe))
	mux.HandleFunc("GET /api/team", s.authed(false, s.handleTeam))
	mux.HandleFunc("POST /api/invites", s.authed(true, s.handleInvite))
	mux.HandleFunc("DELETE /api/invites/{id}", s.authed(true, s.handleRevokeInvite))
	mux.HandleFunc("PATCH /api/members/{id}", s.authed(true, s.handleChangeMember))
	mux.HandleFunc("DELETE /api/members/{id}", s.authed(true, s.handleRemoveMember))
	mux.HandleFunc("POST /api/org/delete", s.authed(true, s.handleDeleteOrg))

	mux.HandleFunc("GET /api/hosts", s.authed(false, s.handleListHosts))
	mux.HandleFunc("PATCH /api/hosts/{id}", s.authed(true, s.handleSetHostMode))
	mux.HandleFunc("GET /api/hosts/{id}/metrics", s.authed(false, s.handleMetrics))
	mux.HandleFunc("GET /api/hosts/{id}/latest", s.authed(false, s.handleLatest))

	mux.HandleFunc("GET /api/rules", s.authed(false, s.handleListRules))
	mux.HandleFunc("POST /api/rules", s.authed(true, s.handleCreateRule))
	mux.HandleFunc("PUT /api/rules/{id}", s.authed(true, s.handleUpdateRule))
	mux.HandleFunc("DELETE /api/rules/{id}", s.authed(true, s.handleDeleteRule))

	mux.HandleFunc("GET /api/incidents", s.authed(false, s.handleListIncidents))
	mux.HandleFunc("GET /api/incidents/{id}", s.authed(false, s.handleGetIncident))
	mux.HandleFunc("POST /api/actions/{id}/decision", s.authed(true, s.handleDecision))
	mux.HandleFunc("GET /api/approvals/pending", s.authed(false, s.handlePending))
	mux.HandleFunc("GET /api/audit", s.authed(false, s.handleAudit))
	if s.webDir != "" {
		mux.Handle("GET /", s.webApp())
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
		mux.ServeHTTP(w, r)
	})
}

func clientIP(r *http.Request) string {
	// shortcut: ignores X-Forwarded-For so it cannot be spoofed; behind a reverse proxy every client shares its IP
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// decode reads a JSON body and rejects unknown fields.
func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON body")
		return false
	}
	return true
}
