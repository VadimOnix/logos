package api

import (
	"context"
	"encoding/json"
	"time"

	"github.com/VadimOnix/logos/server/internal/overlay"
	"github.com/VadimOnix/logos/server/internal/store"
)

// Overlay sync engine (F7). The desired state lives in Postgres; this file
// pushes it to agents over the live channel and records what came back.
// Everything here is idempotent and converges: a failed or missed sync is
// retried on the next membership change, manual sync, or agent reconnect.

const overlaySyncTimeout = 30 * time.Second

// syncOverlay pushes the current member set to every online member. When a
// first sync reveals a member's public key, another pass distributes it to
// the peers, so a freshly added node becomes reachable in one call.
func (s *Server) syncOverlay(ctx context.Context, overlayID int64) {
	for pass := 0; pass < 3; pass++ {
		ov, err := s.store.GetOverlay(ctx, overlayID)
		if err != nil {
			return // deleted meanwhile (or ctx over) — nothing to sync
		}
		members, err := s.store.ListOverlayMembers(ctx, overlayID)
		if err != nil {
			s.log.Error("overlay sync: list members", "overlay", overlayID, "err", err)
			return
		}
		keysChanged := false
		for _, m := range members {
			if m.NodeStatus != store.NodeStatusEnrolled || !s.hub.IsOnline(m.NodeID) {
				continue
			}
			changed, err := s.syncOverlayMember(ctx, ov, members, m)
			if err != nil {
				s.log.Warn("overlay sync", "overlay", ov.Name, "node", m.NodeName, "err", err)
				s.store.SetOverlayMemberSyncError(ctx, overlayID, m.NodeID, err.Error())
				continue
			}
			s.store.SetOverlayMemberSyncError(ctx, overlayID, m.NodeID, "")
			keysChanged = keysChanged || changed
		}
		if !keysChanged {
			return
		}
	}
}

// syncOverlayMember pushes one member's desired interface state and stores
// the public key the agent reported. Returns whether the key changed.
func (s *Server) syncOverlayMember(ctx context.Context, ov *store.Overlay, members []*store.OverlayMember, m *store.OverlayMember) (bool, error) {
	callCtx, cancel := context.WithTimeout(ctx, overlaySyncTimeout)
	defer cancel()
	res, err := s.hub.Call(callCtx, m.NodeID, "overlay.sync", overlay.BuildSync(ov, members, m))
	if err != nil {
		return false, err
	}
	var out struct {
		PublicKey string `json:"public_key"`
	}
	if err := json.Unmarshal(res, &out); err != nil {
		return false, err
	}
	if out.PublicKey == "" || out.PublicKey == m.PublicKey {
		return false, nil
	}
	if err := s.store.SetOverlayMemberKey(ctx, ov.ID, m.NodeID, out.PublicKey); err != nil {
		return false, err
	}
	return true, nil
}

// reconcileNodeOverlays runs when an agent (re)connects: push the node's
// full desired overlay set in one RPC so the agent can also prune interfaces
// for overlays it was removed from while offline. Key changes then propagate
// to the peers via per-overlay syncs.
func (s *Server) reconcileNodeOverlays(nodeID string) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*overlaySyncTimeout)
	defer cancel()

	ids, err := s.store.ListNodeOverlayIDs(ctx, nodeID)
	if err != nil {
		s.log.Error("overlay reconcile: list overlays", "node", nodeID, "err", err)
		return
	}
	specs := []overlay.SyncParams{}
	byIface := map[string]*store.OverlayMember{}
	for _, id := range ids {
		ov, err := s.store.GetOverlay(ctx, id)
		if err != nil {
			continue
		}
		members, err := s.store.ListOverlayMembers(ctx, id)
		if err != nil {
			continue
		}
		for _, m := range members {
			if m.NodeID == nodeID {
				specs = append(specs, overlay.BuildSync(ov, members, m))
				byIface[overlay.IfaceName(id)] = m
			}
		}
	}
	// Nodes in no overlay still get the (cheap) call: it prunes leftover
	// logosN interfaces after the node was removed from its last overlay.
	res, err := s.hub.Call(ctx, nodeID, "overlay.reconcile", map[string]any{"overlays": specs})
	if err != nil {
		s.log.Warn("overlay reconcile", "node", nodeID, "err", err)
		for _, m := range byIface {
			s.store.SetOverlayMemberSyncError(ctx, m.OverlayID, nodeID, err.Error())
		}
		return
	}
	var out struct {
		PublicKeys map[string]string `json:"public_keys"`
		Removed    []string          `json:"removed"`
	}
	if err := json.Unmarshal(res, &out); err != nil {
		s.log.Warn("overlay reconcile: bad result", "node", nodeID, "err", err)
		return
	}
	for iface, m := range byIface {
		s.store.SetOverlayMemberSyncError(ctx, m.OverlayID, nodeID, "")
		pk := out.PublicKeys[iface]
		if pk != "" && pk != m.PublicKey {
			if err := s.store.SetOverlayMemberKey(ctx, m.OverlayID, nodeID, pk); err == nil {
				// Let the peers learn the (new) key.
				s.syncOverlay(ctx, m.OverlayID)
			}
		}
	}
	if len(out.Removed) > 0 {
		s.log.Info("overlay reconcile pruned interfaces", "node", nodeID, "removed", out.Removed)
	}
}

// removeOverlayFromNode asks a node to tear its interface down; best-effort
// (offline nodes prune on their next reconnect via reconcile).
func (s *Server) removeOverlayFromNode(ctx context.Context, overlayID int64, nodeID string) {
	callCtx, cancel := context.WithTimeout(ctx, overlaySyncTimeout)
	defer cancel()
	if _, err := s.hub.Call(callCtx, nodeID, "overlay.remove",
		map[string]string{"iface": overlay.IfaceName(overlayID)}); err != nil {
		s.log.Warn("overlay remove", "overlay", overlayID, "node", nodeID, "err", err)
	}
}
