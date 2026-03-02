package security

import (
	"context"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// JWK represents a JSON Web Key (RSA only, which covers RS256/RS384/RS512).
type JWK struct {
	Kty string `json:"kty"`
	Kid string `json:"kid"`
	Use string `json:"use"`
	Alg string `json:"alg"`
	E   string `json:"e"`
	N   string `json:"n"`
}

// PublicKey reconstructs the RSA public key from the JWK.
func (k *JWK) PublicKey() (*rsa.PublicKey, error) {
	if k.Kty != "RSA" {
		return nil, fmt.Errorf("unsupported key type %q, expected RSA", k.Kty)
	}

	exponent, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(k.E, "="))
	if err != nil {
		return nil, fmt.Errorf("decode exponent: %w", err)
	}

	modulus, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(k.N, "="))
	if err != nil {
		return nil, fmt.Errorf("decode modulus: %w", err)
	}

	return &rsa.PublicKey{
		E: int(big.NewInt(0).SetBytes(exponent).Uint64()),
		N: big.NewInt(0).SetBytes(modulus),
	}, nil
}

// JWKSCache caches fetched JWKS keys with a configurable TTL.
type JWKSCache struct {
	mu      sync.RWMutex
	keys    map[string]*jwksCacheEntry // keyed by JWKS URL + kid
	ttl     time.Duration
	client  *http.Client
}

type jwksCacheEntry struct {
	key     *JWK
	expires time.Time
}

// NewJWKSCache creates a new JWKS cache with the given TTL.
func NewJWKSCache(ttl time.Duration) *JWKSCache {
	return &JWKSCache{
		keys: make(map[string]*jwksCacheEntry),
		ttl:  ttl,
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// FetchKey retrieves a JWK by kid from the JWKS endpoint, using the cache.
func (c *JWKSCache) FetchKey(ctx context.Context, jwksURL, kid string) (*JWK, error) {
	cacheKey := jwksURL + "#" + kid

	c.mu.RLock()
	entry, ok := c.keys[cacheKey]
	c.mu.RUnlock()

	if ok && time.Now().Before(entry.expires) {
		return entry.key, nil
	}

	// Fetch from remote.
	key, err := c.fetchFromRemote(ctx, jwksURL, kid)
	if err != nil {
		return nil, err
	}

	c.mu.Lock()
	c.keys[cacheKey] = &jwksCacheEntry{
		key:     key,
		expires: time.Now().Add(c.ttl),
	}
	// Evict expired entries if cache grows.
	if len(c.keys) > 100 {
		now := time.Now()
		for k, v := range c.keys {
			if now.After(v.expires) {
				delete(c.keys, k)
			}
		}
	}
	c.mu.Unlock()

	return key, nil
}

func (c *JWKSCache) fetchFromRemote(ctx context.Context, jwksURL, kid string) (*JWK, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", jwksURL, nil)
	if err != nil {
		return nil, fmt.Errorf("jwks: create request: %w", err)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("jwks: fetch failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 256*1024))
	if err != nil {
		return nil, fmt.Errorf("jwks: read body: %w", err)
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("jwks: %s returned %d: %s", jwksURL, resp.StatusCode, string(body))
	}

	var jwks struct {
		Keys []*JWK `json:"keys"`
	}
	if err := json.Unmarshal(body, &jwks); err != nil {
		return nil, fmt.Errorf("jwks: decode: %w", err)
	}

	for _, key := range jwks.Keys {
		if key.Kid == kid {
			return key, nil
		}
	}

	return nil, fmt.Errorf("jwks: key with kid %q not found at %s", kid, jwksURL)
}

// ParseJWTWithJWKS validates a JWT against a JWKS endpoint and returns
// the verified claims. Supports RS256, RS384, RS512.
//
// The jwksCache parameter is optional; pass nil to skip caching (not recommended).
func ParseJWTWithJWKS(ctx context.Context, token string, jwksURL string, jwksCache *JWKSCache) (jwt.MapClaims, error) {
	if token == "" {
		return nil, errors.New("empty token")
	}
	if jwksURL == "" {
		return nil, errors.New("empty JWKS URL")
	}

	// Parse unverified to extract kid from header.
	parser := &jwt.Parser{}
	unverified, _, err := parser.ParseUnverified(token, jwt.MapClaims{})
	if err != nil {
		return nil, fmt.Errorf("jwks: parse unverified: %w", err)
	}

	kid, _ := unverified.Header["kid"].(string)
	if kid == "" {
		return nil, errors.New("jwks: missing kid in token header")
	}

	// Fetch the key.
	var key *JWK
	if jwksCache != nil {
		key, err = jwksCache.FetchKey(ctx, jwksURL, kid)
	} else {
		key, err = (&JWKSCache{
			client: &http.Client{Timeout: 10 * time.Second},
		}).fetchFromRemote(ctx, jwksURL, kid)
	}
	if err != nil {
		return nil, err
	}

	pubKey, err := key.PublicKey()
	if err != nil {
		return nil, fmt.Errorf("jwks: reconstruct public key: %w", err)
	}

	// Determine allowed methods from the key's alg field.
	allowedMethods := []string{"RS256", "RS384", "RS512"}
	if key.Alg != "" {
		allowedMethods = []string{key.Alg}
	}

	// Parse and verify the token.
	verifyParser := jwt.NewParser(jwt.WithValidMethods(allowedMethods))
	parsedToken, err := verifyParser.Parse(token, func(t *jwt.Token) (any, error) {
		return pubKey, nil
	})
	if err != nil {
		return nil, fmt.Errorf("jwks: verify token: %w", err)
	}

	claims, ok := parsedToken.Claims.(jwt.MapClaims)
	if !ok || !parsedToken.Valid {
		return nil, errors.New("jwks: invalid token claims")
	}

	return claims, nil
}
