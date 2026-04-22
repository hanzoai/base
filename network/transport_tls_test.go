package network

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"testing"
	"time"
)

// generateTestCerts makes a self-signed CA + a leaf cert with the given
// DNS SANs. Used by TLS tests to validate the SAN-pinning path without
// standing up a real PKI.
func generateTestCerts(t *testing.T, sans []string) (caPem, leafPem, keyPem []byte) {
	t.Helper()

	// CA.
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("ca key: %v", err)
	}
	caTmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "base-network-test-ca"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
	}
	caDER, err := x509.CreateCertificate(rand.Reader, caTmpl, caTmpl, &caKey.PublicKey, caKey)
	if err != nil {
		t.Fatalf("ca create: %v", err)
	}
	caPem = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER})

	// Leaf signed by CA with the requested SANs.
	leafKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("leaf key: %v", err)
	}
	leafTmpl := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: sans[0]},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		DNSNames:     sans,
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
	}
	leafDER, err := x509.CreateCertificate(rand.Reader, leafTmpl, caTmpl, &leafKey.PublicKey, caKey)
	if err != nil {
		t.Fatalf("leaf create: %v", err)
	}
	leafPem = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: leafDER})
	keyDER, err := x509.MarshalECPrivateKey(leafKey)
	if err != nil {
		t.Fatalf("leaf key marshal: %v", err)
	}
	keyPem = pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	return
}

func TestTLSConfig_Enabled(t *testing.T) {
	var zero *TLSConfig
	if zero.Enabled() {
		t.Error("nil *TLSConfig should not be Enabled")
	}
	empty := &TLSConfig{}
	if empty.Enabled() {
		t.Error("zero TLSConfig should not be Enabled")
	}

	ca, leaf, key := generateTestCerts(t, []string{"pod-a.ns.svc.cluster.local"})
	c := &TLSConfig{CACerts: ca, ServerCert: leaf, ServerKey: key}
	if !c.Enabled() {
		t.Error("expected Enabled with CA + ServerCert/Key")
	}
}

func TestTLSConfig_ServerConfigRejectsUnconfigured(t *testing.T) {
	c := &TLSConfig{}
	if _, err := c.ServerConfig(); err == nil {
		t.Error("ServerConfig on unconfigured TLSConfig must error")
	}
}

func TestTLSConfig_VerifyPeerPinning(t *testing.T) {
	ca, leaf, _ := generateTestCerts(t, []string{"pod-a.ns.svc.cluster.local"})

	// Parse the leaf so we can hand it to the verifier as a "verified chain".
	block, _ := pem.Decode(leaf)
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("parse leaf: %v", err)
	}

	// Allowlist includes the leaf's SAN → verify passes.
	ok := &TLSConfig{
		CACerts:     ca,
		AllowedSANs: []string{"pod-a.ns.svc.cluster.local", "pod-b.ns.svc.cluster.local"},
	}
	if err := ok.verifyPeerCertificate(nil, [][]*x509.Certificate{{cert}}); err != nil {
		t.Errorf("SAN in allowlist should pass: %v", err)
	}

	// Allowlist excludes the leaf's SAN → verify fails with ErrTLSUnpinnedPeer.
	bad := &TLSConfig{
		CACerts:     ca,
		AllowedSANs: []string{"pod-z.ns.svc.cluster.local"},
	}
	err = bad.verifyPeerCertificate(nil, [][]*x509.Certificate{{cert}})
	if err == nil {
		t.Fatal("unpinned peer SAN should be rejected")
	}
	// Error should be wrapping ErrTLSUnpinnedPeer.
	if !errorIs(err, ErrTLSUnpinnedPeer) {
		t.Errorf("expected ErrTLSUnpinnedPeer, got %v", err)
	}

	// Empty allowlist → dev fallback, accept any cert that chained.
	dev := &TLSConfig{CACerts: ca}
	if err := dev.verifyPeerCertificate(nil, [][]*x509.Certificate{{cert}}); err != nil {
		t.Errorf("empty allowlist should accept: %v", err)
	}
}

// errorIs wraps errors.Is without requiring the caller to import errors.
func errorIs(err, target error) bool {
	for err != nil {
		if err == target {
			return true
		}
		type wrapper interface{ Unwrap() error }
		w, ok := err.(wrapper)
		if !ok {
			return false
		}
		err = w.Unwrap()
	}
	return false
}
