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

// splitCanary partitions the node list for a staged rollout: the first
// `canary` nodes run before anyone else. A canary of 0 (or one covering the
// whole list) means a single unstaged batch.
func splitCanary(ids []string, canary int) (first, rest []string) {
	if canary <= 0 || canary >= len(ids) {
		return ids, nil
	}
	return ids[:canary], ids[canary:]
}

// POST /api/v1/nodes/packages/bulk {action, name?, node_ids, canary?}
func (s *Server) handleBulkPackageAction(w http.ResponseWriter, r *http.Request, u *store.User) {
	var req struct {
		Action  string   `json:"action"`
		Name    string   `json:"name"`
		NodeIDs []string `json:"node_ids"`
		// Canary > 0 stages the rollout: the first N nodes run alone, and
		// any canary failure skips the remaining nodes (PRD §5.2 "staged
		// rollout").
		Canary int `json:"canary"`
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
	if req.Canary < 0 {
		httpError(w, http.StatusBadRequest, "canary must be >= 0")
		return
	}

	params := map[string]string{"name": req.Name}
	first, rest := splitCanary(req.NodeIDs, req.Canary)
	results := s.runBulk(r.Context(), first, method, params)

	aborted := false
	if rest != nil {
		if countOK(results) < len(results) {
			// A canary failed: leave the rest of the fleet untouched.
			aborted = true
			for _, id := range rest {
				results = append(results, bulkNodeResult{NodeID: id, Error: "skipped: canary stage failed"})
			}
		} else {
			results = append(results, s.runBulk(r.Context(), rest, method, params)...)
		}
	}

	okCount := countOK(results)
	detail := fmt.Sprintf("%d/%d nodes ok", okCount, len(results))
	if aborted {
		detail += " (canary failed, rollout aborted)"
	}
	s.audit(r.Context(), u, "package.bulk_"+req.Action, req.Name, detail)
	s.log.Info("bulk package action", "action", req.Action, "pkg", req.Name,
		"nodes", len(results), "ok", okCount, "aborted", aborted)
	writeJSON(w, http.StatusOK, map[string]any{
		"ok_count": okCount,
		"total":    len(results),
		"aborted":  aborted,
		"results":  results,
	})
}

// runBulk fans one batch out with bounded concurrency, preserving order.
func (s *Server) runBulk(ctx context.Context, ids []string, method string, params any) []bulkNodeResult {
	results := make([]bulkNodeResult, len(ids))
	sem := make(chan struct{}, bulkConcurrency)
	var wg sync.WaitGroup
	for i, id := range ids {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			results[i] = s.bulkCallNode(ctx, id, method, params)
		}()
	}
	wg.Wait()
	return results
}

func countOK(results []bulkNodeResult) int {
	n := 0
	for _, res := range results {
		if res.OK {
			n++
		}
	}
	return n
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
