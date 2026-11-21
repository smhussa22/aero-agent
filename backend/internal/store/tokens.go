package store

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/smhussa22/aero-agent/backend/internal/ingest"
)

// hashToken is what the database stores; tokens are 256-bit random, so a plain SHA-256 is enough
func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// TokenStore implements ingest.TokenVerifier on the hosts table.
type TokenStore struct {
	Pool *pgxpool.Pool
}

func (s TokenStore) HostForToken(ctx context.Context, token string) (string, error) {
	var host string
	err := s.Pool.QueryRow(ctx, "SELECT id FROM hosts WHERE token_hash = $1", hashToken(token)).Scan(&host)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ingest.ErrUnknownToken
	}
	return host, err
}

// IssueToken creates the host if needed and replaces its token. The plaintext is returned
// once and never stored, so rotating is the only way to recover a lost token.
func IssueToken(ctx context.Context, pool *pgxpool.Pool, hostID string) (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	token := base64.RawURLEncoding.EncodeToString(raw)

	_, err := pool.Exec(ctx, `INSERT INTO hosts (id, token_hash) VALUES ($1, $2)
		ON CONFLICT (id) DO UPDATE SET token_hash = $2`, hostID, hashToken(token))
	return token, err
}
