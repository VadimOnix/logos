package api

import (
	"encoding/json"
	"errors"
	"net/http"
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
	return v
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
func (s *Server) handleRemoveNode(w http.ResponseWriter, r *http.Request, _ *store.User) {
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
	s.log.Info("node removed from management", "node", id)
	writeJSON(w, http.StatusOK, map[string]string{"status": "left"})
}

// handleDeleteNode erases the node record (server-side data deletion, PRD §4.4).
// Only nodes that already left can be deleted, to prevent skipping offboarding.
func (s *Server) handleDeleteNode(w http.ResponseWriter, r *http.Request, _ *store.User) {
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
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}
