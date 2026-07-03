package api

import (
	_ "embed"
	"net/http"
)

//go:embed panel.html
var panelHTML []byte

// handlePanel serves the built-in single-page admin panel. A proper SPA panel
// is planned; this keeps F3 usable from a browser without extra deployment.
func (s *Server) handlePanel(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(panelHTML)
}
