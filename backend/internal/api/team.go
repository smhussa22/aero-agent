package api

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/smhussa22/aero-agent/backend/internal/auth"
	"github.com/smhussa22/aero-agent/backend/internal/store"
)

// auditNow records an event outside a larger transaction.
func (s *Server) auditNow(ctx context.Context, orgID, actor, event string, details map[string]any) {
	if _, err := s.pool.Exec(ctx, "INSERT INTO audit_log (org_id, actor, event, details) VALUES ($1::uuid, $2, $3, $4)", orgID, actor, event, details); err != nil {
		slog.Error("audit write failed", "event", event, "err", err)
	}
}

// teamError turns the store's well-known errors into plain answers.
func teamError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, store.ErrBadEmail), errors.Is(err, store.ErrBadRole):
		writeErr(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, store.ErrEmailTaken), errors.Is(err, store.ErrLastAdmin):
		writeErr(w, http.StatusConflict, err.Error())
	case errors.Is(err, store.ErrSelf):
		writeErr(w, http.StatusForbidden, err.Error())
	case errors.Is(err, store.ErrNotFound):
		writeErr(w, http.StatusNotFound, "not found")
	default:
		fail(w, err)
	}
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r.Context())
	name, err := store.OrgName(r.Context(), s.pool, u.OrgID)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": u.ID, "email": u.Email, "role": u.Role, "org": map[string]string{"id": u.OrgID, "name": name}})
}

func (s *Server) handleTeam(w http.ResponseWriter, r *http.Request) {
	members, invites, err := store.ListTeam(r.Context(), s.pool, userFrom(r.Context()).OrgID)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"members": members, "invites": invites})
}

func (s *Server) handleInvite(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Email string `json:"email"`
		Role  string `json:"role"`
	}
	if !decode(w, r, &in) {
		return
	}
	u := userFrom(r.Context())
	inv, err := store.CreateInvite(r.Context(), s.pool, u.OrgID, u.ID, in.Email, in.Role)
	if err != nil {
		teamError(w, err)
		return
	}
	s.auditNow(r.Context(), u.OrgID, u.Email, "invite_created", map[string]any{"email": inv.Email, "role": inv.Role})

	// the invitation works by email address, so the email only tells them where to sign in; if no mail
	// provider is configured the admin is told to pass the address on themselves
	emailed := false
	if s.mailer != nil {
		org, _ := store.OrgName(r.Context(), s.pool, u.OrgID)
		text := fmt.Sprintf("%s invited you to join %q on Aero Agent.\n\nSign in at %s using this email address (%s) to accept.\n\nThis invitation expires in 7 days.", u.Email, org, s.publicURL, inv.Email)
		if err := s.mailer.Send(r.Context(), inv.Email, "You're invited to Aero Agent", text); err != nil {
			slog.Warn("invitation email failed", "err", err)
		} else {
			emailed = true
		}
	}
	writeJSON(w, http.StatusCreated, map[string]any{"invite": inv, "emailed": emailed, "sign_in_url": s.publicURL})
}

func (s *Server) handleRevokeInvite(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r.Context())
	if err := store.RevokeInvite(r.Context(), s.pool, u.OrgID, r.PathValue("id")); err != nil {
		teamError(w, err)
		return
	}
	s.auditNow(r.Context(), u.OrgID, u.Email, "invite_revoked", map[string]any{"invite": r.PathValue("id")})
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleChangeMember(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Role string `json:"role"`
	}
	if !decode(w, r, &in) {
		return
	}
	if in.Role == "" {
		writeErr(w, http.StatusBadRequest, store.ErrBadRole.Error())
		return
	}
	u := userFrom(r.Context())
	if err := store.ChangeMember(r.Context(), s.pool, u.OrgID, u.ID, r.PathValue("id"), in.Role); err != nil {
		teamError(w, err)
		return
	}
	s.auditNow(r.Context(), u.OrgID, u.Email, "member_role_changed", map[string]any{"member": r.PathValue("id"), "role": in.Role})
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleRemoveMember(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r.Context())
	if err := store.ChangeMember(r.Context(), s.pool, u.OrgID, u.ID, r.PathValue("id"), ""); err != nil {
		teamError(w, err)
		return
	}
	s.auditNow(r.Context(), u.OrgID, u.Email, "member_removed", map[string]any{"member": r.PathValue("id")})
	w.WriteHeader(http.StatusNoContent)
}

// handleDeleteOrg erases the organization and everything in it. The caller must type its exact name.
func (s *Server) handleDeleteOrg(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Confirm string `json:"confirm"`
	}
	if !decode(w, r, &in) {
		return
	}
	u := userFrom(r.Context())
	name, err := store.OrgName(r.Context(), s.pool, u.OrgID)
	if err != nil {
		fail(w, err)
		return
	}
	if in.Confirm != name {
		writeErr(w, http.StatusBadRequest, "type the organization's exact name to confirm")
		return
	}
	if err := store.DeleteOrg(r.Context(), s.pool, u.OrgID); err != nil {
		fail(w, err)
		return
	}
	slog.Info("organization deleted", "org", u.OrgID, "by", u.Email)
	http.SetCookie(w, &http.Cookie{Name: auth.SessionCookie, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteLaxMode})
	w.WriteHeader(http.StatusNoContent)
}
