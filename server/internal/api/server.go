// Package api is the HTTP surface of the control plane: admin/panel API,
// the enrollment endpoint, and the persistent agent channel.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/VadimOnix/logos/server/internal/auth"
	"github.com/VadimOnix/logos/server/internal/hub"
	"github.com/VadimOnix/logos/server/internal/store"
)

const sessionCookie = "logos_session"
const sessionTTL = 7 * 24 * time.Hour

type Server struct {
	store *store.Store
	hub   *hub.Hub
	log   *slog.Logger

	enrollLimiter *rateLimiter
	loginLimiter  *rateLimiter
}

func NewServer(st *store.Store, h *hub.Hub, log *slog.Logger) *Server {
	return &Server{
		store:         st,
		hub:           h,
		log:           log,
		enrollLimiter: newRateLimiter(0.2, 10), // ~12/min sustained per IP
		loginLimiter:  newRateLimiter(0.2, 10),
	}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})

	// Auth
	mux.HandleFunc("POST /api/v1/auth/login", s.handleLogin)
	mux.HandleFunc("POST /api/v1/auth/logout", s.handleLogout)
	mux.Handle("GET /api/v1/me", s.requireUser(s.handleMe))

	// API tokens
	mux.Handle("GET /api/v1/tokens", s.requireUser(s.handleListTokens))
	mux.Handle("POST /api/v1/tokens", s.requireUser(s.handleCreateToken))
	mux.Handle("DELETE /api/v1/tokens/{id}", s.requireUser(s.handleDeleteToken))

	// Claim codes
	mux.Handle("GET /api/v1/claim-codes", s.requireUser(s.handleListClaimCodes))
	mux.Handle("POST /api/v1/claim-codes", s.requireUser(s.handleCreateClaimCode))
	mux.Handle("DELETE /api/v1/claim-codes/{id}", s.requireUser(s.handleDeleteClaimCode))

	// Nodes
	mux.Handle("GET /api/v1/nodes", s.requireUser(s.handleListNodes))
	mux.Handle("GET /api/v1/nodes/{id}", s.requireUser(s.handleGetNode))
	mux.Handle("POST /api/v1/nodes/{id}/remove", s.requireUser(s.handleRemoveNode))
	mux.Handle("DELETE /api/v1/nodes/{id}", s.requireUser(s.handleDeleteNode))
	mux.Handle("GET /api/v1/nodes/{id}/packages", s.requireUser(s.handleNodePackages))
	mux.Handle("POST /api/v1/nodes/{id}/packages", s.requireUser(s.handleNodePackageAction))
	mux.Handle("GET /api/v1/nodes/{id}/config", s.requireUser(s.handleNodeConfig))

	// Agent-facing
	mux.HandleFunc("POST /api/v1/enroll", s.handleEnroll)
	mux.HandleFunc("POST /api/v1/agent/leave", s.handleAgentLeave)
	mux.HandleFunc("GET /api/v1/agent/ws", s.handleAgentWS)

	// Built-in panel
	mux.HandleFunc("GET /", s.handlePanel)

	return s.logRequests(mux)
}

func (s *Server) logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		if r.URL.Path == "/healthz" {
			return
		}
		s.log.Debug("http", "method", r.Method, "path", r.URL.Path, "dur", time.Since(start).Round(time.Millisecond))
	})
}

// currentUser resolves the request's user from the session cookie or an
// "Authorization: Bearer" API token.
func (s *Server) currentUser(r *http.Request) (*store.User, error) {
	if c, err := r.Cookie(sessionCookie); err == nil && c.Value != "" {
		u, err := s.store.GetSessionUser(r.Context(), auth.HashToken(c.Value))
		if err == nil {
			return u, nil
		}
		if !errors.Is(err, store.ErrNotFound) {
			return nil, err
		}
	}
	if tok := bearerToken(r); tok != "" {
		u, err := s.store.GetAPITokenUser(r.Context(), auth.HashToken(tok))
		if err == nil {
			return u, nil
		}
		if !errors.Is(err, store.ErrNotFound) {
			return nil, err
		}
	}
	return nil, store.ErrNotFound
}

type userHandler func(w http.ResponseWriter, r *http.Request, u *store.User)

func (s *Server) requireUser(next userHandler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, err := s.currentUser(r)
		if errors.Is(err, store.ErrNotFound) {
			httpError(w, http.StatusUnauthorized, "authentication required")
			return
		}
		if err != nil {
			s.internalError(w, err)
			return
		}
		next(w, r, u)
	})
}

func bearerToken(r *http.Request) string {
	h := r.Header.Get("Authorization")
	const prefix = "Bearer "
	if len(h) > len(prefix) && strings.EqualFold(h[:len(prefix)], prefix) {
		return h[len(prefix):]
	}
	return ""
}

// Helpers

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func readJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		httpError(w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
		return false
	}
	return true
}

func httpError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func (s *Server) internalError(w http.ResponseWriter, err error) {
	s.log.Error("internal error", "err", err)
	httpError(w, http.StatusInternalServerError, "internal error")
}

// StartSessionJanitor purges expired sessions periodically.
func (s *Server) StartSessionJanitor(ctx context.Context) {
	go func() {
		t := time.NewTicker(time.Hour)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				if err := s.store.DeleteExpiredSessions(ctx); err != nil && ctx.Err() == nil {
					s.log.Warn("session janitor", "err", err)
				}
			}
		}
	}()
}
