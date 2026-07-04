package ca

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"testing"
	"time"
)

func newTestCSR(t *testing.T) string {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.CreateCertificateRequest(rand.Reader,
		&x509.CertificateRequest{Subject: pkix.Name{CommonName: "ignored-subject"}}, key)
	if err != nil {
		t.Fatal(err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: der}))
}

func TestGenerateLoadRoundtrip(t *testing.T) {
	authority, err := Generate()
	if err != nil {
		t.Fatal(err)
	}
	certPEM, keyPEM, err := authority.PEM()
	if err != nil {
		t.Fatal(err)
	}
	reloaded, err := Load(certPEM, keyPEM)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.CertPEM() != certPEM {
		t.Error("reloaded CA cert differs")
	}
}

func TestSignAgentCSRBindsNodeIdentity(t *testing.T) {
	authority, err := Generate()
	if err != nil {
		t.Fatal(err)
	}
	const nodeID = "0d9e4f3a-1111-4000-8000-000000000001"
	certPEM, notAfter, err := authority.SignAgentCSR(newTestCSR(t), nodeID)
	if err != nil {
		t.Fatal(err)
	}
	block, _ := pem.Decode([]byte(certPEM))
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	// Identity comes from the node, never from the CSR subject.
	if cert.Subject.CommonName != nodeID {
		t.Errorf("CN = %q, want node id", cert.Subject.CommonName)
	}
	if cert.NotAfter.Sub(time.Now()) > AgentCertTTL {
		t.Errorf("cert lives longer than the TTL: %v", cert.NotAfter)
	}
	if !notAfter.Equal(cert.NotAfter) {
		t.Errorf("reported notAfter %v != cert %v", notAfter, cert.NotAfter)
	}
	// Must chain to the CA and be a client cert.
	if _, err := cert.Verify(x509.VerifyOptions{
		Roots:     authority.Pool(),
		KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}); err != nil {
		t.Errorf("cert does not verify against its CA: %v", err)
	}

	if _, _, err := authority.SignAgentCSR("not a csr", nodeID); err == nil {
		t.Error("garbage CSR accepted")
	}
}

func TestServerCertHosts(t *testing.T) {
	authority, err := Generate()
	if err != nil {
		t.Fatal(err)
	}
	tlsCert, err := authority.ServerCert([]string{"logos.example.com", "192.0.2.10"})
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(tlsCert.Certificate[0])
	if err != nil {
		t.Fatal(err)
	}
	if len(cert.DNSNames) != 1 || cert.DNSNames[0] != "logos.example.com" {
		t.Errorf("DNSNames = %v", cert.DNSNames)
	}
	if len(cert.IPAddresses) != 1 || cert.IPAddresses[0].String() != "192.0.2.10" {
		t.Errorf("IPAddresses = %v", cert.IPAddresses)
	}
	if err := cert.VerifyHostname("logos.example.com"); err != nil {
		t.Error(err)
	}
}
