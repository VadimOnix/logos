package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/VadimOnix/logos/server/internal/store"
)

// nodeView is the API representation of a node, with liveness computed from
// the hub (a node is online iff its management channel is up).
type nodeView struct {
	ID           string          `json:"id"`
	Name         string          `json:"name"`
	Hostname     string          `json:"hostname"`
	AgentVersion string          `json:"agent_version"`
	OSVersion    string          `json:"os_version"`
	Arch         string          `json:"arch"`
	Status       string          `json:"status"` // online | offline | left
	EnrolledAt   time.Time       `json:"enrolled_at"`
	LeftAt       *time.Time      `json:"left_at,omitempty"`
	LastSeenAt   *time.Time      `json:"last_seen_at,omitempty"`
	Metrics      json.RawMessage `json:"metrics,omitempty"`
	// ConfigDrift is true when the live config fingerprint differs from the
	// accepted baseline — the config changed outside Logos (v1.0 drift
	// detection).
	ConfigDrift bool `json:"config_drift,omitempty"`
}

func (s *Server) nodeView(n *store.Node) nodeView {
	v := nodeView{
		ID:           n.ID,
		Name:         n.Name,
		Hostname:     n.Hostname,
		AgentVersion: n.AgentVersion,
		OSVersion:    n.OSVersion,
		Arch:         n.Arch,
		EnrolledAt:   n.EnrolledAt,
		LeftAt:       n.LeftAt,
		LastSeenAt:   n.LastSeenAt,
		Metrics:      n.LastMetrics,
	}
	switch {
	case n.Status == store.NodeStatusLeft:
		v.Status = "left"
	case s.hub.IsOnline(n.ID):
		v.Status = "online"
	default:
		v.Status = "offline"
	}
	v.ConfigDrift = configDrift(n.ConfigBaselineHash, n.LastMetrics)
	return v
}

// configDrift compares the accepted baseline against the hash in the latest
// heartbeat. No baseline or no reported hash means "no drift" — absence of
// evidence, not evidence of change.
func configDrift(baseline *string, lastMetrics []byte) bool {
	if baseline == nil {
		return false
	}
	current := store.ConfigHashFromMetrics(lastMetrics)
	return current != "" && current != *baseline
}

func (s *Server) handleListNodes(w http.ResponseWriter, r *http.Request, _ *store.User) {
	nodes, err := s.store.ListNodes(r.Context())
	if err != nil {
		s.internalError(w, err)
		return
	}
	out := make([]nodeView, 0, len(nodes))
	for _, n := range nodes {
		out = append(out, s.nodeView(n))
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleGetNode(w http.ResponseWriter, r *http.Request, _ *store.User) {
	n, err := s.store.GetNode(r.Context(), r.PathValue("id"))
	if errors.Is(err, store.ErrNotFound) {
		httpError(w, http.StatusNotFound, "node not found")
		return
	}
	if err != nil {
		s.internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, s.nodeView(n))
}

// handleRemoveNode is the panel-side "Remove from management" (PRD §4.4):
// revokes the node's token, marks it left, and tells a connected agent to
// unenroll itself.
func (s *Server) handleRemoveNode(w http.ResponseWriter, r *http.Request, u *store.User) {
	id := r.PathValue("id")
	switch err := s.store.MarkNodeLeft(r.Context(), id); {
	case errors.Is(err, store.ErrNotFound):
		httpError(w, http.StatusNotFound, "node not found or already left")
		return
	case err != nil:
		s.internalError(w, err)
		return
	}
	s.hub.Kick(id, "removed from management in the panel")
	s.audit(r.Context(), u, "node.remove", id, "")
	s.log.Info("node removed from management", "node", id)
	writeJSON(w, http.StatusOK, map[string]string{"status": "left"})
}

// handleRenameNode changes the display name. Overlays the node belongs to
// are re-synced in the background: overlay-DNS labels derive from the name.
func (s *Server) handleRenameNode(w http.ResponseWriter, r *http.Request, u *store.User) {
	id := r.PathValue("id")
	var req struct {
		Name string `json:"name"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" || len(req.Name) > 64 {
		httpError(w, http.StatusBadRequest, "name is required (max 64 chars)")
		return
	}
	switch err := s.store.RenameNode(r.Context(), id, req.Name); {
	case errors.Is(err, store.ErrNotFound):
		httpError(w, http.StatusNotFound, "node not found")
		return
	case err != nil:
		s.internalError(w, err)
		return
	}
	if ids, err := s.store.ListNodeOverlayIDs(r.Context(), id); err == nil && len(ids) > 0 {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 4*overlaySyncTimeout)
			defer cancel()
			for _, oid := range ids {
				s.syncOverlay(ctx, oid)
			}
		}()
	}
	s.audit(r.Context(), u, "node.rename", id, req.Name)
	writeJSON(w, http.StatusOK, map[string]string{"name": req.Name})
}

// handleAcceptConfigBaseline adopts the node's current config as the new
// drift baseline — the operator reviewed the out-of-band change and blessed
// it (v1.0 drift detection).
func (s *Server) handleAcceptConfigBaseline(w http.ResponseWriter, r *http.Request, u *store.User) {
	id := r.PathValue("id")
	n, err := s.store.GetNode(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		httpError(w, http.StatusNotFound, "node not found")
		return
	}
	if err != nil {
		s.internalError(w, err)
		return
	}
	current := store.ConfigHashFromMetrics(n.LastMetrics)
	if current == "" {
		httpError(w, http.StatusConflict, "node has not reported a config fingerprint yet")
		return
	}
	if err := s.store.SetNodeConfigBaseline(r.Context(), id, current); err != nil {
		s.internalError(w, err)
		return
	}
	s.audit(r.Context(), u, "config.baseline_accept", id, "")
	writeJSON(w, http.StatusOK, map[string]any{"config_drift": false})
}

// handleDeleteNode erases the node record (server-side data deletion, PRD §4.4).
// Only nodes that already left can be deleted, to prevent skipping offboarding.
func (s *Server) handleDeleteNode(w http.ResponseWriter, r *http.Request, u *store.User) {
	id := r.PathValue("id")
	n, err := s.store.GetNode(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		httpError(w, http.StatusNotFound, "node not found")
		return
	}
	if err != nil {
		s.internalError(w, err)
		return
	}
	if n.Status != store.NodeStatusLeft {
		httpError(w, http.StatusConflict, "node is still managed; remove it from management first")
		return
	}
	if err := s.store.DeleteNode(r.Context(), id); err != nil && !errors.Is(err, store.ErrNotFound) {
		s.internalError(w, err)
		return
	}
	s.audit(r.Context(), u, "node.delete", id, n.Name)
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}
