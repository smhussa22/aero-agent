package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/smhussa22/aero-agent/backend/internal/store"
)

type fakeSessions map[string]store.User

func (f fakeSessions) UserForSession(_ context.Context, token string) (store.User, error) {
	if u, ok := f[token]; ok {
		return u, nil
	}
	return store.User{}, store.ErrNoSession
}

func testServer() *Server {
	return &Server{
		sessions: fakeSessions{
			"admin-tok":  {ID: "1", Email: "a@x.io", Role: "admin"},
			"viewer-tok": {ID: "2", Email: "v@x.io", Role: "viewer"},
		},
		signin:       newLimiters(0, 2),
		perUser:      newLimiters(1000, 1000),
		publicOrigin: "https://app.example.com",
	}
}

func do(h http.Handler, method, path, token string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader("{}"))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestAuthenticationAndRoles(t *testing.T) {
	s := testServer()
	reached := false
	read := s.authed(false, func(w http.ResponseWriter, r *http.Request) { reached = true })
	write := s.authed(true, func(w http.ResponseWriter, r *http.Request) { reached = true })

	for _, c := range []struct {
		name    string
		h       http.HandlerFunc
		token   string
		want    int
		reached bool
	}{
		{"no token", read, "", 401, false},
		{"bad token", read, "nope", 401, false},
		{"viewer can read", read, "viewer-tok", 200, true},
		{"viewer cannot write", write, "viewer-tok", 403, false},
		{"admin can write", write, "admin-tok", 200, true},
	} {
		reached = false
		rec := do(c.h, "GET", "/x", c.token)
		if rec.Code != c.want || reached != c.reached {
			t.Errorf("%s: status=%d reached=%v", c.name, rec.Code, reached)
		}
	}
}

func TestUserRateLimit(t *testing.T) {
	s := testServer()
	s.perUser = newLimiters(0, 2)
	h := s.authed(false, func(w http.ResponseWriter, r *http.Request) {})
	codes := []int{do(h, "GET", "/x", "admin-tok").Code, do(h, "GET", "/x", "admin-tok").Code, do(h, "GET", "/x", "admin-tok").Code}
	if codes[0] != 200 || codes[1] != 200 || codes[2] != 429 {
		t.Fatalf("codes=%v", codes)
	}
}

func TestSignInStepsAreRateLimitedAndNeedAConfiguredProvider(t *testing.T) {
	s := testServer()
	h := s.Routes()
	// no provider configured: a plain answer, not a crash
	if rec := do(h, "GET", "/auth/login", ""); rec.Code != 503 {
		t.Fatalf("unconfigured sign-in = %d, want 503", rec.Code)
	}
	if rec := do(h, "GET", "/auth/callback?code=x&state=y", ""); rec.Code != 503 {
		t.Fatalf("unconfigured callback = %d, want 503", rec.Code)
	}
	// the limiter answers before any provider or database is touched
	s.signin = newLimiters(0, 0)
	s.auth = nil
	if rec := do(s.Routes(), "GET", "/auth/login", ""); rec.Code != 503 {
		t.Fatalf("status=%d", rec.Code)
	}
}

