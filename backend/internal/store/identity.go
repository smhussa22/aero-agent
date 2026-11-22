package store

import (
	"context"
	"errors"
	"fmt"
	"net/mail"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrNotInvited  = errors.New("this email address has not been invited")
	ErrEmailTaken  = errors.New("that email address already belongs to an account")
	ErrLastAdmin   = errors.New("an organization must keep at least one admin")
	ErrSelf        = errors.New("you cannot change your own membership")
	ErrBadEmail    = errors.New("that does not look like an email address")
	ErrBadRole     = errors.New("role must be admin or viewer")
	ErrNotFound    = errors.New("not found")
	InviteLifetime = 7 * 24 * time.Hour
	maxEmailLen    = 254
	validRoles     = map[string]bool{"admin": true, "viewer": true}
)

// Identity is what the identity provider vouched for. The email must already be verified by the provider.
type Identity struct {
	Issuer  string
	Subject string
	Email   string
	Name    string
}

func normalizeEmail(e string) (string, error) {
	e = strings.ToLower(strings.TrimSpace(e))
	if e == "" || len(e) > maxEmailLen {
		return "", ErrBadEmail
	}
	a, err := mail.ParseAddress(e)
	if err != nil || a.Address != e || strings.ContainsAny(e, " ,;<>") {
		return "", ErrBadEmail
	}
	return e, nil
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

// SignIn decides who a verified identity is, in this order:
//  1. an account already bound to this provider identity;
//  2. an older account with the same verified email, which is bound now;
//  3. an invitation sent to that verified email, which is accepted now;
//  4. a brand-new organization, but only when signupOpen is true.
//
// Anything else is ErrNotInvited. Nobody gets in by guessing a link: access follows the verified email.
func SignIn(ctx context.Context, pool *pgxpool.Pool, id Identity, signupOpen bool) (User, error) {
	email, err := normalizeEmail(id.Email)
	if err != nil {
		return User{}, err
	}
	if id.Issuer == "" || id.Subject == "" {
		return User{}, errors.New("identity is missing its issuer or subject")
	}

	var u User
	err = withTx(ctx, pool, func(tx pgx.Tx) error {
		// 1. already known
		err := tx.QueryRow(ctx, `SELECT id::text, org_id::text, email, role FROM users WHERE idp_issuer = $1 AND idp_subject = $2`,
			id.Issuer, id.Subject).Scan(&u.ID, &u.OrgID, &u.Email, &u.Role)
		if err == nil {
			return nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}

		// 2. an account that predates the provider, matched on the verified email and not yet bound
		err = tx.QueryRow(ctx, `UPDATE users SET idp_issuer = $1, idp_subject = $2
			WHERE email = $3 AND idp_subject IS NULL RETURNING id::text, org_id::text, email, role`,
			id.Issuer, id.Subject, email).Scan(&u.ID, &u.OrgID, &u.Email, &u.Role)
		if err == nil {
			return nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}

		// 3. an invitation to this email
		var inviteID, org, role string
		err = tx.QueryRow(ctx, `SELECT id::text, org_id::text, role FROM invites
			WHERE email = $1 AND accepted_at IS NULL AND expires_at > now() ORDER BY created_at LIMIT 1 FOR UPDATE`, email).Scan(&inviteID, &org, &role)
		switch {
		case err == nil:
			if _, err := tx.Exec(ctx, "UPDATE invites SET accepted_at = now() WHERE id = $1::uuid", inviteID); err != nil {
				return err
			}
		case errors.Is(err, pgx.ErrNoRows):
			// 4. a fresh organization, only if the operator opened signup
			if !signupOpen {
				return ErrNotInvited
			}
			name := strings.TrimSpace(id.Name)
			if name == "" {
				name = strings.SplitN(email, "@", 2)[0]
			}
			team := fmt.Sprintf("%s's team", name)
			if len(team) > 80 {
				team = team[:80]
			}
			if err := tx.QueryRow(ctx, "INSERT INTO orgs (name) VALUES ($1) RETURNING id::text", strings.ToValidUTF8(team, "")).Scan(&org); err != nil {
				return err
			}
			role = "admin"
		default:
			return err
		}

		err = tx.QueryRow(ctx, `INSERT INTO users (org_id, email, role, idp_issuer, idp_subject) VALUES ($1::uuid, $2, $3, $4, $5)
			RETURNING id::text, org_id::text, email, role`, org, email, role, id.Issuer, id.Subject).Scan(&u.ID, &u.OrgID, &u.Email, &u.Role)
		if isUniqueViolation(err) {
			return ErrEmailTaken
		}
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `INSERT INTO audit_log (org_id, actor, event, details) VALUES ($1::uuid, $2, 'user_joined', $3)`,
			u.OrgID, u.Email, map[string]any{"role": u.Role})
		return err
	})
	return u, err
}

func withTx(ctx context.Context, pool *pgxpool.Pool, fn func(pgx.Tx) error) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

type Member struct {
	ID        string    `json:"id"`
	Email     string    `json:"email"`
	Role      string    `json:"role"`
	CreatedAt time.Time `json:"created_at"`
}

type Invite struct {
	ID        string    `json:"id"`
	Email     string    `json:"email"`
	Role      string    `json:"role"`
	ExpiresAt time.Time `json:"expires_at"`
}

