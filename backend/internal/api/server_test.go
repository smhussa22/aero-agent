package api

import (
	"context"
	"net/http"
	"net/http/httptest"
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
		login:   newLimiters(0, 2),
		perUser: newLimiters(1000, 1000),
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

func TestLoginIsRateLimitedPerIP(t *testing.T) {
	s := testServer()
	// the limiter runs before any database access, so a nil pool is fine for the rejected request
	s.login = newLimiters(0, 0)
	rec := do(http.HandlerFunc(s.handleLogin), "POST", "/api/login", "")
	if rec.Code != 429 {
		t.Fatalf("status=%d", rec.Code)
	}
}

func TestEveryRouteExceptLoginRequiresAuth(t *testing.T) {
	h := testServer().Routes()
	for _, r := range [][2]string{
		{"POST", "/api/logout"}, {"GET", "/api/hosts"}, {"PATCH", "/api/hosts/h"}, {"GET", "/api/hosts/h/metrics"},
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
