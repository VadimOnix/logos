package api

import (
	"errors"
	"net/http"

	"github.com/VadimOnix/logos/server/internal/store"
)

// Handlers for the dedicated mTLS agent listener. The TLS layer has already
// verified the client certificate against the internal CA; the certificate's
// CommonName is the node UUID. Node status is re-checked per request, so a
// removed ("left") node is rejected even while its certificate is still
// formally valid — no CRL needed.

// mtlsNode resolves the authenticated node from the client certificate.
func (s *Server) mtlsNode(w http.ResponseWriter, r *http.Request) *store.Node {
	if r.TLS == nil || len(r.TLS.PeerCertificates) == 0 {
		httpError(w, http.StatusUnauthorized, "client certificate required")
		return nil
	}
	nodeID := r.TLS.PeerCertificates[0].Subject.CommonName
	node, err := s.store.GetNode(r.Context(), nodeID)
	if errors.Is(err, store.ErrNotFound) {
		httpError(w, http.StatusUnauthorized, "unknown node")
		return nil
	}
	if err != nil {
		s.internalError(w, err)
		return nil
	}
	if node.Status != store.NodeStatusEnrolled {
		httpError(w, http.StatusUnauthorized, "node has left management")
		return nil
	}
	return node
}

func (s *Server) handleAgentWSMTLS(w http.ResponseWriter, r *http.Request) {
	if node := s.mtlsNode(w, r); node != nil {
		s.serveAgentWS(w, r, node)
	}
}

// handleAgentRenew rotates the node's client certificate (PRD §6: per-node
// certs with rotation). Authenticated by the current — still valid — cert.
func (s *Server) handleAgentRenew(w http.ResponseWriter, r *http.Request) {
	node := s.mtlsNode(w, r)
	if node == nil {
		return
	}
	var req struct {
		CSR string `json:"csr"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	certPEM, notAfter, err := s.ca.SignAgentCSR(req.CSR, node.ID)
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid CSR: "+err.Error())
		return
	}
	if err := s.store.SetNodeCertNotAfter(r.Context(), node.ID, notAfter); err != nil {
		s.internalError(w, err)
		return
	}
	s.log.Info("agent certificate renewed", "node", node.ID, "not_after", notAfter)
	writeJSON(w, http.StatusOK, map[string]any{
		"client_cert": certPEM,
		"ca_cert":     s.ca.CertPEM(),
		"not_after":   notAfter,
	})
}
