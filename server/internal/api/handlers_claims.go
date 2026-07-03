package api

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/VadimOnix/logos/server/internal/auth"
	"github.com/VadimOnix/logos/server/internal/store"
)

const (
	defaultClaimTTL = time.Hour
	maxClaimTTL     = 7 * 24 * time.Hour
)

func (s *Server) handleCreateClaimCode(w http.ResponseWriter, r *http.Request, u *store.User) {
	var req struct {
		Note       string `json:"note"`
		TTLMinutes int    `json:"ttl_minutes"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	ttl := defaultClaimTTL
	if req.TTLMinutes > 0 {
		ttl = time.Duration(req.TTLMinutes) * time.Minute
		if ttl > maxClaimTTL {
			httpError(w, http.StatusBadRequest, "ttl_minutes exceeds the 7-day maximum")
			return
		}
	}

	code := auth.NewClaimCode()
	c, err := s.store.CreateClaimCode(r.Context(), auth.HashToken(code), req.Note, u.ID, time.Now().Add(ttl))
	if err != nil {
		s.internalError(w, err)
		return
	}
	// The plaintext code is returned exactly once; only its hash is stored.
	writeJSON(w, http.StatusCreated, map[string]any{
		"id":         c.ID,
		"code":       code,
		"note":       c.Note,
		"expires_at": c.ExpiresAt,
	})
}

func (s *Server) handleListClaimCodes(w http.ResponseWriter, r *http.Request, _ *store.User) {
	codes, err := s.store.ListClaimCodes(r.Context())
	if err != nil {
		s.internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, codes)
}

func (s *Server) handleDeleteClaimCode(w http.ResponseWriter, r *http.Request, _ *store.User) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid claim code id")
		return
	}
	switch err := s.store.DeleteClaimCode(r.Context(), id); {
	case errors.Is(err, store.ErrNotFound):
		httpError(w, http.StatusNotFound, "claim code not found or already used")
	case err != nil:
		s.internalError(w, err)
	default:
		writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
	}
}
