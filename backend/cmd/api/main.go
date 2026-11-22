// Command api serves the REST API for the web app.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/smhussa22/aero-agent/backend/internal/api"
	"github.com/smhussa22/aero-agent/backend/internal/auth"
	"github.com/smhussa22/aero-agent/backend/internal/mail"
)

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func main() {
	addr := env("API_ADDR", ":8080")
	dbURL := env("DATABASE_URL", "postgres://aero:aero@localhost:5432/aero")
	certFile, keyFile := os.Getenv("TLS_CERT_FILE"), os.Getenv("TLS_KEY_FILE")
	useTLS := certFile != "" && keyFile != ""

	// fail closed: logins and bearer tokens must not cross the network in cleartext by accident
	if !useTLS && os.Getenv("INSECURE_DEV") != "1" {
		slog.Error("set TLS_CERT_FILE and TLS_KEY_FILE (or INSECURE_DEV=1 for local development)")
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		slog.Error("connect failed", "err", err)
		os.Exit(1)
	}
	defer pool.Close()

	server := api.NewServer(pool)

	// sign-in goes through an OpenID Connect provider (WorkOS AuthKit, Auth0, Clerk, Cognito, Keycloak, ...)
	if issuer := os.Getenv("OIDC_ISSUER"); issuer != "" {
		public := os.Getenv("PUBLIC_URL")
		svc, err := auth.New(ctx, pool, auth.Config{
			Issuer: issuer, ClientID: os.Getenv("OIDC_CLIENT_ID"), ClientSecret: os.Getenv("OIDC_CLIENT_SECRET"),
			PublicURL: public, SignupOpen: os.Getenv("SIGNUP_OPEN") == "1",
		})
		if err != nil {
			slog.Error("identity provider setup failed", "err", err)
			os.Exit(1)
		}
		server.WithAuth(svc, public)
		slog.Info("sign-in enabled", "issuer", issuer, "signup_open", os.Getenv("SIGNUP_OPEN") == "1")
	} else {
		slog.Warn("OIDC_ISSUER is not set: nobody can sign in")
	}
	if key := os.Getenv("RESEND_API_KEY"); key != "" {
		server.WithMailer(mail.Resend{APIKey: key, From: os.Getenv("MAIL_FROM")})
	}
	// WEB_DIR serves the built web app from this same origin, so the browser never needs CORS
	if dir := os.Getenv("WEB_DIR"); dir != "" {
		var extra []string
		for _, o := range strings.Split(os.Getenv("WEB_CONNECT_EXTRA"), ",") {
			if o = strings.TrimSpace(o); o != "" {
				extra = append(extra, o)
			}
		}
		server.WithWebApp(dir, extra)
	}

	srv := &http.Server{
		Addr:              addr,
		Handler:           server.Routes(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		srv.Shutdown(shutdown)
	}()

	slog.Info("api listening", "addr", addr, "tls", useTLS)
	if useTLS {
		err = srv.ListenAndServeTLS(certFile, keyFile)
	} else {
		slog.Warn("serving WITHOUT TLS; logins and tokens cross the network in cleartext")
		err = srv.ListenAndServe()
	}
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		slog.Error("serve failed", "err", err)
		os.Exit(1)
	}
}
