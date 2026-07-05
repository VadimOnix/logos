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
func (s *Server) handleNodePackageAction(w http.ResponseWriter, r *http.Request, u *store.User) {
	var req struct {
		Action string `json:"action"`
		Name   string `json:"name"`
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
	params := map[string]string{"name": req.Name}
	if res := s.callNode(w, r, rpcMutateTimeout, method, params); res != nil {
		s.audit(r.Context(), u, "package."+req.Action, r.PathValue("id"), strings.TrimSpace(req.Name))
		s.log.Info("package action", "node", r.PathValue("id"), "action", req.Action, "pkg", req.Name)
		writeRawJSON(w, res)
	}
}

// POST /api/v1/nodes/{id}/firmware {url, sha256, keep_config?} — sysupgrade
// orchestration (v1.0): the agent downloads and hash-verifies the image,
// acknowledges, then flashes. The node drops offline during the flash and
// reconnects with its existing identity once the new firmware boots.
func (s *Server) handleNodeFirmware(w http.ResponseWriter, r *http.Request, u *store.User) {
	var req struct {
		URL        string `json:"url"`
		SHA256     string `json:"sha256"`
		KeepConfig *bool  `json:"keep_config,omitempty"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	req.URL = strings.TrimSpace(req.URL)
	req.SHA256 = strings.ToLower(strings.TrimSpace(req.SHA256))
	if !strings.HasPrefix(req.URL, "http://") && !strings.HasPrefix(req.URL, "https://") {
		httpError(w, http.StatusBadRequest, "url must be http(s)")
		return
	}
	if len(req.SHA256) != 64 {
		httpError(w, http.StatusBadRequest, "sha256 (64 hex chars) is required — the agent refuses to flash unverified images")
		return
	}
	params := map[string]any{"url": req.URL, "sha256": req.SHA256}
	if req.KeepConfig != nil {
		params["keep_config"] = *req.KeepConfig
	}
	// The download can take minutes on a slow uplink; the agent replies
	// after verification, just before flashing.
	if res := s.callNode(w, r, rpcMutateTimeout, "firmware.upgrade", params); res != nil {
		s.audit(r.Context(), u, "firmware.upgrade", r.PathValue("id"), "sha256 "+req.SHA256[:12])
		s.log.Warn("firmware upgrade started", "node", r.PathValue("id"), "sha256", req.SHA256)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		w.Write(res)
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
