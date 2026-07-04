package api

import (
	"net/http"

	"github.com/VadimOnix/logos/server/internal/store"
)

// statsView is the fleet-wide summary served by GET /api/v1/stats — the
// numbers the panel dashboard and external monitoring poll for, aggregated
// server-side so callers don't have to page through /api/v1/nodes.
type statsView struct {
	Nodes struct {
		Total   int `json:"total"` // online + offline + left
		Online  int `json:"online"`
		Offline int `json:"offline"`
		Left    int `json:"left"`
	} `json:"nodes"`
	Overlays int `json:"overlays"`
	// Alerts counts nodes with an outstanding (raised, not yet cleared)
	// alert of each kind (F11).
	Alerts struct {
		Offline  int `json:"offline"`
		DiskFull int `json:"disk_full"`
	} `json:"alerts"`
}

// fleetStats aggregates the summary from node rows, hub liveness, and the
// overlay count. Liveness comes from isOnline — same source of truth as the
// per-node status in nodeView.
func fleetStats(nodes []*store.Node, isOnline func(nodeID string) bool, overlayCount int) statsView {
	var v statsView
	v.Overlays = overlayCount
	v.Nodes.Total = len(nodes)
	for _, n := range nodes {
		if n.Status == store.NodeStatusLeft {
			// The alert watcher skips left nodes, so any alert marks on
			// them are frozen leftovers — don't count them as outstanding.
			v.Nodes.Left++
			continue
		}
		if isOnline(n.ID) {
			v.Nodes.Online++
		} else {
			v.Nodes.Offline++
		}
		if n.AlertedOfflineAt != nil {
			v.Alerts.Offline++
		}
		if n.AlertedDiskFullAt != nil {
			v.Alerts.DiskFull++
		}
	}
	return v
}

func (s *Server) handleStats(w http.ResponseWriter, r *http.Request, _ *store.User) {
	nodes, err := s.store.ListNodes(r.Context())
	if err != nil {
		s.internalError(w, err)
		return
	}
	overlays, err := s.store.ListOverlays(r.Context())
	if err != nil {
		s.internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, fleetStats(nodes, s.hub.IsOnline, len(overlays)))
}
