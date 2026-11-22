package auth

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/smhussa22/aero-agent/backend/internal/store"
	"github.com/smhussa22/aero-agent/backend/internal/testdb"
)

const clientID = "aero-test"

// fakeIdP behaves like a standards-following OpenID Connect provider: discovery, an authorize step that records
// the PKCE challenge, a token step that refuses a wrong verifier or a reused code, and signed ID tokens.
type fakeIdP struct {
	srv *httptest.Server
	key *rsa.PrivateKey

	mu    sync.Mutex
	codes map[string]issued

	// what the next sign-in will be
	claims   map[string]any
	nonce    func(real string) string // lets a test tamper with the nonce
	audience string
	expired  bool

	lastAuthorize url.Values
}

type issued struct {
	nonce, challenge string
	claims           map[string]any
	used             bool
}

func newIdP(t *testing.T) *fakeIdP {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeIdP{key: key, codes: map[string]issued{}, audience: clientID}
	mux := http.NewServeMux()
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)

	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"issuer": f.srv.URL, "authorization_endpoint": f.srv.URL + "/authorize", "token_endpoint": f.srv.URL + "/token",
			"jwks_uri": f.srv.URL + "/keys", "id_token_signing_alg_values_supported": []string{"RS256"},
		})
	})
	mux.HandleFunc("/keys", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{Key: &key.PublicKey, KeyID: "k1", Algorithm: "RS256", Use: "sig"}}})
	})
	mux.HandleFunc("/authorize", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		f.mu.Lock()
		defer f.mu.Unlock()
		f.lastAuthorize = q
		if q.Get("code_challenge_method") != "S256" || q.Get("code_challenge") == "" {
			http.Error(w, "PKCE required", http.StatusBadRequest)
			return
		}
		code := fmt.Sprintf("code-%d", len(f.codes)+1)
		f.codes[code] = issued{nonce: q.Get("nonce"), challenge: q.Get("code_challenge"), claims: f.claims}
		http.Redirect(w, r, q.Get("redirect_uri")+"?code="+code+"&state="+url.QueryEscape(q.Get("state")), http.StatusFound)
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		f.mu.Lock()
		defer f.mu.Unlock()
		in, ok := f.codes[r.Form.Get("code")]
		sum := sha256.Sum256([]byte(r.Form.Get("code_verifier")))
		if !ok || in.used || base64.RawURLEncoding.EncodeToString(sum[:]) != in.challenge {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": "invalid_grant"})
			return
		}
		in.used = true
		f.codes[r.Form.Get("code")] = in
		w.Header().Set("Content-Type", "application/json") // real providers do; without it clients cannot parse the answer

		nonce := in.nonce
		if f.nonce != nil {
			nonce = f.nonce(nonce)
		}
		exp := time.Now().Add(time.Hour)
		if f.expired {
			exp = time.Now().Add(-time.Hour)
		}
		claims := map[string]any{"iss": f.srv.URL, "aud": f.audience, "exp": exp.Unix(), "iat": time.Now().Unix(), "nonce": nonce}
		for k, v := range in.claims {
			claims[k] = v
		}
		payload, _ := json.Marshal(claims)
		signer, _ := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: key},
			(&jose.SignerOptions{}).WithType("JWT").WithHeader("kid", "k1"))
		jws, _ := signer.Sign(payload)
		tok, _ := jws.CompactSerialize()
		json.NewEncoder(w).Encode(map[string]any{"access_token": "unused", "token_type": "Bearer", "expires_in": 3600, "id_token": tok})
	})
	return f
}

type harness struct {
	t      *testing.T
	pool   *pgxpool.Pool
	idp    *fakeIdP
	app    *httptest.Server
	svc    *Service
	signup bool
}

func newHarness(t *testing.T, signupOpen bool) *harness {
	t.Helper()
	pool := testdb.Open(t)
	idp := newIdP(t)
	h := &harness{t: t, pool: pool, idp: idp}

	var handler http.Handler
	h.app = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { handler.ServeHTTP(w, r) }))
	t.Cleanup(h.app.Close)

	svc, err := New(context.Background(), pool, Config{Issuer: idp.srv.URL, ClientID: clientID, ClientSecret: "s3cret", PublicURL: h.app.URL, SignupOpen: signupOpen})
	if err != nil {
		t.Fatal(err)
	}
	h.svc = svc
	mux := http.NewServeMux()
	mux.HandleFunc("GET /auth/login", svc.Login)
	mux.HandleFunc("GET /auth/callback", svc.Callback)
	mux.HandleFunc("POST /auth/logout", svc.Logout)
	handler = mux
	return h
}

