package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/VadimOnix/logos/server/internal/store"
)

// F4 write path: versioned config pushes with connectivity-proven
// confirmation. The server applies a change through the agent, which arms an
// auto-revert watchdog; the server confirms only over a live channel. If the
// change broke connectivity, confirmation never arrives and the agent
// restores the pre-change snapshot (PRD §6 Resilience, §9).

type uciChangeReq struct {
	Op    string `json:"op"`
	Key   string `json:"key"`
	Value string `json:"value,omitempty"`
}

const (
	defaultRevertTimeoutSec = 90
	minRevertTimeoutSec     = 15
	maxRevertTimeoutSec     = 600
	confirmGrace            = 20 * time.Second
)

// POST /api/v1/nodes/{id}/config/changes — apply UCI changes.
func (s *Server) handleApplyConfig(w http.ResponseWriter, r *http.Request, u *store.User) {
	var req struct {
		Changes          []uciChangeReq `json:"changes"`
		RevertTimeoutSec int            `json:"revert_timeout_sec"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	if len(req.Changes) == 0 {
		httpError(w, http.StatusBadRequest, "changes is required")
		return
	}
	for i, ch := range req.Changes {
		if ch.Key == "" || (ch.Op != "set" && ch.Op != "delete") {
			httpError(w, http.StatusBadRequest, fmt.Sprintf("change %d: need op (set|delete) and key", i))
			return
		}
	}
	revertSec := req.RevertTimeoutSec
	if revertSec == 0 {
		revertSec = defaultRevertTimeoutSec
	}
	if revertSec < minRevertTimeoutSec || revertSec > maxRevertTimeoutSec {
		httpError(w, http.StatusBadRequest,
			fmt.Sprintf("revert_timeout_sec must be %d–%d", minRevertTimeoutSec, maxRevertTimeoutSec))
		return
	}

	changesJSON, err := json.Marshal(req.Changes)
	if err != nil {
		s.internalError(w, err)
		return
	}
	change, err := s.store.CreateConfigChange(r.Context(), r.PathValue("id"), "apply", changesJSON, u.ID)
	if err != nil {
		s.internalError(w, err)
		return
	}

	params := map[string]any{
		"apply_id":           strconv.FormatInt(change.ID, 10),
		"changes":            req.Changes,
		"revert_timeout_sec": revertSec,
	}
	s.audit(r.Context(), u, "config.apply", r.PathValue("id"),
		fmt.Sprintf("change %d (%d ops)", change.ID, len(req.Changes)))
	s.runConfigChange(w, r, change, params, "uci.apply", revertSec)
}

// POST /api/v1/nodes/{id}/config/changes/{change_id}/rollback — restore the
// pre-change snapshots of a confirmed change, through the same watchdog flow.
func (s *Server) handleRollbackConfig(w http.ResponseWriter, r *http.Request, u *store.User) {
	nodeID := r.PathValue("id")
	prevID, err := strconv.ParseInt(r.PathValue("change_id"), 10, 64)
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid change id")
		return
	}
	prev, err := s.store.GetConfigChange(r.Context(), nodeID, prevID)
	if errors.Is(err, store.ErrNotFound) {
		httpError(w, http.StatusNotFound, "config change not found")
		return
	}
	if err != nil {
		s.internalError(w, err)
		return
	}
	if prev.Status != store.ChangeStatusConfirmed || len(prev.Snapshots) == 0 {
		httpError(w, http.StatusConflict, "only confirmed changes with stored snapshots can be rolled back")
		return
	}
	var snapshots map[string]string
	if err := json.Unmarshal(prev.Snapshots, &snapshots); err != nil {
		s.internalError(w, err)
		return
	}

	meta, _ := json.Marshal(map[string]int64{"rollback_of": prev.ID})
	change, err := s.store.CreateConfigChange(r.Context(), nodeID, "rollback", meta, u.ID)
	if err != nil {
		s.internalError(w, err)
		return
	}
	params := map[string]any{
		"apply_id":           strconv.FormatInt(change.ID, 10),
		"snapshots":          snapshots,
		"revert_timeout_sec": defaultRevertTimeoutSec,
	}
	s.audit(r.Context(), u, "config.rollback", nodeID,
		fmt.Sprintf("change %d rolls back change %d", change.ID, prev.ID))
	s.runConfigChange(w, r, change, params, "uci.restore", defaultRevertTimeoutSec)
}

// runConfigChange invokes the agent method, records the outcome, and starts
// the delayed-confirmation flow.
func (s *Server) runConfigChange(w http.ResponseWriter, r *http.Request, change *store.ConfigChange, params map[string]any, method string, revertSec int) {
	res := s.callNode(w, r, rpcQueryTimeout, method, params)
	if res == nil {
		// callNode already wrote the HTTP error; record the failure.
		if _, err := s.store.DecideConfigChange(context.WithoutCancel(r.Context()), change.ID, store.ChangeStatusFailed, "agent call failed"); err != nil {
			s.log.Error("record failed config change", "change", change.ID, "err", err)
		}
		return
	}

	var applied struct {
		Snapshots map[string]string `json:"snapshots"`
	}
	if err := json.Unmarshal(res, &applied); err == nil && len(applied.Snapshots) > 0 {
		if snaps, err := json.Marshal(applied.Snapshots); err == nil {
			if err := s.store.SetConfigChangeSnapshots(r.Context(), change.ID, snaps); err != nil {
				s.log.Error("store snapshots", "change", change.ID, "err", err)
			}
		}
	}

	go s.confirmAfterDelay(change.ID, change.NodeID, revertSec)

	s.log.Info("config change applied, awaiting confirmation",
		"change", change.ID, "node", change.NodeID, "method", method)
	writeJSON(w, http.StatusAccepted, map[string]any{
		"id":     change.ID,
		"status": store.ChangeStatusApplying,
	})
}

// confirmAfterDelay waits a moment (letting service reloads settle and the
// channel drop if the change broke networking), then confirms over the live
// channel. If confirmation cannot be delivered before the watchdog window
// closes, the change is recorded as reverted.
func (s *Server) confirmAfterDelay(changeID int64, nodeID string, revertSec int) {
	ctx := context.Background()
	applyID := strconv.FormatInt(changeID, 10)

	confirmDelay := min(10*time.Second, time.Duration(revertSec)*time.Second/3)
	deadline := time.Now().Add(time.Duration(revertSec)*time.Second + confirmGrace)
	time.Sleep(confirmDelay)

	for time.Now().Before(deadline) {
		callCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
		_, err := s.hub.Call(callCtx, nodeID, "uci.confirm", map[string]string{"apply_id": applyID})
		cancel()
		if err == nil {
			if ok, derr := s.store.DecideConfigChange(ctx, changeID, store.ChangeStatusConfirmed, ""); derr != nil {
				s.log.Error("decide config change", "change", changeID, "err", derr)
			} else if ok {
				s.log.Info("config change confirmed", "change", changeID, "node", nodeID)
			}
			return
		}
		// Offline or confirm failed — retry until the watchdog window closes;
		// a reconnect may also confirm via hello reconciliation meanwhile.
		time.Sleep(5 * time.Second)
	}

	if ok, err := s.store.DecideConfigChange(ctx, changeID, store.ChangeStatusReverted,
		"confirmation not delivered before the watchdog deadline; the agent reverted to the pre-change snapshot"); err != nil {
		s.log.Error("decide config change", "change", changeID, "err", err)
	} else if ok {
		s.log.Warn("config change reverted", "change", changeID, "node", nodeID)
	}
}

// reconcilePendingApply confirms an unconfirmed change reported by an agent
// in its hello: the reconnect itself is the connectivity proof.
func (s *Server) reconcilePendingApply(nodeID, applyID string) {
	ctx := context.Background()
	changeID, err := strconv.ParseInt(applyID, 10, 64)
	if err != nil {
		return
	}
	change, err := s.store.GetConfigChange(ctx, nodeID, changeID)
	if err != nil || change.Status != store.ChangeStatusApplying {
		return
	}
	callCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	_, err = s.hub.Call(callCtx, nodeID, "uci.confirm", map[string]string{"apply_id": applyID})
	cancel()
	if err != nil {
		s.log.Warn("hello reconciliation: confirm failed", "change", changeID, "err", err)
		return
	}
	if ok, err := s.store.DecideConfigChange(ctx, changeID, store.ChangeStatusConfirmed, ""); err == nil && ok {
		s.log.Info("config change confirmed via reconnect", "change", changeID, "node", nodeID)
	}
}

// GET /api/v1/nodes/{id}/config/changes — change history (newest first).
func (s *Server) handleListConfigChanges(w http.ResponseWriter, r *http.Request, _ *store.User) {
	changes, err := s.store.ListConfigChanges(r.Context(), r.PathValue("id"), 100)
	if err != nil {
		s.internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, changes)
}
