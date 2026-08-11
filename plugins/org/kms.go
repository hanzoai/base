// KMS bridge for the base/platform plugin.
//
// The plugin needs three operations per org:
//
//   - GetSecret(orgId, secretPath)         — read a per-org credential
//   - SetSecret(orgId, secretPath, value)  — write a per-org credential
//   - DeleteSecret(orgId, secretPath)      — remove a per-org credential
//
// All three go to the ONE secrets store over the ONE in-cluster transport:
// native ZAP to github.com/luxfi/kms, the canonical KMS. There is no HTTP
// path. Secrets never live anywhere else.
//
// THE ORG IS A PATH SEGMENT. The KMS store shards its base boundary on the
// "orgs/{org}" prefix of the secret PATH; a path without that segment is
// deployment-wide and shared by every base. The previous implementation
// carried the org in an HTTP URL (/v1/kms/orgs/{org}/secrets/…) and passed the
// caller's bare path straight through on the ZAP transport — so on the
// transport operators are told to use in-cluster, every org read and wrote the
// same deployment-wide record. ref() below is the one place that mapping lives.
package org

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/luxfi/keys"
	"github.com/luxfi/kms/pkg/envelope"
	"github.com/luxfi/kms/pkg/zapclient"
)

const (
	// defaultKMSEndpoint is the in-cluster KMS ZAP service, the same default
	// github.com/luxfi/kms itself ships.
	defaultKMSEndpoint = "zap.kms.svc.cluster.local:9999"

	secretCacheTTL = 1 * time.Minute
	callTimeout    = 10 * time.Second
	mdnsScheme     = "zap+mdns://"
	zapScheme      = "zap://"
)

// ErrKMSNotConfigured is returned by every operation when no KMS endpoint is
// set. Callers treat it as "this deployment has no KMS" and fall back to the
// process environment; it is never a transport failure.
var ErrKMSNotConfigured = errors.New("kms: endpoint not configured")

// secrets is the KMS surface this bridge uses. *zapclient.Client satisfies it;
// tests substitute a recorder through the dialKMS seam.
type secrets interface {
	GetAt(ctx context.Context, path, name, env string) (string, error)
	PutAt(ctx context.Context, path, name, env, value string) error
	DeleteAt(ctx context.Context, path, name, env string) error
	Close()
}

// dialKMS is the production seam, mirroring luxfi/kms's own dial seam. addr is
// "" for mDNS discovery, otherwise a host:port.
//
// The identity is mnemonic-derived and REQUIRED: the KMS server verifies the
// signed envelope on every secret opcode and asks consensus whether the
// resulting NodeID may act on the path. No mnemonic → no dial. Fail closed.
var dialKMS = func(ctx context.Context, addr string) (secrets, error) {
	ident, err := serviceIdentity()
	if err != nil {
		return nil, err
	}
	return zapclient.DialWithConfig(ctx, zapclient.Config{
		NodeID:      nodeIDFromEnv("hanzo-base"),
		ServiceType: "_kms._tcp",
		PeerAddr:    addr,
		IdentityHeader: envelope.IdentityHeader{
			NodeID:      ident.NodeID,
			FullDigest:  ident.FullDigest,
			ServicePath: ident.ServicePath,
			PublicKey:   ident.PublicKey,
		},
		// *keys.ServiceIdentity satisfies envelope.Signer directly.
		Signer: ident,
	})
}

// serviceIdentity derives this deployment's signing identity from the BIP-39
// mnemonic the kms-operator mounts as LUX_MNEMONIC. Same (mnemonic, path) on
// every replica → the same NodeID byte-for-byte, so a scaled-out Base shares
// one consensus authorization.
func serviceIdentity() (*keys.ServiceIdentity, error) {
	mnemonic := strings.TrimSpace(os.Getenv("LUX_MNEMONIC"))
	if mnemonic == "" {
		mnemonic = strings.TrimSpace(os.Getenv("MNEMONIC"))
	}
	if mnemonic == "" {
		return nil, errors.New("kms: LUX_MNEMONIC is not set — the KMS transport is identity-signed and cannot dial anonymously")
	}
	return keys.NewServiceIdentity(mnemonic, servicePathFromEnv("hanzo/base"))
}

// KMSClient is the platform-side facade over the canonical KMS client.
//
// One connection serves every org — the org rides in the secret path, not in
// the connection — and it is dialled lazily on the first secret call so a
// deployment without KMS never touches the network. A failed dial or a
// transport error drops the connection so the next call redials; a KMS restart
// does not permanently disable secrets for the process lifetime.
type KMSClient struct {
	addr string // host:port; empty with mdns set means discovery
	mdns bool
	env  string

	mu    sync.RWMutex
	cache map[string]*secretCacheEntry

	connMu sync.Mutex
	cli    secrets
}

// secretCacheEntry holds one cached secret value with its expiry.
type secretCacheEntry struct {
	value   string
	expires time.Time
}

