package agent

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"runtime"
	"strings"
	"time"
)

// Enroll performs the claim-code enrollment flow (PRD §4.2 step 5): generate
// a keypair, exchange the code for a node identity, persist the state file.
func Enroll(ctx context.Context, statePath, serverURL, code string) error {
	serverURL = strings.TrimRight(strings.TrimSpace(serverURL), "/")
	if serverURL == "" || code == "" {
		return fmt.Errorf("both --server and --code are required")
	}
	u, err := url.Parse(serverURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return fmt.Errorf("--server must be an http(s) URL, e.g. https://logos.example.com")
	}
	if _, err := LoadState(statePath); err == nil {
		return fmt.Errorf("already enrolled (state at %s); run `logos-agent leave` first", statePath)
	}

	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return err
	}

	hostname, _ := os.Hostname()
	// The TLS key never leaves the device; only the CSR is sent.
	tlsKeyPEM, csrPEM, err := newKeyAndCSR(hostname)
	if err != nil {
		return err
	}
	reqBody, err := json.Marshal(enrollRequest{
		Code:         code,
		PublicKey:    hex.EncodeToString(pub),
		CSR:          csrPEM,
		Hostname:     hostname,
		AgentVersion: Version,
		OSVersion:    OSVersion(),
		Arch:         runtime.GOARCH,
	})
	if err != nil {
		return err
	}

	httpCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(httpCtx, http.MethodPost, serverURL+"/api/v1/enroll", bytes.NewReader(reqBody))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("contact control plane: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		return fmt.Errorf("enrollment rejected: %s", apiError(resp))
	}
	var er enrollResponse
	if err := json.NewDecoder(resp.Body).Decode(&er); err != nil {
		return fmt.Errorf("parse enrollment response: %w", err)
	}
	if er.NodeID == "" || er.NodeToken == "" {
		return fmt.Errorf("control plane returned an incomplete enrollment response")
	}

	st := &State{
		ServerURL:  serverURL,
		NodeID:     er.NodeID,
		NodeToken:  er.NodeToken,
		PrivateKey: hex.EncodeToString(priv.Seed()),
	}
	if er.ClientCert != "" && er.CACert != "" && er.AgentEndpoint != "" {
		st.ClientCert = er.ClientCert
		st.ClientKey = tlsKeyPEM
		st.CACert = er.CACert
		st.AgentEndpoint = er.AgentEndpoint
	}
	if err := SaveState(statePath, st); err != nil {
		return fmt.Errorf("save state: %w", err)
	}
	channel := "token (legacy)"
	if st.HasMTLS() {
		channel = "mTLS via " + st.AgentEndpoint
	}
	fmt.Printf("enrolled as %q (node %s)\nchannel: %s\nstate: %s\n", er.NodeName, er.NodeID, channel, statePath)
	return nil
}

func apiError(resp *http.Response) string {
	var body struct {
		Error string `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err == nil && body.Error != "" {
		return fmt.Sprintf("%s (%s)", body.Error, resp.Status)
	}
	return resp.Status
}
