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

func (s TokenStore) IdentityForToken(ctx context.Context, token string) (ingest.Identity, error) {
	var id ingest.Identity
	err := s.Pool.QueryRow(ctx, "SELECT id, org_id::text FROM hosts WHERE token_hash = $1", hashToken(token)).Scan(&id.HostID, &id.OrgID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ingest.Identity{}, ingest.ErrUnknownToken
	}
	return id, err
}

// IssueToken creates the host in the organization if needed (the server picks its id) and replaces its
// token. The plaintext is returned once and never stored, so rotating is the only way to recover a lost token.
func IssueToken(ctx context.Context, pool *pgxpool.Pool, orgID, name string) (hostID, token string, err error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", "", err
	}
	token = base64.RawURLEncoding.EncodeToString(raw)

	err = pool.QueryRow(ctx, `INSERT INTO hosts (id, org_id, name, token_hash) VALUES (gen_random_uuid()::text, $1::uuid, $2, $3)
		ON CONFLICT (org_id, name) DO UPDATE SET token_hash = $3 RETURNING id`, orgID, name, hashToken(token)).Scan(&hostID)
	return hostID, token, err
}
