package agent

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

// mTLS channel material (PRD §6: per-node certs with rotation). The agent
// generates its key locally — only the CSR leaves the device — and pins the
// control plane's internal CA for the agent endpoint.

// renewBefore mirrors the server's rotation policy.
const renewBefore = 30 * 24 * time.Hour

// newKeyAndCSR generates an ECDSA P-256 key and a CSR for it. The server
// ignores the CSR subject and binds the cert to the node UUID itself.
func newKeyAndCSR(hostname string) (keyPEM, csrPEM string, err error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return "", "", err
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return "", "", err
	}
	csrDER, err := x509.CreateCertificateRequest(rand.Reader,
		&x509.CertificateRequest{Subject: pkix.Name{CommonName: hostname}}, key)
	if err != nil {
		return "", "", err
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})),
		string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: csrDER})), nil
}

// mtlsConfig builds the TLS client config: our client cert plus the pinned CA.
func mtlsConfig(st *State) (*tls.Config, error) {
	cert, err := tls.X509KeyPair([]byte(st.ClientCert), []byte(st.ClientKey))
	if err != nil {
		return nil, fmt.Errorf("load client cert: %w", err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM([]byte(st.CACert)) {
		return nil, errors.New("invalid pinned CA cert")
	}
	return &tls.Config{
		MinVersion:   tls.VersionTLS12,
		Certificates: []tls.Certificate{cert},
		RootCAs:      pool,
	}, nil
}

// certNotAfter parses the expiry of the stored client certificate.
func certNotAfter(certPEM string) (time.Time, error) {
	block, _ := pem.Decode([]byte(certPEM))
	if block == nil {
		return time.Time{}, errors.New("invalid client cert PEM")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return time.Time{}, err
	}
	return cert.NotAfter, nil
}

// maybeRenewCert rotates the client certificate when it is inside the
// renewal window. Called on each successful connect; failures are logged and
// retried on the next reconnect (the current cert stays valid meanwhile).
func maybeRenewCert(ctx context.Context, statePath string, st *State, log *slog.Logger) {
	notAfter, err := certNotAfter(st.ClientCert)
	if err != nil {
		log.Warn("cannot parse own client cert", "err", err)
		return
	}
	if time.Until(notAfter) > renewBefore {
		return
	}
	log.Info("client certificate inside renewal window, rotating", "not_after", notAfter)

	hostname := strings.SplitN(st.NodeID, "-", 2)[0]
	keyPEM, csrPEM, err := newKeyAndCSR(hostname)
	if err != nil {
		log.Warn("generate renewal key", "err", err)
		return
	}
	tlsCfg, err := mtlsConfig(st)
	if err != nil {
		log.Warn("renewal tls config", "err", err)
		return
	}
	renewURL := strings.Replace(st.AgentEndpoint, "wss://", "https://", 1) + "/agent/renew"
	body, _ := json.Marshal(map[string]string{"csr": csrPEM})
	reqCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, renewURL, bytes.NewReader(body))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Transport: &http.Transport{TLSClientConfig: tlsCfg}}
	resp, err := client.Do(req)
	if err != nil {
		log.Warn("certificate renewal failed", "err", err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		log.Warn("certificate renewal rejected", "status", resp.Status)
		return
	}
	var out struct {
		ClientCert string `json:"client_cert"`
		CACert     string `json:"ca_cert"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil || out.ClientCert == "" {
		log.Warn("parse renewal response", "err", err)
		return
	}
	st.ClientCert = out.ClientCert
	st.ClientKey = keyPEM
	if out.CACert != "" {
		st.CACert = out.CACert
	}
	if err := SaveState(statePath, st); err != nil {
		log.Error("persist renewed certificate", "err", err)
		return
	}
	log.Info("client certificate renewed")
}
