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
	"golang.org/x/crypto/bcrypt"
)

const (
	SessionTTL     = 12 * time.Hour
	minPasswordLen = 12
	maxPasswordLen = 72 // bcrypt ignores everything past 72 bytes, so reject instead of truncating
)

var (
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrNoSession          = errors.New("no valid session")
)

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

// dummyHash makes Authenticate take the same time for unknown emails as for wrong passwords
var dummyHash, _ = bcrypt.GenerateFromPassword([]byte("dummy-password"), bcrypt.DefaultCost)

func CreateUser(ctx context.Context, pool *pgxpool.Pool, orgID, email, password, role string) error {
	if len(password) < minPasswordLen || len(password) > maxPasswordLen {
		return errors.New("password must be 12 to 72 bytes")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	_, err = pool.Exec(ctx, "INSERT INTO users (org_id, email, password_hash, role) VALUES ($1::uuid, $2, $3, $4)",
		orgID, strings.ToLower(strings.TrimSpace(email)), string(hash), role)
	return err
}

func Authenticate(ctx context.Context, pool *pgxpool.Pool, email, password string) (User, error) {
	var u User
	var hash string
	err := pool.QueryRow(ctx, "SELECT id::text, org_id::text, email, role, password_hash FROM users WHERE email = $1",
		strings.ToLower(strings.TrimSpace(email))).Scan(&u.ID, &u.OrgID, &u.Email, &u.Role, &hash)
	if errors.Is(err, pgx.ErrNoRows) {
		bcrypt.CompareHashAndPassword(dummyHash, []byte(password))
		return User{}, ErrInvalidCredentials
	}
	if err != nil {
		return User{}, err
	}
	if bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) != nil {
		return User{}, ErrInvalidCredentials
	}
	return u, nil
}

// NewSession issues a bearer token valid for SessionTTL; the plaintext is returned once.
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
