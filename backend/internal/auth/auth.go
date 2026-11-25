// Package auth signs people in through an OpenID Connect identity provider (WorkOS AuthKit, Auth0, Clerk,
// Cognito, Keycloak, anything standard). Aero stores no passwords: the provider owns signup, email verification,
// password reset and multi-factor, and this package only runs the standard authorization-code flow with PKCE
// and turns a verified identity into a session cookie. The browser never sees a token.
package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/oauth2"

	"github.com/smhussa22/aero-agent/backend/internal/store"
)

const (
	SessionCookie = "aero_session"
	flowCookie    = "aero_flow"
	flowLifetime  = 10 * time.Minute
)

type Config struct {
	Issuer       string
	ClientID     string
	ClientSecret string
	PublicURL    string // where browsers reach this app, for example https://app.example.com
	SignupOpen   bool   // false: only invited emails can create an account
}

type Service struct {
	cfg      Config
	pool     *pgxpool.Pool
	oauth    oauth2.Config
	verifier *oidc.IDTokenVerifier
	sb       *oidc.IDTokenVerifier // set when signing in with Supabase instead of an OpenID Connect provider
	secure   bool
}

// New contacts the provider's discovery document, so a wrong issuer fails at startup rather than at first login.
func New(ctx context.Context, pool *pgxpool.Pool, cfg Config) (*Service, error) {
	cfg.PublicURL = strings.TrimRight(cfg.PublicURL, "/")
	if cfg.Issuer == "" || cfg.ClientID == "" || cfg.PublicURL == "" {
		return nil, errors.New("OIDC_ISSUER, OIDC_CLIENT_ID and PUBLIC_URL are required")
	}
	provider, err := oidc.NewProvider(ctx, cfg.Issuer)
	if err != nil {
		return nil, err
	}
	return &Service{
		cfg:  cfg,
		pool: pool,
		oauth: oauth2.Config{
			ClientID: cfg.ClientID, ClientSecret: cfg.ClientSecret,
			Endpoint:    provider.Endpoint(),
			RedirectURL: cfg.PublicURL + "/auth/callback",
			Scopes:      []string{oidc.ScopeOpenID, "email", "profile"},
		},
		verifier: provider.Verifier(&oidc.Config{ClientID: cfg.ClientID}),
		secure:   strings.HasPrefix(cfg.PublicURL, "https://"),
	}, nil
}

// Secure reports whether cookies carry the Secure flag (true unless PUBLIC_URL is plain http, for local development).
func (s *Service) Secure() bool { return s.secure }

func hash(v string) string {
	sum := sha256.Sum256([]byte(v))
	return hex.EncodeToString(sum[:])
}

func random() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func (s *Service) cookie(name, value string, maxAge int, path string) *http.Cookie {
	return &http.Cookie{Name: name, Value: value, Path: path, MaxAge: maxAge, HttpOnly: true, Secure: s.secure, SameSite: http.SameSiteLaxMode}
}

// page is deliberately plain: no inline script or style, so the strict Content-Security-Policy holds.
func page(w http.ResponseWriter, status int, title, body string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	w.Write([]byte("<!doctype html><meta charset=utf-8><meta name=viewport content=\"width=device-width,initial-scale=1\"><title>" +
		title + "</title><main style=\"font:16px system-ui;max-width:32rem;margin:4rem auto;padding:0 1rem\"><h1>" + title + "</h1><p>" + body + "</p><p><a href=\"/\">Back to Aero Agent</a></p></main>"))
}

// Login starts a sign-in: it records a one-time state, nonce and PKCE verifier, ties the browser to them with a
// cookie, and sends the browser to the provider.
func (s *Service) Login(w http.ResponseWriter, r *http.Request) {
	if s.verifier == nil {
		page(w, http.StatusNotFound, "Not available", "This server signs in with email and password on its own page.")
		return
	}
	state, err1 := random()
	nonce, err2 := random()
	if err1 != nil || err2 != nil {
		page(w, http.StatusInternalServerError, "Something went wrong", "Please try again.")
		return
	}
	verifier := oauth2.GenerateVerifier()

	s.pool.Exec(r.Context(), "DELETE FROM auth_flows WHERE expires_at < now()") // opportunistic cleanup
	if _, err := s.pool.Exec(r.Context(), "INSERT INTO auth_flows (state_hash, nonce, code_verifier, expires_at) VALUES ($1, $2, $3, $4)",
		hash(state), nonce, verifier, time.Now().Add(flowLifetime)); err != nil {
		slog.Error("could not start sign-in", "err", err)
		page(w, http.StatusInternalServerError, "Something went wrong", "Please try again.")
		return
	}
	http.SetCookie(w, s.cookie(flowCookie, state, int(flowLifetime.Seconds()), "/auth"))
	http.Redirect(w, r, s.oauth.AuthCodeURL(state, oauth2.S256ChallengeOption(verifier), oidc.Nonce(nonce)), http.StatusFound)
}

