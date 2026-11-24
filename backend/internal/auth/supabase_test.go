package auth

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"

	"github.com/smhussa22/aero-agent/backend/internal/testdb"
)

// fakeSupabase serves a key set like a Supabase project does and signs tokens with its private key.
type fakeSupabase struct {
	srv *httptest.Server
	key *ecdsa.PrivateKey
}

func newFakeSupabase(t *testing.T) *fakeSupabase {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	set := jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{Key: &key.PublicKey, KeyID: "k1", Algorithm: "ES256", Use: "sig"}}}
	mux := http.NewServeMux()
	mux.HandleFunc("/auth/v1/.well-known/jwks.json", func(w http.ResponseWriter, r *http.Request) { json.NewEncoder(w).Encode(set) })
	f := &fakeSupabase{srv: httptest.NewServer(mux), key: key}
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeSupabase) token(t *testing.T, signer *ecdsa.PrivateKey, issuer, aud string, exp time.Duration, extra map[string]any) string {
	t.Helper()
	s, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.ES256, Key: jose.JSONWebKey{Key: signer, KeyID: "k1"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	std := jwt.Claims{Issuer: issuer, Subject: "user-1", Audience: jwt.Audience{aud}, Expiry: jwt.NewNumericDate(time.Now().Add(exp))}
	raw, err := jwt.Signed(s).Claims(std).Claims(extra).Serialize()
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// Everything here is refused before any database is touched, so no database is needed.
func TestSupabaseSessionRefusesBadTokens(t *testing.T) {
	f := newFakeSupabase(t)
	svc, err := NewSupabase(context.Background(), nil, f.srv.URL, "http://localhost:8080", true)
	if err != nil {
		t.Fatal(err)
	}
	good := f.srv.URL + "/auth/v1"
	other, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	no := false

	cases := map[string]string{
		"signed by someone else": f.token(t, other, good, "authenticated", time.Hour, map[string]any{"email": "a@b.example"}),
		"wrong issuer":           f.token(t, f.key, "https://evil.example/auth/v1", "authenticated", time.Hour, map[string]any{"email": "a@b.example"}),
		"wrong audience":         f.token(t, f.key, good, "anon", time.Hour, map[string]any{"email": "a@b.example"}),
		"expired":                f.token(t, f.key, good, "authenticated", -time.Hour, map[string]any{"email": "a@b.example"}),
		"no email":               f.token(t, f.key, good, "authenticated", time.Hour, map[string]any{}),
		"email not confirmed":    f.token(t, f.key, good, "authenticated", time.Hour, map[string]any{"email": "a@b.example", "user_metadata": map[string]any{"email_verified": &no}}),
		"not a token":            "garbage",
	}
	for name, tok := range cases {
		body, _ := json.Marshal(map[string]string{"access_token": tok})
		w := httptest.NewRecorder()
		svc.Session(w, httptest.NewRequest("POST", "/auth/session", strings.NewReader(string(body))))
		if w.Code != http.StatusUnauthorized {
			t.Errorf("%s: want 401, got %d (%s)", name, w.Code, w.Body.String())
		}
		if len(w.Result().Cookies()) != 0 {
			t.Errorf("%s: a refused sign-in must not set a cookie", name)
		}
	}
}

func TestSupabaseSessionSignUpAndInvites(t *testing.T) {
	pool := testdb.Open(t)
	f := newFakeSupabase(t)
	issuer := f.srv.URL + "/auth/v1"
	email := "new-" + strings.ReplaceAll(time.Now().Format("150405.000000"), ".", "") + "@signup.example"
	tok := f.token(t, f.key, issuer, "authenticated", time.Hour, map[string]any{"email": email})
	call := func(open bool) *httptest.ResponseRecorder {
		svc, err := NewSupabase(context.Background(), pool, f.srv.URL, "http://localhost:8080", open)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := json.Marshal(map[string]string{"access_token": tok})
		w := httptest.NewRecorder()
		svc.Session(w, httptest.NewRequest("POST", "/auth/session", strings.NewReader(string(body))))
		return w
	}

	// sign-up closed and nobody invited this address: refused, no cookie
	if w := call(false); w.Code != http.StatusForbidden || len(w.Result().Cookies()) != 0 {
		t.Fatalf("uninvited with sign-up closed: want 403 and no cookie, got %d", w.Code)
	}
	// sign-up open: an account and organization are created and a session cookie is set
	w := call(true)
	if w.Code != http.StatusNoContent {
		t.Fatalf("sign-up open: want 204, got %d (%s)", w.Code, w.Body.String())
	}
	cs := w.Result().Cookies()
	if len(cs) != 1 || cs[0].Name != SessionCookie || cs[0].Value == "" || !cs[0].HttpOnly {
		t.Fatalf("want one HttpOnly session cookie, got %+v", cs)
	}
	t.Cleanup(func() {
		pool.Exec(context.Background(), "DELETE FROM orgs WHERE id IN (SELECT org_id FROM users WHERE email = $1)", email)
	})
	// the same person signing in again, even with sign-up closed, gets in
	if w := call(false); w.Code != http.StatusNoContent {
		t.Fatalf("returning user: want 204, got %d", w.Code)
	}
}
