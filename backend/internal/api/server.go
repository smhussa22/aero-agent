// Package api is the REST API the web app talks to.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"

	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/time/rate"

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
	login    *limiters // per client IP, strict
	perUser  *limiters // per authenticated user

	webDir       string   // built web app to serve, empty for API only
	connectExtra []string // extra origins the web app may call
}

func NewServer(pool *pgxpool.Pool) *Server {
	return &Server{
		pool:     pool,
		sessions: store.SessionStore{Pool: pool},
		login:    newLimiters(rate.Limit(5.0/60), 5), // about 5 attempts a minute
		perUser:  newLimiters(20, 40),
	}
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

// authed requires a valid session; admin additionally requires the admin role.
func (s *Server) authed(admin bool, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := bearer(r)
		if token == "" {
			writeErr(w, http.StatusUnauthorized, "missing token")
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

// Routes returns the full handler. Every route except login requires a session.
func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/login", s.handleLogin)
	mux.HandleFunc("POST /api/logout", s.authed(false, s.handleLogout))

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
