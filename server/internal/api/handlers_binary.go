package api

import (
	"net/http"
	"os"
	"path/filepath"
	"regexp"
)

// handleAgentBinary serves cross-compiled agent binaries to the adoption
// tool (PRD F12: the tool fetches the right binary for the router's arch
// from the control plane). Unauthenticated by design — the binary is a
// public artifact; enrollment still requires a claim code.
var goarchRe = regexp.MustCompile(`^[a-z0-9]+$`)

func (s *Server) handleAgentBinary(w http.ResponseWriter, r *http.Request) {
	if s.agentBinariesDir == "" {
		httpError(w, http.StatusNotFound,
			"agent binary hosting is not configured (set LOGOS_AGENT_BINARIES_DIR)")
		return
	}
	goarch := r.PathValue("goarch")
	if !goarchRe.MatchString(goarch) {
		httpError(w, http.StatusBadRequest, "invalid architecture")
		return
	}
	path := filepath.Join(s.agentBinariesDir, "logos-agent-linux-"+goarch)
	if _, err := os.Stat(path); err != nil {
		httpError(w, http.StatusNotFound, "no agent binary for "+goarch)
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	http.ServeFile(w, r, path)
}
