package api

import (
	"context"
	"net/http"
	"strconv"

	"github.com/VadimOnix/logos/server/internal/store"
)

// audit records an admin action in the audit log (v1.0 basic audit: who did
// what to which object). A failed write is logged and never blocks the
// action itself — this is an operational trail, not a forensic gate.
// Detail must never carry secrets (tokens, codes, passwords).
func (s *Server) audit(ctx context.Context, u *store.User, action, target, detail string) {
	if err := s.store.InsertAudit(ctx, u.Email, action, target, detail); err != nil && ctx.Err() == nil {
		s.log.Warn("audit write failed", "action", action, "err", err)
	}
}

// auditLimit parses the ?limit= query value, clamped to [1,500]; bad or
// missing input falls back to 100.
func auditLimit(q string) int {
	n, err := strconv.Atoi(q)
	if err != nil || n < 1 {
		return 100
	}
	if n > 500 {
		return 500
	}
	return n
}

// GET /api/v1/audit — most recent admin actions, newest first.
func (s *Server) handleListAudit(w http.ResponseWriter, r *http.Request, _ *store.User) {
	entries, err := s.store.ListAudit(r.Context(), auditLimit(r.URL.Query().Get("limit")))
	if err != nil {
		s.internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, entries)
}
