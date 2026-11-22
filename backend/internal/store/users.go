package store

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const SessionTTL = 12 * time.Hour

var ErrNoSession = errors.New("no valid session")

type User struct {
	ID    string `json:"id"`
	OrgID string `json:"org_id"`
	Email string `json:"email"`
	Role  string `json:"role"` // admin or viewer
}

// CreateOrg makes an organization and returns its id.
func CreateOrg(ctx context.Context, pool *pgxpool.Pool, name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 80 {
		return "", errors.New("organization name must be 1 to 80 characters")
	}
	var id string
	err := pool.QueryRow(ctx, "INSERT INTO orgs (name) VALUES ($1) RETURNING id::text", name).Scan(&id)
	return id, err
}

// SoleOrg returns the only organization, for single-team installs where naming one is just noise.
func SoleOrg(ctx context.Context, pool *pgxpool.Pool) (string, error) {
	rows, err := pool.Query(ctx, "SELECT id::text FROM orgs LIMIT 2")
	if err != nil {
		return "", err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return "", err
		}
		ids = append(ids, id)
	}
	if len(ids) != 1 {
		return "", errors.New("there is not exactly one organization: pass --org <id>, or create one with --new-org")
	}
	return ids[0], rows.Err()
}

// NewSession issues a session token valid for SessionTTL; the plaintext is returned once.
func NewSession(ctx context.Context, pool *pgxpool.Pool, userID string) (string, time.Time, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", time.Time{}, err
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	expires := time.Now().Add(SessionTTL)

	pool.Exec(ctx, "DELETE FROM sessions WHERE expires_at < now()") // opportunistic cleanup
	_, err := pool.Exec(ctx, "INSERT INTO sessions (token_hash, user_id, expires_at) VALUES ($1, $2, $3)",
		hashToken(token), userID, expires)
	return token, expires, err
}

// SessionStore looks users up by session token.
type SessionStore struct {
	Pool *pgxpool.Pool
}

func (s SessionStore) UserForSession(ctx context.Context, token string) (User, error) {
	var u User
	err := s.Pool.QueryRow(ctx, `SELECT u.id::text, u.org_id::text, u.email, u.role FROM sessions s
		JOIN users u ON u.id = s.user_id WHERE s.token_hash = $1 AND s.expires_at > now()`,
		hashToken(token)).Scan(&u.ID, &u.OrgID, &u.Email, &u.Role)
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, ErrNoSession
	}
	return u, err
}

func DeleteSession(ctx context.Context, pool *pgxpool.Pool, token string) error {
	_, err := pool.Exec(ctx, "DELETE FROM sessions WHERE token_hash = $1", hashToken(token))
	return err
}
