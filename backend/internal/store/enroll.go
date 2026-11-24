package store

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"regexp"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// EnrollTTL is how long a code stays usable.
const EnrollTTL = 15 * time.Minute

var (
	ErrBadCode = errors.New("enrollment code is invalid, expired or already used")
	ErrBadName = errors.New("server name must be 1-100 letters, digits, dots, dashes or underscores")
)

var hostName = regexp.MustCompile(`^[A-Za-z0-9._-]{1,100}$`)

// CreateEnrollCode makes a single-use code for the organization. The plaintext is returned once.
func CreateEnrollCode(ctx context.Context, pool *pgxpool.Pool, orgID, userID string) (code string, expires time.Time, err error) {
	raw := make([]byte, 15) // 120 bits: unguessable within the code's lifetime
	if _, err := rand.Read(raw); err != nil {
		return "", time.Time{}, err
	}
	code = base64.RawURLEncoding.EncodeToString(raw)
	expires = time.Now().Add(EnrollTTL)
	_, err = pool.Exec(ctx, "INSERT INTO enroll_codes (code_hash, org_id, created_by, expires_at) VALUES ($1, $2::uuid, NULLIF($3, '')::uuid, $4)",
		hashToken(code), orgID, userID, expires)
	return code, expires, err
}

// RedeemEnrollCode burns the code and issues the named server's token. The code works once, even if two
// requests race for it, because the UPDATE only matches an unused row.
func RedeemEnrollCode(ctx context.Context, pool *pgxpool.Pool, code, name string) (hostID, token string, err error) {
	if !hostName.MatchString(name) {
		return "", "", ErrBadName
	}
	var orgID string
	err = pool.QueryRow(ctx, `UPDATE enroll_codes SET used_at = now()
		WHERE code_hash = $1 AND used_at IS NULL AND expires_at > now() RETURNING org_id::text`, hashToken(code)).Scan(&orgID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", ErrBadCode
	}
	if err != nil {
		return "", "", err
	}
	return IssueToken(ctx, pool, orgID, name)
}
