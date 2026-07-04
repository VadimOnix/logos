package agent

import (
	"crypto/x509"
	"encoding/pem"
	"strings"
	"testing"
)

func TestNewKeyAndCSR(t *testing.T) {
	keyPEM, csrPEM, err := newKeyAndCSR("router-1")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(keyPEM, "EC PRIVATE KEY") {
		t.Errorf("key PEM type: %q", keyPEM[:40])
	}
	block, _ := pem.Decode([]byte(csrPEM))
	if block == nil || block.Type != "CERTIFICATE REQUEST" {
		t.Fatal("bad CSR PEM")
	}
	csr, err := x509.ParseCertificateRequest(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	if err := csr.CheckSignature(); err != nil {
		t.Errorf("CSR self-signature: %v", err)
	}
}

func TestHasMTLS(t *testing.T) {
	st := &State{ServerURL: "http://x", NodeID: "n", NodeToken: "t"}
	if st.HasMTLS() {
		t.Error("token-only state reported as mTLS")
	}
	st.AgentEndpoint = "wss://x:8443"
	st.ClientCert = "c"
	st.ClientKey = "k"
	st.CACert = "ca"
	if !st.HasMTLS() {
		t.Error("complete mTLS state not detected")
	}
}