func noFollow() *http.Client {
	jar, _ := cookiejar.New(nil)
	return &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}

// signIn runs the whole browser journey, stopping at the app's final answer to the callback.
func (h *harness) signIn(client *http.Client, claims map[string]any) *http.Response {
	h.t.Helper()
	h.idp.claims = claims
	login, err := client.Get(h.app.URL + "/auth/login")
	if err != nil {
		h.t.Fatal(err)
	}
	if login.StatusCode != http.StatusFound {
		h.t.Fatalf("login: status %d", login.StatusCode)
	}
	authorize, err := client.Get(login.Header.Get("Location"))
	if err != nil {
		h.t.Fatal(err)
	}
	if authorize.StatusCode != http.StatusFound {
		h.t.Fatalf("authorize: status %d", authorize.StatusCode)
	}
	cb, err := client.Get(authorize.Header.Get("Location"))
	if err != nil {
		h.t.Fatal(err)
	}
	return cb
}

func (h *harness) sessionUser(resp *http.Response) (store.User, bool) {
	for _, c := range resp.Cookies() {
		if c.Name == SessionCookie && c.Value != "" {
			u, err := store.SessionStore{Pool: h.pool}.UserForSession(context.Background(), c.Value)
			return u, err == nil
		}
	}
	return store.User{}, false
}

func uniqueEmail(prefix string) string {
	b := make([]byte, 4)
	rand.Read(b)
	return fmt.Sprintf("%s-%x@example.test", prefix, b)
}

func claims(email string, verified bool) map[string]any {
	return map[string]any{"sub": "sub-" + email, "email": email, "email_verified": verified, "name": "Pat"}
}

func cleanupUser(t *testing.T, pool *pgxpool.Pool, email string) {
	t.Cleanup(func() {
		ctx := context.Background()
		var org string
		if pool.QueryRow(ctx, "SELECT org_id::text FROM users WHERE email = $1", email).Scan(&org) == nil {
			pool.Exec(ctx, "SELECT delete_org($1::uuid)", org)
		}
	})
}

// ---- the happy paths ----

func TestInvitedPersonSignsInAndJoinsTheInvitingOrganization(t *testing.T) {
	h := newHarness(t, false)
	email := uniqueEmail("alice")
	orgID, _, err := store.CreateInviteForNewOrg(context.Background(), h.pool, "Acme "+email, strings.ToUpper(email)) // case must not matter
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { h.pool.Exec(context.Background(), "SELECT delete_org($1::uuid)", orgID) })

	resp := h.signIn(noFollow(), claims(email, true))
	if resp.StatusCode != http.StatusFound || resp.Header.Get("Location") != "/" {
		t.Fatalf("callback: status %d to %q, want a redirect home", resp.StatusCode, resp.Header.Get("Location"))
	}

	var sess *http.Cookie
	for _, c := range resp.Cookies() {
		if c.Name == SessionCookie {
			sess = c
		}
	}
	if sess == nil || !sess.HttpOnly || sess.SameSite != http.SameSiteLaxMode || sess.Path != "/" || sess.MaxAge <= 0 {
		t.Fatalf("session cookie must be HttpOnly, SameSite=Lax and expire: %+v", sess)
	}
	u, ok := h.sessionUser(resp)
	if !ok || u.OrgID != orgID || u.Role != "admin" || u.Email != email {
		t.Fatalf("session user %+v (ok=%v), want admin of %s", u, ok, orgID)
	}

	var accepted bool
	h.pool.QueryRow(context.Background(), "SELECT accepted_at IS NOT NULL FROM invites WHERE org_id = $1::uuid", orgID).Scan(&accepted)
	if !accepted {
		t.Error("the invitation should be marked accepted")
	}

	// the same person again: same account, no duplicate
	again := h.signIn(noFollow(), claims(email, true))
	u2, _ := h.sessionUser(again)
	if u2.ID != u.ID {
		t.Errorf("second sign-in made a different account: %s vs %s", u2.ID, u.ID)
	}
}