// CreateInvite invites an email address to the organization. Inviting the same address again refreshes it.
func CreateInvite(ctx context.Context, pool *pgxpool.Pool, orgID, createdBy, email, role string) (Invite, error) {
	e, err := normalizeEmail(email)
	if err != nil {
		return Invite{}, err
	}
	if !validRoles[role] {
		return Invite{}, ErrBadRole
	}
	var exists bool
	if err := pool.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM users WHERE email = $1)", e).Scan(&exists); err != nil {
		return Invite{}, err
	}
	if exists {
		return Invite{}, ErrEmailTaken
	}
	var inv Invite
	err = pool.QueryRow(ctx, `INSERT INTO invites (org_id, email, role, created_by, expires_at) VALUES ($1::uuid, $2, $3, NULLIF($4, '')::uuid, $5)
		ON CONFLICT (org_id, email) WHERE accepted_at IS NULL DO UPDATE SET role = $3, expires_at = $5
		RETURNING id::text, email, role, expires_at`, orgID, e, role, createdBy, time.Now().Add(InviteLifetime)).Scan(&inv.ID, &inv.Email, &inv.Role, &inv.ExpiresAt)
	return inv, err
}

func ListTeam(ctx context.Context, pool *pgxpool.Pool, orgID string) ([]Member, []Invite, error) {
	members := []Member{}
	rows, err := pool.Query(ctx, "SELECT id::text, email, role, created_at FROM users WHERE org_id = $1::uuid ORDER BY created_at", orgID)
	if err != nil {
		return nil, nil, err
	}
	for rows.Next() {
		var m Member
		if err := rows.Scan(&m.ID, &m.Email, &m.Role, &m.CreatedAt); err != nil {
			rows.Close()
			return nil, nil, err
		}
		members = append(members, m)
	}
	rows.Close()

	invites := []Invite{}
	rows, err = pool.Query(ctx, `SELECT id::text, email, role, expires_at FROM invites
		WHERE org_id = $1::uuid AND accepted_at IS NULL AND expires_at > now() ORDER BY created_at`, orgID)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var i Invite
		if err := rows.Scan(&i.ID, &i.Email, &i.Role, &i.ExpiresAt); err != nil {
			return nil, nil, err
		}
		invites = append(invites, i)
	}
	return members, invites, rows.Err()
}

func RevokeInvite(ctx context.Context, pool *pgxpool.Pool, orgID, inviteID string) error {
	tag, err := pool.Exec(ctx, "DELETE FROM invites WHERE id = $1::uuid AND org_id = $2::uuid AND accepted_at IS NULL", inviteID, orgID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ChangeMember removes a member (role == "") or changes their role, inside the caller's organization only.
// It refuses to touch the caller, and refuses to leave the organization without an admin.
func ChangeMember(ctx context.Context, pool *pgxpool.Pool, orgID, actorID, memberID, role string) error {
	if role != "" && !validRoles[role] {
		return ErrBadRole
	}
	if actorID == memberID {
		return ErrSelf
	}
	return withTx(ctx, pool, func(tx pgx.Tx) error {
		var current string
		// lock the organization's admins so two concurrent changes cannot each think another admin remains
		rows, err := tx.Query(ctx, "SELECT id::text, role FROM users WHERE org_id = $1::uuid FOR UPDATE", orgID)
		if err != nil {
			return err
		}
		admins, found := 0, false
		for rows.Next() {
			var id, r string
			if err := rows.Scan(&id, &r); err != nil {
				rows.Close()
				return err
			}
			if r == "admin" && id != memberID {
				admins++
			}
			if id == memberID {
				found, current = true, r
			}
		}
		rows.Close()
		if !found {
			return ErrNotFound
		}
		if current == "admin" && role != "admin" && admins == 0 {
			return ErrLastAdmin
		}
		if role == "" {
			_, err = tx.Exec(ctx, "DELETE FROM users WHERE id = $1::uuid AND org_id = $2::uuid", memberID, orgID)
		} else {
			_, err = tx.Exec(ctx, "UPDATE users SET role = $1 WHERE id = $2::uuid AND org_id = $3::uuid", role, memberID, orgID)
		}
		return err
	})
}

// DeleteOrg erases the organization and all of its data, including its audit entries.
func DeleteOrg(ctx context.Context, pool *pgxpool.Pool, orgID string) error {
	return withTx(ctx, pool, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, "SELECT delete_org($1::uuid)", orgID)
		return err
	})
}

// OrgName returns an organization's name.
func OrgName(ctx context.Context, pool *pgxpool.Pool, orgID string) (string, error) {
	var name string
	err := pool.QueryRow(ctx, "SELECT name FROM orgs WHERE id = $1::uuid", orgID).Scan(&name)
	return name, err
}

// CreateInviteForNewOrg bootstraps a brand-new organization with its first admin invitation, for operators.
func CreateInviteForNewOrg(ctx context.Context, pool *pgxpool.Pool, orgName, email string) (orgID string, inv Invite, err error) {
	if orgID, err = CreateOrg(ctx, pool, orgName); err != nil {
		return "", Invite{}, err
	}
	inv, err = CreateInvite(ctx, pool, orgID, "", email, "admin")
	return orgID, inv, err
}
