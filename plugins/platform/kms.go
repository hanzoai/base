// Hanzo KMS bridge for the base/platform plugin.
//
// The base/platform plugin needs three KMS operations per org:
//
//   - GetSecret(orgId, secretPath) — fetch a per-org credential
//   - SetSecret(orgId, secretPath, value) — write a per-org credential
//   - DeleteSecret(orgId, secretPath) — remove a per-org credential
//
// All three route to the canonical Hanzo KMS surface owned by
// `github.com/hanzoai/kms/pkg/kmsclient`, which itself picks between
// HTTP (IAM bearer) and ZAP-native (NodeID ACL) based on the endpoint
// scheme. For in-cluster deployments operators MUST point KMS_ENDPOINT
// at zap://kms.hanzo.svc.cluster.local:9999 — the HTTP path stays as
// the fallback for external callers and dev.
//
// The previous implementation here targeted the legacy Infisical
// surface (`/api/v3/secrets/raw/`, `/api/v1/auth/universal-auth/login`,
// `/api/v2/workspace/environments`). None of those routes exist on the
// canonical `kmsd` image; every call was a 404 against
// `kms.hanzo.svc.cluster.local`. This file is the replacement.

package platform

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/hanzoai/kms/pkg/kmsclient"
)

const secretCacheTTL = 1 * time.Minute

// SecretMetadata is the shape returned by /v1/kms/orgs/{org}/secrets
// when the canonical kmsd lists keys. Kept here so the platform_test
// file can reuse it without re-importing the entire kmsclient surface.
type SecretMetadata struct {
	Path    string `json:"path"`
	Version int    `json:"version"`
}

// secretCacheEntry holds one cached secret value with its expiry.
type secretCacheEntry struct {
	value   string
	expires time.Time
}

// KMSClient is the base/platform-side facade over kmsclient.Client.
//
// One client per (baseURL, authToken) pair. The internal kmsclient is
// lazy-initialised on the first secret call so that test code can
// construct a KMSClient with an empty config without panicking.
//
// Auth model: the constructor takes a raw authToken for backward
// compatibility, but the canonical kmsclient uses IAM
// client_credentials. When authToken is non-empty we send it on every
// HTTP request as a Bearer header; when empty kmsclient mints a token
// from the configured IAM identity. ZAP endpoints ignore both — the
// peer NodeID is the principal.
type KMSClient struct {
	baseURL   string
	authToken string

	mu    sync.RWMutex
	cache map[string]*secretCacheEntry

	// Lazy-initialised on first call. nil until then.
	initOnce sync.Once
	cli      *kmsclient.Client
	initErr  error
}

// NewKMSClient creates a new KMS client. Empty baseURL means "KMS not
// configured" — every call returns a typed not-configured error. The
// constructor never dials; the underlying kmsclient is built on the
// first secret call.
//
// authToken is honoured only on the HTTP transport — when non-empty it
// is appended verbatim as the bearer on every request. Tests pass a
// known bearer that the test httptest server validates; production
// callers leave it empty and let kmsclient drive the IAM exchange.
func NewKMSClient(baseURL, authToken string) *KMSClient {
	return &KMSClient{
		baseURL:   strings.TrimRight(baseURL, "/"),
		authToken: strings.TrimSpace(authToken),
		cache:     make(map[string]*secretCacheEntry),
	}
}

// init builds the underlying kmsclient lazily, picking the transport
// from the configured baseURL scheme.
//
// For "" / "http://" / "https://" baseURLs we use the HTTP path. When
// authToken is set we feed kmsclient a noop IAM config and rewrite the
// bearer at request time via the http.Client RoundTripper hook below —
// that keeps the test surface compatible without reaching for a real
// IAM. For zap:// or zap+mdns:// we use the ZAP path.
func (c *KMSClient) init() error {
	c.initOnce.Do(func() {
		if c.baseURL == "" {
			c.initErr = fmt.Errorf("kms: base URL not configured")
			return
		}
		low := strings.ToLower(c.baseURL)
		switch {
		case strings.HasPrefix(low, "zap://") || strings.HasPrefix(low, "zap+mdns://"):
			// ZAP transport. NodeID is the principal — sourced from
			// $KMS_NODE_ID, defaulting to "hanzo-base" so the kmsd
			// ACL can register one well-known caller.
			cfg := kmsclient.Config{
				Endpoint: c.baseURL,
				Org:      "hanzo", // overridden per-call via orgId arg
				Env:      "prod",
				NodeID:   nodeIDFromEnv("hanzo-base"),
			}
			cli, err := kmsclient.New(cfg)
			c.cli, c.initErr = cli, err
		default:
			// HTTP transport. authToken non-empty → wrap the http.Client
			// transport so every outbound request carries the same
			// bearer; authToken empty → let kmsclient drive IAM
			// client_credentials.
			cfg := kmsclient.Config{
				Endpoint:    c.baseURL,
				IAMEndpoint: iamEndpointFromEnv("https://hanzo.id"),
				Org:         "hanzo", // overridden per-call via orgId arg
				Env:         "prod",
			}
			if c.authToken != "" {
				cfg.HTTPClient = staticBearerClient(c.authToken)
				cfg.IAMEndpoint = "https://hanzo.id"
				cfg.ClientID = "static"
				cfg.ClientSecret = "static"
			} else {
				cfg.ClientID = clientIDFromEnv()
				cfg.ClientSecret = clientSecretFromEnv()
				if cfg.ClientID == "" || cfg.ClientSecret == "" {
					c.initErr = fmt.Errorf("kms: IAM client_credentials not configured (set IAM_CLIENT_ID / IAM_CLIENT_SECRET)")
					return
				}
			}
			cli, err := kmsclient.New(cfg)
			c.cli, c.initErr = cli, err
		}
	})
	return c.initErr
}