func TestOpenSignupGivesANewOrganization(t *testing.T) {
	h := newHarness(t, true)
	email := uniqueEmail("founder")
	cleanupUser(t, h.pool, email)

	resp := h.signIn(noFollow(), claims(email, true))
	u, ok := h.sessionUser(resp)
	if !ok || u.Role != "admin" {
		t.Fatalf("open signup should create an admin, got %+v ok=%v", u, ok)
	}
	name, _ := store.OrgName(context.Background(), h.pool, u.OrgID)
	if name != "Pat's team" {
		t.Errorf("organization name %q", name)
	}
}

func TestAnExistingAccountIsBoundOnFirstProviderSignIn(t *testing.T) {
	h := newHarness(t, false)
	email := uniqueEmail("legacy")
	org, _ := store.CreateOrg(context.Background(), h.pool, "Legacy "+email)
	t.Cleanup(func() { h.pool.Exec(context.Background(), "SELECT delete_org($1::uuid)", org) })
	var id string
	h.pool.QueryRow(context.Background(), "INSERT INTO users (org_id, email, role) VALUES ($1::uuid, $2, 'viewer') RETURNING id::text", org, email).Scan(&id)

	u, ok := h.sessionUser(h.signIn(noFollow(), claims(email, true)))
	if !ok || u.ID != id || u.Role != "viewer" || u.OrgID != org {
		t.Fatalf("got %+v ok=%v, want the existing viewer %s", u, ok, id)
	}
}

// ---- everything that must not get in ----

func expectRefused(t *testing.T, h *harness, resp *http.Response, status int, email string) {
	t.Helper()
	if resp.StatusCode != status {
		t.Errorf("status %d, want %d", resp.StatusCode, status)
	}
	if _, ok := h.sessionUser(resp); ok {
		t.Error("a session was issued")
	}
	var n int
	h.pool.QueryRow(context.Background(), "SELECT count(*) FROM users WHERE email = $1", strings.ToLower(email)).Scan(&n)
	if n != 0 {
		t.Errorf("an account was created for %s", email)
	}
}

func TestNotInvitedIsRefusedWhenSignupIsClosed(t *testing.T) {
	h := newHarness(t, false)
	email := uniqueEmail("stranger")
	expectRefused(t, h, h.signIn(noFollow(), claims(email, true)), http.StatusForbidden, email)
}

func TestAnUnverifiedEmailNeverGetsIn(t *testing.T) {
	h := newHarness(t, true) // even with signup open
	email := uniqueEmail("unverified")
	expectRefused(t, h, h.signIn(noFollow(), claims(email, false)), http.StatusUnauthorized, email)
}

func TestAnInvitationIsForThatEmailAndNobodyElse(t *testing.T) {
	h := newHarness(t, false)
	invited, intruder := uniqueEmail("invited"), uniqueEmail("intruder")
	orgID, _, err := store.CreateInviteForNewOrg(context.Background(), h.pool, "Acme "+invited, invited)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { h.pool.Exec(context.Background(), "SELECT delete_org($1::uuid)", orgID) })
	expectRefused(t, h, h.signIn(noFollow(), claims(intruder, true)), http.StatusForbidden, intruder)
}

func TestAnExpiredInvitationIsRefused(t *testing.T) {
	h := newHarness(t, false)
	email := uniqueEmail("late")
	orgID, _, _ := store.CreateInviteForNewOrg(context.Background(), h.pool, "Acme "+email, email)
	t.Cleanup(func() { h.pool.Exec(context.Background(), "SELECT delete_org($1::uuid)", orgID) })
	h.pool.Exec(context.Background(), "UPDATE invites SET expires_at = now() - interval '1 minute' WHERE org_id = $1::uuid", orgID)
	expectRefused(t, h, h.signIn(noFollow(), claims(email, true)), http.StatusForbidden, email)
}

func TestTheSignInMustStartInTheSameBrowser(t *testing.T) {
	h := newHarness(t, true)
	email := uniqueEmail("phished")

	// a victim's browser starts sign-in; an attacker tries to finish it from a browser that never started one
	h.idp.claims = claims(email, true)
	victim := noFollow()
	login, _ := victim.Get(h.app.URL + "/auth/login")
	authorize, _ := victim.Get(login.Header.Get("Location"))
	callbackURL := authorize.Header.Get("Location")

	attacker := noFollow() // no flow cookie
	resp, _ := attacker.Get(callbackURL)
	expectRefused(t, h, resp, http.StatusBadRequest, email)

	// and a cookie holding a different state does not help either
	req, _ := http.NewRequest("GET", callbackURL, nil)
	req.AddCookie(&http.Cookie{Name: flowCookie, Value: "some-other-state"})
	resp, _ = noFollow().Do(req)
	expectRefused(t, h, resp, http.StatusBadRequest, email)
}

