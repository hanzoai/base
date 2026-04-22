package network

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
)

// R5 — Quasar p2p mTLS wrapper.
//
// The production transport must require peer authentication: without
// mTLS, any host reachable on port 9651 (default BASE_LISTEN_P2P)
// forges envelopes for any known shardID. ShardIDs are derived from
// JWT.sub / org_id and are enumerable, so the mTLS layer is the only
// cryptographic boundary between the internet and the consensus DAG.
//
// This file ships the CONFIG surface + cert-pinning verifier that the
// production transport (QUIC or gRPC, Phase-2 scope) plugs into. The
// implementation details of the wire protocol are intentionally out of
// scope here — wrapping is additive, so the Phase-2 transport links
// against `TLSConfig.ServerConfig() / ClientConfig()` and `VerifyPeer`
// without further API churn.
//
// Certs: two canonical sources.
//   1. Hanzo KMS plugin (base/plugins/kms) — issues short-lived (1 h)
//      Ed25519 certs per pod, SAN = pod DNS name, SPIFFE ID embedded.
//   2. Self-signed dev issuer — test/dev fallback while the KMS plugin
//      lands. CA key lives in the platform's secrets namespace;
//      operator mounts it as a Secret.
//
// Pinning: the peer's leaf cert SAN must match an entry in the
// operator-emitted BASE_PEERS list. Any cert whose SAN is outside the
// list is rejected even if it chains to the local CA — this stops a
// compromised neighbor pod from impersonating peers it's not supposed
// to be.

// TLSConfig is the transport-level mTLS settings plumbed through Config.
// Zero values mean "no TLS"; production callers MUST set CACerts + at
// least one of (ServerCert, ClientCert) to activate mTLS.
type TLSConfig struct {
	// CACerts are the PEM-encoded CA certificates the transport trusts
	// for peer certs. Supplied by the operator from the Base-network
	// Secret (KMS-issued) or the self-signed dev CA.
	CACerts []byte

	// ServerCert / ServerKey are the local pod's TLS identity. Both
	// PEM-encoded. Production: issued by KMS; dev: self-signed.
	ServerCert []byte
	ServerKey  []byte

	// ClientCert / ClientKey are optional — when the transport opens
	// outbound connections it presents these. Typically the same as
	// the server cert (pod identity is one cert per pod).
	ClientCert []byte
	ClientKey  []byte

	// AllowedSANs is the SAN allowlist: the pod DNS names from
	// BASE_PEERS. Any peer cert whose DNSNames slice does not include
	// one of these is rejected. Empty slice + non-nil CACerts = "trust
	// any cert chained to CA" (dev-only fallback).
	AllowedSANs []string
}

// ErrTLSUnpinnedPeer is returned by the verifier when a peer's leaf cert
// chains to the CA but does not present a SAN listed in AllowedSANs.
// This is the R5 defence — a compromised neighbor pod with a valid but
// unexpected identity cannot impersonate the real peers.
var ErrTLSUnpinnedPeer = errors.New("transport: peer SAN not in BASE_PEERS allowlist")

// Enabled reports whether this config activates TLS. Nil-safe.
func (c *TLSConfig) Enabled() bool {
	if c == nil {
		return false
	}
	return len(c.CACerts) > 0 && (len(c.ServerCert) > 0 || len(c.ClientCert) > 0)
}

// ServerConfig builds a *tls.Config suitable for an mTLS listener. The
// VerifyPeerCertificate hook rejects any peer whose SAN is not in the
// allowlist even when the chain verifies.
func (c *TLSConfig) ServerConfig() (*tls.Config, error) {
	if !c.Enabled() {
		return nil, errors.New("transport: TLS not configured")
	}
	if len(c.ServerCert) == 0 || len(c.ServerKey) == 0 {
		return nil, errors.New("transport: ServerConfig requires ServerCert/ServerKey")
	}
	cert, err := tls.X509KeyPair(c.ServerCert, c.ServerKey)
	if err != nil {
		return nil, fmt.Errorf("transport: parse server keypair: %w", err)
	}
	pool, err := c.caPool()
	if err != nil {
		return nil, err
	}
	return &tls.Config{
		Certificates:          []tls.Certificate{cert},
		ClientCAs:             pool,
		ClientAuth:            tls.RequireAndVerifyClientCert,
		MinVersion:            tls.VersionTLS13,
		VerifyPeerCertificate: c.verifyPeerCertificate,
	}, nil
}

// ClientConfig builds a *tls.Config for outbound connections. Same
// VerifyPeerCertificate hook — pinning works symmetrically.
func (c *TLSConfig) ClientConfig() (*tls.Config, error) {
	if !c.Enabled() {
		return nil, errors.New("transport: TLS not configured")
	}
	clientCert := c.ClientCert
	clientKey := c.ClientKey
	if len(clientCert) == 0 {
		// Re-use the server cert when no separate client cert is set.
		clientCert = c.ServerCert
		clientKey = c.ServerKey
	}
	if len(clientCert) == 0 || len(clientKey) == 0 {
		return nil, errors.New("transport: ClientConfig requires Client/ServerCert")
	}
	cert, err := tls.X509KeyPair(clientCert, clientKey)
	if err != nil {
		return nil, fmt.Errorf("transport: parse client keypair: %w", err)
	}
	pool, err := c.caPool()
	if err != nil {
		return nil, err
	}
	return &tls.Config{
		Certificates:          []tls.Certificate{cert},
		RootCAs:               pool,
		MinVersion:            tls.VersionTLS13,
		VerifyPeerCertificate: c.verifyPeerCertificate,
	}, nil
}

func (c *TLSConfig) caPool() (*x509.CertPool, error) {
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(c.CACerts) {
		return nil, errors.New("transport: failed to parse CACerts (not a PEM bundle?)")
	}
	return pool, nil
}

// verifyPeerCertificate is the SAN-pinning hook. Called after chain
// verification has already succeeded — we just need to decide whether
// the authenticated peer is on the allowlist.
func (c *TLSConfig) verifyPeerCertificate(_ [][]byte, verifiedChains [][]*x509.Certificate) error {
	if len(c.AllowedSANs) == 0 {
		// Dev fallback: trust any cert that verifies against the CA.
		// Production callers MUST set AllowedSANs from BASE_PEERS.
		return nil
	}
	if len(verifiedChains) == 0 || len(verifiedChains[0]) == 0 {
		return ErrTLSUnpinnedPeer
	}
	leaf := verifiedChains[0][0]
	// DNSNames SAN must intersect the allowlist. We do NOT fall back to
	// CommonName — legacy CN-as-hostname is insecure per RFC 6125.
	for _, peer := range leaf.DNSNames {
		for _, allowed := range c.AllowedSANs {
			if peer == allowed {
				return nil
			}
		}
	}
	return fmt.Errorf("%w: cert SAN %v, allow %v",
		ErrTLSUnpinnedPeer, leaf.DNSNames, c.AllowedSANs)
}
