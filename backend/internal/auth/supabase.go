package auth

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/smhussa22/aero-agent/backend/internal/store"
)

// NewSupabase signs people in with Supabase Auth: the browser signs up or in with Supabase directly (so the form stays
// on this site), then hands the access token here. We check its signature against the project's public keys and turn
// it into the same session cookie the rest of the app already uses. Aero still stores no passwords.
// projectURL is like https://abcd1234.supabase.co. The project must use asymmetric signing keys (the default on new
// projects) and have "Confirm email" switched on, so an email address is proven before it can claim an invitation.
func NewSupabase(ctx context.Context, pool *pgxpool.Pool, projectURL, publicURL string, signupOpen bool) (*Service, error) {
	projectURL = strings.TrimRight(projectURL, "/")
	if projectURL == "" || publicURL == "" {
		return nil, errors.New("SUPABASE_URL and PUBLIC_URL are required")
	}
	issuer := projectURL + "/auth/v1"
	keys := oidc.NewRemoteKeySet(ctx, issuer+"/.well-known/jwks.json")
	return &Service{
		cfg:  Config{Issuer: issuer, PublicURL: strings.TrimRight(publicURL, "/"), SignupOpen: signupOpen},
		pool: pool,
		// Supabase puts "authenticated" in the audience of a signed-in user's token
		sb:     oidc.NewVerifier(issuer, keys, &oidc.Config{ClientID: "authenticated", SupportedSigningAlgs: []string{"ES256", "RS256"}}),
		secure: strings.HasPrefix(publicURL, "https://"),
	}, nil
}

func jsonError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

// Session trades a verified Supabase access token for this app's session cookie.
func (s *Service) Session(w http.ResponseWriter, r *http.Request) {
	if s.sb == nil {
		jsonError(w, http.StatusNotFound, "Supabase sign-in is not configured")
		return
	}
	var in struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || in.AccessToken == "" {
		jsonError(w, http.StatusBadRequest, "invalid request")
		return
	}
	tok, err := s.sb.Verify(r.Context(), in.AccessToken)
	if err != nil {
		slog.Warn("supabase token rejected", "err", err)
		jsonError(w, http.StatusUnauthorized, "could not verify your sign-in, please try again")
		return
	}
	var claims struct {
		Email        string `json:"email"`
		UserMetadata struct {
			EmailVerified *bool  `json:"email_verified"`
			Name          string `json:"name"`
		} `json:"user_metadata"`
	}
	if err := tok.Claims(&claims); err != nil || claims.Email == "" {
		jsonError(w, http.StatusUnauthorized, "your account has no email address")
		return
	}
	// access follows the email address, so one Supabase says is unconfirmed is refused
	if claims.UserMetadata.EmailVerified != nil && !*claims.UserMetadata.EmailVerified {
		jsonError(w, http.StatusUnauthorized, "confirm your email address first, using the link we sent you")
		return
	}

	user, err := store.SignIn(r.Context(), s.pool, store.Identity{Issuer: tok.Issuer, Subject: tok.Subject, Email: claims.Email, Name: claims.UserMetadata.Name}, s.cfg.SignupOpen)
	switch {
	case errors.Is(err, store.ErrNotInvited):
		jsonError(w, http.StatusForbidden, "this email address has not been invited yet; ask an admin of your team to invite you")
		return
	case errors.Is(err, store.ErrEmailTaken):
		jsonError(w, http.StatusConflict, "that email address is already linked to a different sign-in")
		return
	case err != nil:
		slog.Error("sign-in failed", "err", err)
		jsonError(w, http.StatusInternalServerError, "something went wrong, please try again")
		return
	}
	session, expires, err := store.NewSession(r.Context(), s.pool, user.ID)
	if err != nil {
		slog.Error("session failed", "err", err)
		jsonError(w, http.StatusInternalServerError, "something went wrong, please try again")
		return
	}
	http.SetCookie(w, s.cookie(SessionCookie, session, int(time.Until(expires).Seconds()), "/"))
	w.WriteHeader(http.StatusNoContent)
}