// Callback finishes a sign-in. The state must match the cookie that started it and can be used only once;
// the ID token is verified (signature, issuer, audience, expiry, nonce); the email must be verified by the provider.
func (s *Service) Callback(w http.ResponseWriter, r *http.Request) {
	if s.verifier == nil {
		page(w, http.StatusNotFound, "Not available", "This server signs in with email and password on its own page.")
		return
	}
	q := r.URL.Query()
	http.SetCookie(w, s.cookie(flowCookie, "", -1, "/auth")) // single use either way

	state := q.Get("state")
	c, err := r.Cookie(flowCookie)
	if err != nil || state == "" || subtle.ConstantTimeCompare([]byte(c.Value), []byte(state)) != 1 {
		page(w, http.StatusBadRequest, "Sign-in expired", "This sign-in did not start in this browser, or it took too long. Please start again.")
		return
	}

	var nonce, verifier string
	err = s.pool.QueryRow(r.Context(), `DELETE FROM auth_flows WHERE state_hash = $1 AND expires_at > now() RETURNING nonce, code_verifier`,
		hash(state)).Scan(&nonce, &verifier)
	if errors.Is(err, pgx.ErrNoRows) {
		page(w, http.StatusBadRequest, "Sign-in expired", "This sign-in link was already used or has expired. Please start again.")
		return
	}
	if err != nil {
		slog.Error("sign-in lookup failed", "err", err)
		page(w, http.StatusInternalServerError, "Something went wrong", "Please try again.")
		return
	}
	if q.Get("error") != "" {
		page(w, http.StatusForbidden, "Sign-in was not completed", "The sign-in provider did not complete the request.")
		return
	}

	tok, err := s.oauth.Exchange(r.Context(), q.Get("code"), oauth2.VerifierOption(verifier))
	if err != nil {
		slog.Warn("code exchange failed", "err", err)
		page(w, http.StatusBadGateway, "Sign-in failed", "The sign-in provider rejected the request. Please try again.")
		return
	}
	raw, _ := tok.Extra("id_token").(string)
	idToken, err := s.verifier.Verify(r.Context(), raw)
	if err != nil || subtle.ConstantTimeCompare([]byte(idToken.Nonce), []byte(nonce)) != 1 {
		slog.Warn("id token rejected", "err", err)
		page(w, http.StatusUnauthorized, "Sign-in failed", "The sign-in response could not be verified.")
		return
	}
	var claims struct {
		Email         string `json:"email"`
		EmailVerified bool   `json:"email_verified"`
		Name          string `json:"name"`
	}
	if err := idToken.Claims(&claims); err != nil {
		page(w, http.StatusUnauthorized, "Sign-in failed", "The sign-in response could not be read.")
		return
	}
	// access follows the email address, so it must be one the provider confirmed the person controls
	if claims.Email == "" || !claims.EmailVerified {
		page(w, http.StatusUnauthorized, "Verify your email first", "Your email address is not verified yet. Verify it with the sign-in provider, then try again.")
		return
	}

	user, err := store.SignIn(r.Context(), s.pool, store.Identity{Issuer: idToken.Issuer, Subject: idToken.Subject, Email: claims.Email, Name: claims.Name}, s.cfg.SignupOpen)
	switch {
	case errors.Is(err, store.ErrNotInvited):
		page(w, http.StatusForbidden, "Not invited yet", "This email address has not been invited. Ask an admin of your team to invite you, then sign in again.")
		return
	case errors.Is(err, store.ErrEmailTaken):
		page(w, http.StatusConflict, "Account conflict", "That email address is already linked to a different sign-in. Contact support.")
		return
	case err != nil:
		slog.Error("sign-in failed", "err", err)
		page(w, http.StatusInternalServerError, "Something went wrong", "Please try again.")
		return
	}

	session, expires, err := store.NewSession(r.Context(), s.pool, user.ID)
	if err != nil {
		slog.Error("session failed", "err", err)
		page(w, http.StatusInternalServerError, "Something went wrong", "Please try again.")
		return
	}
	http.SetCookie(w, s.cookie(SessionCookie, session, int(time.Until(expires).Seconds()), "/"))
	http.Redirect(w, r, "/", http.StatusFound)
}

// Logout ends the session here. (It does not sign the person out of the identity provider itself.)
func (s *Service) Logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(SessionCookie); err == nil {
		store.DeleteSession(r.Context(), s.pool, c.Value)
	}
	http.SetCookie(w, s.cookie(SessionCookie, "", -1, "/"))
	w.WriteHeader(http.StatusNoContent)
}
