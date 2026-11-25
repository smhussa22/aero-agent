package api

import (
	_ "embed"
	"errors"
	"fmt"
	"net"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/smhussa22/aero-agent/backend/internal/store"
)

//go:embed install.sh
var installScript []byte

// WithGateway sets the address new servers are told to stream metrics to (host:port).
func (s *Server) WithGateway(addr string) *Server {
	s.gateway = addr
	return s
}

func (s *Server) handleInstallScript(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/x-shellscript; charset=utf-8")
	w.Write(installScript)
}

// handleCreateEnrollment mints a code and the one-line command that uses it.
func (s *Server) handleCreateEnrollment(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r.Context())
	code, expires, err := store.CreateEnrollCode(r.Context(), s.pool, u.OrgID, u.ID)
	if err != nil {
		fail(w, err)
		return
	}
	s.auditNow(r.Context(), u.OrgID, u.Email, "enroll_code_created", map[string]any{})
	cmd := fmt.Sprintf("curl -fsSL %s/install.sh | sudo sh -s -- --server %s --code %s", s.publicURL, s.publicURL, code)
	writeJSON(w, http.StatusCreated, map[string]any{"code": code, "expires_at": expires, "command": cmd})
}

// handleEnroll is public: the code is the credential. It is rate limited per address like sign-in.
func (s *Server) handleEnroll(w http.ResponseWriter, r *http.Request) {
	if s.gateway == "" {
		writeErr(w, http.StatusServiceUnavailable, "this server has no gateway address configured")
		return
	}
	if !s.signin.allow(clientIP(r)) {
		writeErr(w, http.StatusTooManyRequests, "too many attempts, wait a minute")
		return
	}
	var in struct {
		Code string `json:"code"`
		Name string `json:"name"`
	}
	if !decode(w, r, &in) {
		return
	}
	hostID, token, err := store.RedeemEnrollCode(r.Context(), s.pool, in.Code, in.Name)
	switch {
	case errors.Is(err, store.ErrBadName):
		writeErr(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, store.ErrBadCode):
		writeErr(w, http.StatusForbidden, err.Error())
	case err != nil:
		fail(w, err)
	default:
		writeJSON(w, http.StatusOK, map[string]string{"host_id": hostID, "token": token, "gateway": s.gateway, "orchestrator": orchestratorAddr(s.gateway)})
	}
}

// WithReleases serves the agent binaries, their checksum list and its signature from dir at /releases/, so a new
// server downloads them from the same place it enrolls and installing needs nothing from the outside.
func (s *Server) WithReleases(dir string) *Server {
	s.releasesDir = dir
	return s
}

func (s *Server) handleReleases(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("file")
	if name == "" || strings.ContainsAny(name, "/\\") || strings.HasPrefix(name, ".") {
		http.NotFound(w, r)
		return
	}
	http.ServeFile(w, r, filepath.Join(s.releasesDir, name))
}

// orchestratorAddr is where the remediation agent connects: the gateway's host on the orchestrator's port.
func orchestratorAddr(gateway string) string {
	host, _, err := net.SplitHostPort(gateway)
	if err != nil {
		return ""
	}
	return net.JoinHostPort(host, "50052")
}