// NewKMSClient builds the bridge for a configured KMS endpoint.
//
// endpoint is "zap://host:port", "zap+mdns://_kms._tcp", or a bare "host:port".
// Empty means "this deployment has no KMS" — every call then returns
// ErrKMSNotConfigured and the caller falls back to the environment. An http(s)
// endpoint is a misconfiguration and is rejected here, where an operator sees
// it, rather than degrading every read into the env fallback at runtime.
func NewKMSClient(endpoint string) (*KMSClient, error) {
	ep := strings.TrimSpace(endpoint)
	c := &KMSClient{
		cache: make(map[string]*secretCacheEntry),
		env:   envOr("KMS_ENV", "prod"),
	}
	if ep == "" {
		return c, nil
	}
	low := strings.ToLower(ep)
	switch {
	case strings.HasPrefix(low, mdnsScheme):
		c.mdns = true
	case strings.HasPrefix(low, "http://"), strings.HasPrefix(low, "https://"):
		return nil, fmt.Errorf(
			"kms: %q is an HTTP endpoint — Base speaks native ZAP to KMS; "+
				"set the KMS endpoint to zap://host:9999 (or host:9999)", endpoint)
	case strings.HasPrefix(low, zapScheme):
		c.addr = strings.TrimSuffix(ep[len(zapScheme):], "/")
	default:
		c.addr = strings.TrimSuffix(ep, "/")
	}
	if c.addr == "" && !c.mdns {
		return nil, fmt.Errorf("kms: %q has no host:port", endpoint)
	}
	return c, nil
}

// configured reports whether this deployment has a KMS to talk to.
func (c *KMSClient) configured() bool { return c != nil && (c.addr != "" || c.mdns) }

// ref maps (org, "provider/key") onto the (path, name) coordinate the KMS store
// keys on. The org leads the path because that segment IS the base boundary.
func ref(orgId, secretPath string) (path, name string) {
	name = strings.Trim(strings.TrimSpace(secretPath), "/")
	dir := ""
	if i := strings.LastIndex(name, "/"); i >= 0 {
		dir, name = name[:i], name[i+1:]
	}
	path = "orgs/" + orgId
	if dir != "" {
		path += "/" + dir
	}
	return path, name
}

// conn returns the live KMS connection, dialling on first use and after a drop.
func (c *KMSClient) conn(ctx context.Context) (secrets, error) {
	c.connMu.Lock()
	defer c.connMu.Unlock()
	if c.cli != nil {
		return c.cli, nil
	}
	cli, err := dialKMS(ctx, c.addr)
	if err != nil {
		return nil, fmt.Errorf("kms: dial %s: %w", c.target(), err)
	}
	c.cli = cli
	return cli, nil
}

func (c *KMSClient) target() string {
	if c.mdns {
		return "_kms._tcp (mDNS)"
	}
	return c.addr
}

// drop closes and forgets the connection so the next call redials.
func (c *KMSClient) drop() {
	c.connMu.Lock()
	defer c.connMu.Unlock()
	if c.cli != nil {
		c.cli.Close()
		c.cli = nil
	}
}

// Close releases the KMS connection. Safe on a nil or unconfigured client.
func (c *KMSClient) Close() {
	if c == nil {
		return
	}
	c.drop()
}

// call runs one KMS operation against a live connection. A failure that is not
// the server's answer (not-found / forbidden) drops the connection.
func (c *KMSClient) call(fn func(context.Context, secrets) error) error {
	if !c.configured() {
		return ErrKMSNotConfigured
	}
	ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
	defer cancel()
	cli, err := c.conn(ctx)
	if err != nil {
		return err
	}
	err = fn(ctx, cli)
	if err != nil && !errors.Is(err, zapclient.ErrNotFound) && !errors.Is(err, zapclient.ErrForbidden) {
		c.drop()
	}
	return err
}

// GetSecret fetches a secret with caching (1 min TTL).
func (c *KMSClient) GetSecret(orgId, secretPath string) (string, error) {
	if !c.configured() {
		return "", ErrKMSNotConfigured
	}
	cacheKey := orgId + "/" + secretPath

	c.mu.RLock()
	entry, ok := c.cache[cacheKey]
	c.mu.RUnlock()
	if ok && time.Now().Before(entry.expires) {
		return entry.value, nil
	}

	path, name := ref(orgId, secretPath)
	var val string
	if err := c.call(func(ctx context.Context, cli secrets) error {
		v, err := cli.GetAt(ctx, path, name, c.env)
		val = v
		return err
	}); err != nil {
		return "", err
	}

	c.mu.Lock()
	c.cache[cacheKey] = &secretCacheEntry{value: val, expires: time.Now().Add(secretCacheTTL)}
	if len(c.cache) > 5000 {
		c.evictExpiredLocked()
	}
	c.mu.Unlock()

	return val, nil
}

// SetSecret creates or updates a secret.
func (c *KMSClient) SetSecret(orgId, secretPath, value string) error {
	path, name := ref(orgId, secretPath)
	if err := c.call(func(ctx context.Context, cli secrets) error {
		return cli.PutAt(ctx, path, name, c.env, value)
	}); err != nil {
		return err
	}
	c.forget(orgId + "/" + secretPath)
	return nil
}

// DeleteSecret removes a secret.
func (c *KMSClient) DeleteSecret(orgId, secretPath string) error {
	path, name := ref(orgId, secretPath)
	if err := c.call(func(ctx context.Context, cli secrets) error {
		return cli.DeleteAt(ctx, path, name, c.env)
	}); err != nil {
		return err
	}
	c.forget(orgId + "/" + secretPath)
	return nil
}

// InvalidateCache clears all cached secrets for an org.
func (c *KMSClient) InvalidateCache(orgId string) {
	prefix := orgId + "/"
	c.mu.Lock()
	for k := range c.cache {
		if strings.HasPrefix(k, prefix) {
			delete(c.cache, k)
		}
	}
	c.mu.Unlock()
}

func (c *KMSClient) forget(cacheKey string) {
	c.mu.Lock()
	delete(c.cache, cacheKey)
	c.mu.Unlock()
}

func (c *KMSClient) evictExpiredLocked() {
	now := time.Now()
	for k, v := range c.cache {
		if now.After(v.expires) {
			delete(c.cache, k)
		}
	}
}
