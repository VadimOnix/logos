package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/VadimOnix/logos/server/internal/hub"
	"github.com/VadimOnix/logos/server/internal/store"
)

// REST endpoints that proxy to live-agent RPCs (F4/F5). They require the
// node's management channel to be up — there is no queueing in M1.

const (
	rpcQueryTimeout  = 60 * time.Second  // list/export
	rpcMutateTimeout = 300 * time.Second // opkg/apk install can be slow on small flash
)

// callNode resolves the node, invokes the RPC, and maps transport errors to
// HTTP statuses. Returns nil on error (response already written).
func (s *Server) callNode(w http.ResponseWriter, r *http.Request, timeout time.Duration, method string, params any) json.RawMessage {
	id := r.PathValue("id")
	n, err := s.store.GetNode(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		httpError(w, http.StatusNotFound, "node not found")
		return nil
	}
	if err != nil {
		s.internalError(w, err)
		return nil
	}
	if n.Status != store.NodeStatusEnrolled {
		httpError(w, http.StatusConflict, "node has left management")
		return nil
	}

	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()
	res, err := s.hub.Call(ctx, n.ID, method, params)
	switch {
	case errors.Is(err, hub.ErrOffline):
		httpError(w, http.StatusServiceUnavailable, "node is offline")
		return nil
	case errors.Is(err, context.DeadlineExceeded):
		httpError(w, http.StatusGatewayTimeout, "the agent did not respond in time")
		return nil
	case err != nil:
		httpError(w, http.StatusBadGateway, err.Error())
		return nil
	}
	return res
}

func writeRawJSON(w http.ResponseWriter, raw json.RawMessage) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write(raw)
}

// GET /api/v1/nodes/{id}/packages — live installed-package list (F5).
func (s *Server) handleNodePackages(w http.ResponseWriter, r *http.Request, _ *store.User) {
	if res := s.callNode(w, r, rpcQueryTimeout, "packages.list", nil); res != nil {
		writeRawJSON(w, res)
	}
}

// POST /api/v1/nodes/{id}/packages — install/remove/update (F5).
func (s *Server) handleNodePackageAction(w http.ResponseWriter, r *http.Request, _ *store.User) {
	var req struct {
		Action string `json:"action"`
		Name   string `json:"name"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	var method string
	switch req.Action {
	case "install":
		method = "packages.install"
	case "remove":
		method = "packages.remove"
	case "update":
		method = "packages.update"
	default:
		httpError(w, http.StatusBadRequest, `action must be "install", "remove", or "update"`)
		return
	}
	if method != "packages.update" && strings.TrimSpace(req.Name) == "" {
		httpError(w, http.StatusBadRequest, "name is required")
		return
	}
	params := map[string]string{"name": strings.TrimSpace(req.Name)}
	if res := s.callNode(w, r, rpcMutateTimeout, method, params); res != nil {
		s.log.Info("package action", "node", r.PathValue("id"), "action", req.Action, "pkg", req.Name)
		writeRawJSON(w, res)
	}
}

// GET /api/v1/nodes/{id}/config[?config=network] — live `uci export`
// snapshot (F4 step 1: read-only; push/versioning comes next).
func (s *Server) handleNodeConfig(w http.ResponseWriter, r *http.Request, _ *store.User) {
	params := map[string]string{}
	if c := r.URL.Query().Get("config"); c != "" {
		params["config"] = c
	}
	if res := s.callNode(w, r, rpcQueryTimeout, "uci.export", params); res != nil {
		writeRawJSON(w, res)
	}
}