func TestACallbackCanOnlyBeUsedOnce(t *testing.T) {
	h := newHarness(t, true)
	email := uniqueEmail("replay")
	cleanupUser(t, h.pool, email)
	h.idp.claims = claims(email, true)

	c := noFollow()
	login, _ := c.Get(h.app.URL + "/auth/login")
	var flow *http.Cookie
	for _, ck := range login.Cookies() {
		if ck.Name == flowCookie {
			flow = ck
		}
	}
	if flow == nil || !flow.HttpOnly || flow.Path != "/auth" || flow.SameSite != http.SameSiteLaxMode {
		t.Fatalf("flow cookie must be HttpOnly, SameSite=Lax and scoped to /auth: %+v", flow)
	}
	authorize, _ := c.Get(login.Header.Get("Location"))
	callbackURL := authorize.Header.Get("Location")

	first, _ := c.Get(callbackURL)
	if _, ok := h.sessionUser(first); !ok {
		t.Fatalf("first use should sign in, got %d", first.StatusCode)
	}

	req, _ := http.NewRequest("GET", callbackURL, nil)
	req.AddCookie(flow) // even holding the original cookie
	again, _ := noFollow().Do(req)
	if again.StatusCode != http.StatusBadRequest {
		t.Errorf("replaying the callback: status %d, want 400", again.StatusCode)
	}
	if _, ok := h.sessionUser(again); ok {
		t.Error("a replayed callback issued a session")
	}
}

func TestAForgedIDTokenIsRejected(t *testing.T) {
	cases := map[string]func(*fakeIdP){
		"wrong nonce":    func(f *fakeIdP) { f.nonce = func(string) string { return "attacker-nonce" } },
		"wrong audience": func(f *fakeIdP) { f.audience = "another-app" },
		"expired token":  func(f *fakeIdP) { f.expired = true },
	}
	for name, tamper := range cases {
		h := newHarness(t, true)
		email := uniqueEmail("forged")
		tamper(h.idp)
		expectRefused(t, h, h.signIn(noFollow(), claims(email, true)), http.StatusUnauthorized, email)
		t.Logf("rejected: %s", name)
	}
}

func TestSignInUsesPKCEAndTheAuthorizationCodeFlow(t *testing.T) {
	h := newHarness(t, true)
	email := uniqueEmail("pkce")
	cleanupUser(t, h.pool, email)
	h.signIn(noFollow(), claims(email, true))

	q := h.idp.lastAuthorize
	if q.Get("response_type") != "code" || q.Get("code_challenge_method") != "S256" || q.Get("code_challenge") == "" {
		t.Errorf("authorize request is not code + PKCE: %v", q)
	}
	if q.Get("state") == "" || q.Get("nonce") == "" || q.Get("client_id") != clientID {
		t.Errorf("authorize request is missing state, nonce or client: %v", q)
	}
	if q.Get("redirect_uri") != h.app.URL+"/auth/callback" {
		t.Errorf("redirect_uri = %q", q.Get("redirect_uri"))
	}
}

func TestLogoutEndsTheSession(t *testing.T) {
	h := newHarness(t, true)
	email := uniqueEmail("leaving")
	cleanupUser(t, h.pool, email)

	c := noFollow()
	resp := h.signIn(c, claims(email, true))
	u, ok := h.sessionUser(resp)
	if !ok {
		t.Fatal("not signed in")
	}
	var token string
	for _, ck := range resp.Cookies() {
		if ck.Name == SessionCookie {
			token = ck.Value
		}
	}

	req, _ := http.NewRequest("POST", h.app.URL+"/auth/logout", nil)
	req.AddCookie(&http.Cookie{Name: SessionCookie, Value: token})
	out, err := c.Do(req)
	if err != nil || out.StatusCode != http.StatusNoContent {
		t.Fatalf("logout: %v %v", err, out)
	}
	if _, err := (store.SessionStore{Pool: h.pool}).UserForSession(context.Background(), token); err == nil {
		t.Errorf("session for %s still works after logout", u.Email)
	}
}
