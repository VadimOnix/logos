package api

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"

	"github.com/VadimOnix/logos/server/internal/store"
)

// Bulk package operations (v1.0 "group/bulk operations"): one call fans a
// package action out to many nodes over their live channels. Fan-out is
// bounded so a large fleet doesn't stampede the hub, and every node gets an
// individual verdict — partial success is the expected outcome, not an error.

const bulkConcurrency = 8

// bulkLimit caps nodes per request; larger fleets should batch client-side.
const bulkLimit = 100

type bulkNodeResult struct {
	NodeID string `json:"node_id"`
	OK     bool   `json:"ok"`
	Error  string `json:"error,omitempty"`
}

// packageMethod maps the API action to the agent RPC method; name is
// required for everything except a fleet-wide update.
func packageMethod(action, name string) (string, error) {
	var method string
	switch action {
	case "install":
		method = "packages.install"
	case "remove":
		method = "packages.remove"
	case "update":
		method = "packages.update"
	default:
		return "", fmt.Errorf(`action must be "install", "remove", or "update"`)
	}
	if method != "packages.update" && name == "" {
		return "", fmt.Errorf("name is required for %s", action)
	}
	return method, nil
}

// POST /api/v1/nodes/packages/bulk {action, name?, node_ids}
func (s *Server) handleBulkPackageAction(w http.ResponseWriter, r *http.Request, u *store.User) {
	var req struct {
		Action  string   `json:"action"`
		Name    string   `json:"name"`
		NodeIDs []string `json:"node_ids"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	method, err := packageMethod(req.Action, req.Name)
	if err != nil {
		httpError(w, http.StatusBadRequest, err.Error())
		return
	}
	if len(req.NodeIDs) == 0 {
		httpError(w, http.StatusBadRequest, "node_ids is required")
		return
	}
	if len(req.NodeIDs) > bulkLimit {
		httpError(w, http.StatusBadRequest, fmt.Sprintf("at most %d nodes per request", bulkLimit))
		return
	}

	params := map[string]string{"name": req.Name}
	results := make([]bulkNodeResult, len(req.NodeIDs))
	sem := make(chan struct{}, bulkConcurrency)
	var wg sync.WaitGroup
	for i, id := range req.NodeIDs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			results[i] = s.bulkCallNode(r.Context(), id, method, params)
		}()
	}
	wg.Wait()

	okCount := 0
	for _, res := range results {
		if res.OK {
			okCount++
		}
	}
	s.audit(r.Context(), u, "package.bulk_"+req.Action, req.Name,
		fmt.Sprintf("%d/%d nodes ok", okCount, len(results)))
	s.log.Info("bulk package action", "action", req.Action, "pkg", req.Name,
		"nodes", len(results), "ok", okCount)
	writeJSON(w, http.StatusOK, map[string]any{
		"ok_count": okCount,
		"total":    len(results),
		"results":  results,
	})
}

// bulkCallNode runs one node's share of a bulk action, mapping every failure
// mode to a per-node verdict instead of an HTTP error.
func (s *Server) bulkCallNode(ctx context.Context, nodeID, method string, params any) bulkNodeResult {
	res := bulkNodeResult{NodeID: nodeID}
	n, err := s.store.GetNode(ctx, nodeID)
	if err != nil {
		res.Error = "node not found"
		return res
	}
	if n.Status != store.NodeStatusEnrolled {
		res.Error = "node has left management"
		return res
	}
	callCtx, cancel := context.WithTimeout(ctx, rpcMutateTimeout)
	defer cancel()
	if _, err := s.hub.Call(callCtx, n.ID, method, params); err != nil {
		res.Error = err.Error()
		return res
	}
	res.OK = true
	return res
}
