package api

import (
	"context"
	"errors"
	"net/http"
	"net/netip"
	"strconv"
	"strings"

	"github.com/VadimOnix/logos/server/internal/overlay"
	"github.com/VadimOnix/logos/server/internal/store"
)

// REST API for overlay networks (F7). Mutations respond immediately and the
// config push to agents runs in the background — the panel polls the list,
// where per-member sync_error/public_key show convergence.

type overlayJSON struct {
	*store.Overlay
	Members []*store.OverlayMember `json:"members"`
}

func (s *Server) overlayWithMembers(ctx context.Context, o *store.Overlay) (*overlayJSON, error) {
	members, err := s.store.ListOverlayMembers(ctx, o.ID)
	if err != nil {
		return nil, err
	}
	return &overlayJSON{Overlay: o, Members: members}, nil
}

// GET /api/v1/overlays — every overlay with its members.
func (s *Server) handleListOverlays(w http.ResponseWriter, r *http.Request, _ *store.User) {
	overlays, err := s.store.ListOverlays(r.Context())
	if err != nil {
		s.internalError(w, err)
		return
	}
	out := []*overlayJSON{}
	for _, o := range overlays {
		oj, err := s.overlayWithMembers(r.Context(), o)
		if err != nil {
			s.internalError(w, err)
			return
		}
		out = append(out, oj)
	}
	writeJSON(w, http.StatusOK, out)
}

// POST /api/v1/overlays {name, cidr}
func (s *Server) handleCreateOverlay(w http.ResponseWriter, r *http.Request, _ *store.User) {
	var req struct {
		Name string `json:"name"`
		CIDR string `json:"cidr"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" || len(req.Name) > 64 {
		httpError(w, http.StatusBadRequest, "name is required (max 64 chars)")
		return
	}
	prefix, err := overlay.ParseCIDR(strings.TrimSpace(req.CIDR))
	if err != nil {
		httpError(w, http.StatusBadRequest, err.Error())
		return
	}
	o, err := s.store.CreateOverlay(r.Context(), req.Name, prefix.String())
	if err != nil {
		if strings.Contains(err.Error(), "unique") {
			httpError(w, http.StatusConflict, "an overlay with that name already exists")
			return
		}
		s.internalError(w, err)
		return
	}
	s.log.Info("overlay created", "name", o.Name, "cidr", o.CIDR)
	writeJSON(w, http.StatusCreated, &overlayJSON{Overlay: o, Members: []*store.OverlayMember{}})
}

func (s *Server) overlayFromPath(w http.ResponseWriter, r *http.Request) *store.Overlay {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid overlay id")
		return nil
	}
	o, err := s.store.GetOverlay(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		httpError(w, http.StatusNotFound, "overlay not found")
		return nil
	}
	if err != nil {
		s.internalError(w, err)
		return nil
	}
	return o
}

// DELETE /api/v1/overlays/{id} — tear down on online members (best-effort;
// offline ones prune at next reconnect), then drop the record.
func (s *Server) handleDeleteOverlay(w http.ResponseWriter, r *http.Request, _ *store.User) {
	o := s.overlayFromPath(w, r)
	if o == nil {
		return
	}
	members, err := s.store.ListOverlayMembers(r.Context(), o.ID)
	if err != nil {
		s.internalError(w, err)
		return
	}
	if err := s.store.DeleteOverlay(r.Context(), o.ID); err != nil {
		s.internalError(w, err)
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*overlaySyncTimeout)
		defer cancel()
		for _, m := range members {
			if s.hub.IsOnline(m.NodeID) {
				s.removeOverlayFromNode(ctx, o.ID, m.NodeID)
			}
		}
	}()
	s.log.Info("overlay deleted", "name", o.Name)
	writeJSON(w, http.StatusOK, map[string]string{"deleted": o.Name})
}

// POST /api/v1/overlays/{id}/members {node_id, subnets?}
func (s *Server) handleAddOverlayMember(w http.ResponseWriter, r *http.Request, _ *store.User) {
	o := s.overlayFromPath(w, r)
	if o == nil {
		return
	}
	var req struct {
		NodeID  string   `json:"node_id"`
		Subnets []string `json:"subnets"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	n, err := s.store.GetNode(r.Context(), strings.TrimSpace(req.NodeID))
	if errors.Is(err, store.ErrNotFound) {
		httpError(w, http.StatusNotFound, "node not found")
		return
	}
	if err != nil {
		s.internalError(w, err)
		return
	}
	if n.Status != store.NodeStatusEnrolled {
		httpError(w, http.StatusConflict, "node has left management")
		return
	}
	subnets := []string{}
	for _, raw := range req.Subnets {
		sn := strings.TrimSpace(raw)
		if sn == "" {
			continue
		}
		p, err := netip.ParsePrefix(sn)
		if err != nil || !p.Addr().Is4() || p.Masked() != p {
			httpError(w, http.StatusBadRequest, "invalid subnet "+sn+" (want e.g. 192.168.5.0/24)")
			return
		}
		subnets = append(subnets, p.String())
	}

	prefix, err := overlay.ParseCIDR(o.CIDR)
	if err != nil {
		s.internalError(w, err)
		return
	}
	used, err := s.store.UsedOverlayIPs(r.Context(), o.ID)
	if err != nil {
		s.internalError(w, err)
		return
	}
	ip, err := overlay.NextFreeIP(prefix, used)
	if err != nil {
		httpError(w, http.StatusConflict, err.Error())
		return
	}
	m, err := s.store.AddOverlayMember(r.Context(), o.ID, n.ID, ip.String(), overlay.ListenPort(o.ID), subnets)
	if err != nil {
		if strings.Contains(err.Error(), "unique") || strings.Contains(err.Error(), "duplicate") {
			httpError(w, http.StatusConflict, "node is already a member of this overlay")
			return
		}
		s.internalError(w, err)
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 4*overlaySyncTimeout)
		defer cancel()
		s.syncOverlay(ctx, o.ID)
	}()
	s.log.Info("overlay member added", "overlay", o.Name, "node", n.Name, "ip", m.OverlayIP)
	writeJSON(w, http.StatusCreated, m)
}

// DELETE /api/v1/overlays/{id}/members/{node_id}
func (s *Server) handleRemoveOverlayMember(w http.ResponseWriter, r *http.Request, _ *store.User) {
	o := s.overlayFromPath(w, r)
	if o == nil {
		return
	}
	nodeID := r.PathValue("node_id")
	if err := s.store.RemoveOverlayMember(r.Context(), o.ID, nodeID); errors.Is(err, store.ErrNotFound) {
		httpError(w, http.StatusNotFound, "not a member of this overlay")
		return
	} else if err != nil {
		s.internalError(w, err)
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 4*overlaySyncTimeout)
		defer cancel()
		if s.hub.IsOnline(nodeID) {
			s.removeOverlayFromNode(ctx, o.ID, nodeID)
		}
		s.syncOverlay(ctx, o.ID)
	}()
	s.log.Info("overlay member removed", "overlay", o.Name, "node", nodeID)
	writeJSON(w, http.StatusOK, map[string]string{"removed": nodeID})
}

// POST /api/v1/overlays/{id}/sync — manual push, synchronous; returns the
// resulting member state so the caller sees fresh keys/errors.
func (s *Server) handleSyncOverlay(w http.ResponseWriter, r *http.Request, _ *store.User) {
	o := s.overlayFromPath(w, r)
	if o == nil {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 4*overlaySyncTimeout)
	defer cancel()
	s.syncOverlay(ctx, o.ID)
	oj, err := s.overlayWithMembers(r.Context(), o)
	if err != nil {
		s.internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, oj)
}
