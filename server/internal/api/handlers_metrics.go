package api

import (
	"errors"
	"net/http"
	"time"

	"github.com/VadimOnix/logos/server/internal/store"
)

// GET /api/v1/nodes/{id}/metrics/history[?since=6h] — the node's retained
// metric samples for charting (F6). `since` is a Go duration, clamped to the
// retention window.
func (s *Server) handleMetricHistory(w http.ResponseWriter, r *http.Request, _ *store.User) {
	id := r.PathValue("id")
	if _, err := s.store.GetNode(r.Context(), id); errors.Is(err, store.ErrNotFound) {
		httpError(w, http.StatusNotFound, "node not found")
		return
	} else if err != nil {
		s.internalError(w, err)
		return
	}

	window := 6 * time.Hour
	if q := r.URL.Query().Get("since"); q != "" {
		if d, err := time.ParseDuration(q); err == nil && d > 0 {
			window = d
		}
	}
	if window > MetricRetention {
		window = MetricRetention
	}

	samples, err := s.store.MetricHistory(r.Context(), id, time.Now().Add(-window))
	if err != nil {
		s.internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, samples)
}
