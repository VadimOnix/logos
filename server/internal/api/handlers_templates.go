package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"github.com/VadimOnix/logos/server/internal/store"
)

// Config templates (v1.0, PRD §5.2): a named list of UCI operations with
// ${var} placeholders. At apply time the template is rendered per node —
// user-supplied variables plus the builtins ${node.name} and ${node.id} —
// and pushed through the exact same versioned auto-revert machinery as a
// hand-written change, so a template that breaks connectivity reverts
// itself node by node.

var templateVarRe = regexp.MustCompile(`\$\{([a-zA-Z0-9_.-]+)\}`)

// substVars replaces every ${var} in s. Unknown variables are an error, not
// a silent passthrough — half-rendered UCI keys must never reach a device.
func substVars(s string, vars map[string]string) (string, error) {
	var missing []string
	out := templateVarRe.ReplaceAllStringFunc(s, func(m string) string {
		name := m[2 : len(m)-1]
		v, ok := vars[name]
		if !ok {
			missing = append(missing, name)
			return m
		}
		return v
	})
	if len(missing) > 0 {
		return "", fmt.Errorf("undefined variable ${%s}", missing[0])
	}
	return out, nil
}

// renderTemplate produces the concrete change list for one node.
func renderTemplate(body []uciChangeReq, vars map[string]string) ([]uciChangeReq, error) {
	out := make([]uciChangeReq, len(body))
	for i, ch := range body {
		key, err := substVars(ch.Key, vars)
		if err != nil {
			return nil, fmt.Errorf("change %d key: %w", i, err)
		}
		value, err := substVars(ch.Value, vars)
		if err != nil {
			return nil, fmt.Errorf("change %d value: %w", i, err)
		}
		out[i] = uciChangeReq{Op: ch.Op, Key: key, Value: value}
	}
	return out, nil
}

// validateTemplateBody checks the ops without rendering: placeholders stay.
func validateTemplateBody(body []uciChangeReq) error {
	if len(body) == 0 {
		return fmt.Errorf("changes is required")
	}
	for i, ch := range body {
		if ch.Key == "" || (ch.Op != "set" && ch.Op != "delete") {
			return fmt.Errorf("change %d: need op (set|delete) and key", i)
		}
	}
	return nil
}

// GET /api/v1/config-templates
func (s *Server) handleListTemplates(w http.ResponseWriter, r *http.Request, _ *store.User) {
	ts, err := s.store.ListConfigTemplates(r.Context())
	if err != nil {
		s.internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, ts)
}

// POST /api/v1/config-templates {name, changes: [{op,key,value}]}
func (s *Server) handleCreateTemplate(w http.ResponseWriter, r *http.Request, u *store.User) {
	var req struct {
		Name    string         `json:"name"`
		Changes []uciChangeReq `json:"changes"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" || len(req.Name) > 64 {
		httpError(w, http.StatusBadRequest, "name is required (max 64 chars)")
		return
	}
	if err := validateTemplateBody(req.Changes); err != nil {
		httpError(w, http.StatusBadRequest, err.Error())
		return
	}
	body, err := json.Marshal(req.Changes)
	if err != nil {
		s.internalError(w, err)
		return
	}
	t, err := s.store.CreateConfigTemplate(r.Context(), req.Name, body)
	if err != nil {
		if strings.Contains(err.Error(), "unique") {
			httpError(w, http.StatusConflict, "a template with that name already exists")
			return
		}
		s.internalError(w, err)
		return
	}
	s.audit(r.Context(), u, "template.create", t.Name, fmt.Sprintf("%d changes", len(req.Changes)))
	writeJSON(w, http.StatusCreated, t)
}

// DELETE /api/v1/config-templates/{id}
func (s *Server) handleDeleteTemplate(w http.ResponseWriter, r *http.Request, u *store.User) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid template id")
		return
	}
	switch err := s.store.DeleteConfigTemplate(r.Context(), id); {
	case errors.Is(err, store.ErrNotFound):
		httpError(w, http.StatusNotFound, "template not found")
	case err != nil:
		s.internalError(w, err)
	default:
		s.audit(r.Context(), u, "template.delete", r.PathValue("id"), "")
		writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
	}
}

type templateApplyResult struct {
	NodeID   string `json:"node_id"`
	OK       bool   `json:"ok"`
	ChangeID int64  `json:"change_id,omitempty"`
	Error    string `json:"error,omitempty"`
}

// POST /api/v1/config-templates/{id}/apply {node_ids, vars?, revert_timeout_sec?}
func (s *Server) handleApplyTemplate(w http.ResponseWriter, r *http.Request, u *store.User) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid template id")
		return
	}
	t, err := s.store.GetConfigTemplate(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		httpError(w, http.StatusNotFound, "template not found")
		return
	}
	if err != nil {
		s.internalError(w, err)
		return
	}

	var req struct {
		NodeIDs          []string          `json:"node_ids"`
		Vars             map[string]string `json:"vars"`
		RevertTimeoutSec int               `json:"revert_timeout_sec"`
	}
	if !readJSON(w, r, &req) {
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
	revertSec := req.RevertTimeoutSec
	if revertSec == 0 {
		revertSec = defaultRevertTimeoutSec
	}
	if revertSec < minRevertTimeoutSec || revertSec > maxRevertTimeoutSec {
		httpError(w, http.StatusBadRequest,
			fmt.Sprintf("revert_timeout_sec must be %d–%d", minRevertTimeoutSec, maxRevertTimeoutSec))
		return
	}
	var body []uciChangeReq
	if err := json.Unmarshal(t.Body, &body); err != nil {
		s.internalError(w, err)
		return
	}

	results := make([]templateApplyResult, len(req.NodeIDs))
	sem := make(chan struct{}, bulkConcurrency)
	var wg sync.WaitGroup
	for i, nodeID := range req.NodeIDs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			res := templateApplyResult{NodeID: nodeID}
			defer func() { results[i] = res }()

			n, err := s.store.GetNode(r.Context(), nodeID)
			if err != nil {
				res.Error = "node not found"
				return
			}
			vars := map[string]string{"node.name": n.Name, "node.id": n.ID}
			for k, v := range req.Vars {
				vars[k] = v
			}
			changes, err := renderTemplate(body, vars)
			if err != nil {
				res.Error = err.Error()
				return
			}
			change, err := s.applyChangeToNode(r.Context(), nodeID, changes, revertSec, u.ID)
			if err != nil {
				res.Error = err.Error()
				return
			}
			res.OK = true
			res.ChangeID = change.ID
		}()
	}
	wg.Wait()

	okCount := 0
	for _, res := range results {
		if res.OK {
			okCount++
		}
	}
	s.audit(r.Context(), u, "template.apply", t.Name, fmt.Sprintf("%d/%d nodes ok", okCount, len(results)))
	s.log.Info("template applied", "template", t.Name, "nodes", len(results), "ok", okCount)
	writeJSON(w, http.StatusAccepted, map[string]any{
		"ok_count": okCount,
		"total":    len(results),
		"results":  results,
	})
}