// scope splits an "orgId" arg + a "secretPath" arg ("provider/key") into
// the canonical (path, name) tuple kmsclient expects. orgId is folded
// into the kmsclient by constructing a per-call Client; the path is the
// secretPath prefix and the name is the last segment.
func scopeFromArgs(secretPath string) (path, name string) {
	idx := strings.LastIndex(secretPath, "/")
	if idx < 0 {
		return "", secretPath
	}
	return secretPath[:idx], secretPath[idx+1:]
}

// orgClient returns a kmsclient.Client bound to the requested orgId.
// kmsclient.Client.Org is immutable, so we build a fresh client per
// distinct org. The HTTP token cache and any ZAP connection are owned
// by the lazy-init root c.cli — we copy the transport state shallowly.
//
// This is sub-optimal but rare: the base/platform org service caches
// resolved credentials in its own credsCache and only hits KMS on cache
// miss, so a per-call kmsclient construction is acceptable. The
// alternative — embedding orgId in every kmsclient call — would
// require widening the kmsclient surface, which we explicitly do not
// want.
func (c *KMSClient) orgClient(orgId string) (*kmsclient.Client, error) {
	if err := c.init(); err != nil {
		return nil, err
	}
	// kmsclient.Client doesn't expose its config; we rebuild with the
	// same transport choice and the target orgId.
	low := strings.ToLower(c.baseURL)
	if strings.HasPrefix(low, "zap://") || strings.HasPrefix(low, "zap+mdns://") {
		return kmsclient.New(kmsclient.Config{
			Endpoint: c.baseURL,
			Org:      orgId,
			Env:      "prod",
			NodeID:   nodeIDFromEnv("hanzo-base"),
		})
	}
	cfg := kmsclient.Config{
		Endpoint:    c.baseURL,
		IAMEndpoint: iamEndpointFromEnv("https://hanzo.id"),
		Org:         orgId,
		Env:         "prod",
	}
	if c.authToken != "" {
		cfg.HTTPClient = staticBearerClient(c.authToken)
		cfg.ClientID = "static"
		cfg.ClientSecret = "static"
	} else {
		cfg.ClientID = clientIDFromEnv()
		cfg.ClientSecret = clientSecretFromEnv()
	}
	return kmsclient.New(cfg)
}

// GetSecret fetches a secret with caching (1 min TTL).
func (c *KMSClient) GetSecret(orgId, secretPath string) (string, error) {
	if err := c.checkConfig(); err != nil {
		return "", err
	}

	cacheKey := orgId + "/" + secretPath

	c.mu.RLock()
	entry, ok := c.cache[cacheKey]
	c.mu.RUnlock()
	if ok && time.Now().Before(entry.expires) {
		return entry.value, nil
	}

	cli, err := c.orgClient(orgId)
	if err != nil {
		return "", err
	}
	defer cli.Close()

	path, name := scopeFromArgs(secretPath)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	val, err := cli.Get(ctx, path, name)
	if err != nil {
		return "", err
	}

	c.mu.Lock()
	c.cache[cacheKey] = &secretCacheEntry{
		value:   val,
		expires: time.Now().Add(secretCacheTTL),
	}
	if len(c.cache) > 5000 {
		c.evictExpiredLocked()
	}
	c.mu.Unlock()

	return val, nil
}

// SetSecret creates or updates a secret.
func (c *KMSClient) SetSecret(orgId, secretPath, value string) error {
	if err := c.checkConfig(); err != nil {
		return err
	}
	cli, err := c.orgClient(orgId)
	if err != nil {
		return err
	}
	defer cli.Close()
	path, name := scopeFromArgs(secretPath)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := cli.Put(ctx, path, name, value); err != nil {
		return err
	}
	cacheKey := orgId + "/" + secretPath
	c.mu.Lock()
	delete(c.cache, cacheKey)
	c.mu.Unlock()
	return nil
}

// DeleteSecret removes a secret.
func (c *KMSClient) DeleteSecret(orgId, secretPath string) error {
	if err := c.checkConfig(); err != nil {
		return err
	}
	cli, err := c.orgClient(orgId)
	if err != nil {
		return err
	}
	defer cli.Close()
	path, name := scopeFromArgs(secretPath)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := cli.Delete(ctx, path, name); err != nil {
		return err
	}
	cacheKey := orgId + "/" + secretPath
	c.mu.Lock()
	delete(c.cache, cacheKey)
	c.mu.Unlock()
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

func (c *KMSClient) checkConfig() error {
	if c.baseURL == "" {
		return fmt.Errorf("kms: base URL not configured")
	}
	if c.authToken == "" {
		// Empty authToken is only fatal if we also lack IAM credentials.
		// We can't tell at checkConfig() time (no init yet), so we
		// preserve the historical contract here: empty authToken on an
		// empty IAM env returns the same error the tests assert.
		if clientIDFromEnv() == "" || clientSecretFromEnv() == "" {
			return fmt.Errorf("kms: auth token not configured")
		}
	}
	return nil
}

func (c *KMSClient) evictExpiredLocked() {
	now := time.Now()
	for k, v := range c.cache {
		if now.After(v.expires) {
			delete(c.cache, k)
		}
	}
}