// A browser attaches its cookie to requests another site triggers, so anything that changes data with a cookie
// must name this app's own origin. Bearer clients (not browsers) are not subject to this.
func TestCookieSessionsCannotBeUsedAcrossSites(t *testing.T) {
	s := testServer()
	ran := false
	h := s.authed(true, func(w http.ResponseWriter, r *http.Request) { ran = true; w.WriteHeader(204) })

	send := func(method, origin, cookie, bearerTok string) int {
		ran = false
		req := httptest.NewRequest(method, "/x", strings.NewReader("{}"))
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		if cookie != "" {
			req.AddCookie(&http.Cookie{Name: "aero_session", Value: cookie})
		}
		if bearerTok != "" {
			req.Header.Set("Authorization", "Bearer "+bearerTok)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec.Code
	}

	for name, c := range map[string]struct {
		method, origin, cookie, bearer string
		want                           int
		wantRan                        bool
	}{
		"cookie post from another site":     {"POST", "https://evil.example", "admin-tok", "", 403, false},
		"cookie post with no origin":        {"POST", "", "admin-tok", "", 403, false},
		"cookie delete from lookalike host": {"DELETE", "https://app.example.com.evil.example", "admin-tok", "", 403, false},
		"cookie post from this app":         {"POST", "https://app.example.com", "admin-tok", "", 204, true},
		"cookie read needs no origin":       {"GET", "", "admin-tok", "", 204, true},
		"bearer client needs no origin":     {"POST", "", "", "admin-tok", 204, true},
		"no credentials at all":             {"POST", "https://app.example.com", "", "", 401, false},
	} {
		if got := send(c.method, c.origin, c.cookie, c.bearer); got != c.want || ran != c.wantRan {
			t.Errorf("%s: status %d (want %d), handler ran=%v (want %v)", name, got, c.want, ran, c.wantRan)
		}
	}

	// with no public address configured, cookie writes are refused outright rather than allowed by default
	s.publicOrigin = ""
	if got := send("POST", "https://app.example.com", "admin-tok", ""); got != 403 {
		t.Errorf("cookie write with no configured origin = %d, want 403", got)
	}
}

func TestEveryRouteExceptSignInRequiresAuth(t *testing.T) {
	h := testServer().Routes()
	for _, r := range [][2]string{
		{"GET", "/api/me"}, {"GET", "/api/team"}, {"POST", "/api/invites"}, {"DELETE", "/api/invites/1"},
		{"PATCH", "/api/members/1"}, {"DELETE", "/api/members/1"}, {"POST", "/api/org/delete"}, {"GET", "/api/hosts"}, {"PATCH", "/api/hosts/h"}, {"GET", "/api/hosts/h/metrics"},
		{"GET", "/api/rules"}, {"POST", "/api/rules"}, {"PUT", "/api/rules/1"}, {"DELETE", "/api/rules/1"},
		{"GET", "/api/incidents"}, {"GET", "/api/incidents/1"}, {"POST", "/api/actions/1/decision"},
	} {
		if rec := do(h, r[0], r[1], ""); rec.Code != 401 {
			t.Errorf("%s %s without a token = %d, want 401", r[0], r[1], rec.Code)
		}
	}
}

func TestWriteRoutesRejectViewers(t *testing.T) {
	h := testServer().Routes()
	for _, r := range [][2]string{
		{"PATCH", "/api/hosts/h"}, {"POST", "/api/rules"}, {"PUT", "/api/rules/1"},
		{"DELETE", "/api/rules/1"}, {"POST", "/api/actions/1/decision"},
		{"POST", "/api/invites"}, {"DELETE", "/api/invites/1"}, {"PATCH", "/api/members/1"},
		{"DELETE", "/api/members/1"}, {"POST", "/api/org/delete"},
	} {
		if rec := do(h, r[0], r[1], "viewer-tok"); rec.Code != 403 {
			t.Errorf("%s %s as viewer = %d, want 403", r[0], r[1], rec.Code)
		}
	}
}

func TestRuleValidation(t *testing.T) {
	ok := rule{Name: "n", Metric: "cpu.usage_percent", Operator: ">", Threshold: 90, Severity: "critical"}
	if msg := ok.validate(); msg != "" {
		t.Fatalf("valid rule rejected: %s", msg)
	}
	for name, mutate := range map[string]func(*rule){
		"no name":      func(r *rule) { r.Name = "" },
		"bad operator": func(r *rule) { r.Operator = "==" },
		"bad severity": func(r *rule) { r.Severity = "fatal" },
		"negative dur": func(r *rule) { r.DurationSeconds = -1 },
		"huge dur":     func(r *rule) { r.DurationSeconds = 999999 },
	} {
		bad := ok
		mutate(&bad)
		if bad.validate() == "" {
			t.Errorf("%s was accepted", name)
		}
	}
}

func TestNewDashboardRoutesRequireAuth(t *testing.T) {
	h := testServer().Routes()
	for _, p := range []string{"/api/hosts/h/latest", "/api/approvals/pending", "/api/audit"} {
		if rec := do(h, "GET", p, ""); rec.Code != 401 {
			t.Errorf("GET %s without a token = %d, want 401", p, rec.Code)
		}
	}
}

func TestWebAppServesSPAWithCSPAndCannotEscapeItsDir(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "dist")
	os.MkdirAll(filepath.Join(dir, "assets"), 0o755)
	os.WriteFile(filepath.Join(dir, "index.html"), []byte("INDEX"), 0o644)
	os.WriteFile(filepath.Join(dir, "assets", "app.js"), []byte("JS"), 0o644)
	os.WriteFile(filepath.Join(root, "secret.txt"), []byte("SECRET"), 0o644) // outside the web dir

	s := testServer().WithWebApp(dir, []string{"http://localhost:11434"})
	h := s.Routes()

	rec := do(h, "GET", "/incidents/123", "") // client-side route: falls back to index
	if rec.Code != 200 || rec.Body.String() != "INDEX" {
		t.Fatalf("SPA fallback: %d %q", rec.Code, rec.Body.String())
	}
	csp := rec.Header().Get("Content-Security-Policy")
	for _, want := range []string{"script-src 'self'", "connect-src 'self' https://api.anthropic.com https://api.openai.com http://localhost:11434", "frame-ancestors 'none'"} {
		if !strings.Contains(csp, want) {
			t.Errorf("CSP missing %q: %s", want, csp)
		}
	}
	if rec := do(h, "GET", "/assets/app.js", ""); rec.Body.String() != "JS" || !strings.Contains(rec.Header().Get("Cache-Control"), "immutable") {
		t.Errorf("asset: %q %q", rec.Body.String(), rec.Header().Get("Cache-Control"))
	}
	for _, p := range []string{"/../secret.txt", "/assets/../../secret.txt", "/%2e%2e/secret.txt"} {
		if rec := do(h, "GET", p, ""); strings.Contains(rec.Body.String(), "SECRET") {
			t.Errorf("%s escaped the web dir", p)
		}
	}
	// the API is untouched by the static fallback
	if rec := do(h, "GET", "/api/hosts", ""); rec.Code != 401 {
		t.Errorf("/api/hosts = %d, want 401", rec.Code)
	}
}
