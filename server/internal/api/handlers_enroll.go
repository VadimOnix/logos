package api

import (
	"crypto/rand"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/VadimOnix/logos/server/internal/auth"
	"github.com/VadimOnix/logos/server/internal/store"
)

// handleEnroll implements the claim-code enrollment flow (PRD §4.2 step 5):
// the agent presents a single-use code and its public key, and receives its
// node identity plus a bearer token for the management channel. mTLS client
// certificates replace the bearer token in milestone M1.
func (s *Server) handleEnroll(w http.ResponseWriter, r *http.Request) {
	if !s.enrollLimiter.allow(clientIP(r)) {
		httpError(w, http.StatusTooManyRequests, "too many enrollment attempts, slow down")
		return
	}
	var req enrollRequest
	if !readJSON(w, r, &req) {
		return
	}
	code := auth.NormalizeClaimCode(req.Code)
	if code == "" {
		httpError(w, http.StatusBadRequest, "code is required")
		return
	}

	nodeID := newUUID()
	name := strings.TrimSpace(req.Hostname)
	if name == "" {
		name = "node-" + nodeID[:8]
	}

	// Create the node first, then consume the code referencing it. A failure
	// between the two steps leaves an unreferenced node with no token replay
	// risk: the code stays unused only if consumption failed, in which case
	// the node record is removed below.
	token := auth.NewToken()
	node, err := s.store.CreateNode(r.Context(), nodeID, name, auth.HashToken(token), store.NodeInfo{
		PublicKey:    req.PublicKey,
		Hostname:     req.Hostname,
		AgentVersion: req.AgentVersion,
		OSVersion:    req.OSVersion,
		Arch:         req.Arch,
	})
	if err != nil {
		s.internalError(w, err)
		return
	}
	if err := s.store.ConsumeClaimCode(r.Context(), auth.HashToken(code), node.ID); err != nil {
		if delErr := s.store.DeleteNode(r.Context(), node.ID); delErr != nil {
			s.log.Error("cleanup node after failed claim", "node", node.ID, "err", delErr)
		}
		if errors.Is(err, store.ErrNotFound) {
			httpError(w, http.StatusForbidden, "invalid, expired, or already used claim code")
			return
		}
		s.internalError(w, err)
		return
	}

	s.log.Info("node enrolled", "node", node.ID, "name", node.Name, "hostname", req.Hostname)
	writeJSON(w, http.StatusCreated, enrollResponse{
		NodeID:    node.ID,
		NodeName:  node.Name,
		NodeToken: token,
	})
}

// handleAgentLeave lets an agent unenroll itself (logos-agent leave, PRD §4.4).
// Auth: the node's own bearer token.
func (s *Server) handleAgentLeave(w http.ResponseWriter, r *http.Request) {
	tok := bearerToken(r)
	if tok == "" {
		httpError(w, http.StatusUnauthorized, "node token required")
		return
	}
	node, err := s.store.GetNodeByTokenHash(r.Context(), auth.HashToken(tok))
	if errors.Is(err, store.ErrNotFound) {
		httpError(w, http.StatusUnauthorized, "unknown or revoked node token")
		return
	}
	if err != nil {
		s.internalError(w, err)
		return
	}
	if err := s.store.MarkNodeLeft(r.Context(), node.ID); err != nil && !errors.Is(err, store.ErrNotFound) {
		s.internalError(w, err)
		return
	}
	s.hub.Kick(node.ID, "node left management")
	s.log.Info("node left", "node", node.ID, "name", node.Name)
	writeJSON(w, http.StatusOK, map[string]string{"status": "left"})
}

// newUUID returns a random RFC 4122 v4 UUID string.
func newUUID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
