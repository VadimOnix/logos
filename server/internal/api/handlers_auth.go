package api

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/VadimOnix/logos/server/internal/auth"
	"github.com/VadimOnix/logos/server/internal/store"
)

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if !s.loginLimiter.allow(clientIP(r)) {
		httpError(w, http.StatusTooManyRequests, "too many attempts, slow down")
		return
	}
	var req struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	req.Email = strings.ToLower(strings.TrimSpace(req.Email))

	u, err := s.store.GetUserByEmail(r.Context(), req.Email)
	if errors.Is(err, store.ErrNotFound) {
		// Burn comparable time so missing vs wrong-password is indistinguishable.
		auth.CheckPassword("$2a$10$7EqJtq98hPqEX7fNZaFWoOhi5B0G1XKgOQ5c1nQO0Yw1uPMLmZKPi", req.Password)
		httpError(w, http.StatusUnauthorized, "invalid email or password")
		return
	}
	if err != nil {
		s.internalError(w, err)
		return
	}
	if !auth.CheckPassword(u.PasswordHash, req.Password) {
		httpError(w, http.StatusUnauthorized, "invalid email or password")
		return
	}

	token := auth.NewToken()
	if err := s.store.CreateSession(r.Context(), auth.HashToken(token), u.ID, time.Now().Add(sessionTTL)); err != nil {
		s.internalError(w, err)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   r.TLS != nil,
		MaxAge:   int(sessionTTL.Seconds()),
	})
	s.audit(r.Context(), u, "auth.login", "", "")
	writeJSON(w, http.StatusOK, map[string]any{"email": u.Email})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookie); err == nil && c.Value != "" {
		if err := s.store.DeleteSession(r.Context(), auth.HashToken(c.Value)); err != nil {
			s.internalError(w, err)
			return
		}
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: "", Path: "/", MaxAge: -1})
	writeJSON(w, http.StatusOK, map[string]string{"status": "logged out"})
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request, u *store.User) {
	writeJSON(w, http.StatusOK, map[string]any{"id": u.ID, "email": u.Email})
}

// API tokens

func (s *Server) handleListTokens(w http.ResponseWriter, r *http.Request, u *store.User) {
	tokens, err := s.store.ListAPITokens(r.Context(), u.ID)
	if err != nil {
		s.internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, tokens)
}

func (s *Server) handleCreateToken(w http.ResponseWriter, r *http.Request, u *store.User) {
	var req struct {
		Name string `json:"name"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	if strings.TrimSpace(req.Name) == "" {
		httpError(w, http.StatusBadRequest, "name is required")
		return
	}
	token := auth.NewToken()
	t, err := s.store.CreateAPIToken(r.Context(), u.ID, strings.TrimSpace(req.Name), auth.HashToken(token))
	if err != nil {
		s.internalError(w, err)
		return
	}
	s.audit(r.Context(), u, "token.create", t.Name, "")
	// The raw token is returned exactly once.
	writeJSON(w, http.StatusCreated, map[string]any{"id": t.ID, "name": t.Name, "token": token})
}

func (s *Server) handleDeleteToken(w http.ResponseWriter, r *http.Request, u *store.User) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid token id")
		return
	}
	switch err := s.store.DeleteAPIToken(r.Context(), u.ID, id); {
	case errors.Is(err, store.ErrNotFound):
		httpError(w, http.StatusNotFound, "token not found")
	case err != nil:
		s.internalError(w, err)
	default:
		s.audit(r.Context(), u, "token.delete", r.PathValue("id"), "")
		writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
	}
}
