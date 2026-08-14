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
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/sync/singleflight"

	"github.com/hanzoai/base/tools/cache"
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
// Backed by cache.TTL (luxfi/cache LRU + time-based expiration).
type JWKSCache struct {
	keys     *cache.TTL[string, *JWK]     // keyed by "jwksURL#kid"
	docs     *cache.TTL[string, struct{}] // the document this process holds a copy of
	fetching singleflight.Group           // one fetch per document, however many ask
	client   *http.Client
}

// NewJWKSCache creates a new JWKS cache with the given TTL.
// Up to 256 key entries are cached (ample for multi-provider JWKS).
func NewJWKSCache(ttl time.Duration) *JWKSCache {
	return &JWKSCache{
		keys: cache.NewTTL[string, *JWK](256, ttl),
		docs: cache.NewTTL[string, struct{}](16, ttl),
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// FetchKey retrieves a JWK by kid from the JWKS endpoint, using the cache.
//
// The document is what is cached, not the key, so a kid the issuer does not
// publish is answered from the copy already held. The kid arrives on an
// unverified token — it is chosen by whoever sent it — so a key lookup must
// not be a way to ask this process to make a request.
//
// Concurrent lookups that do need the document share one fetch: the key rolls
// over for every caller at once, so without this a rotation is a burst.
func (c *JWKSCache) FetchKey(ctx context.Context, jwksURL, kid string) (*JWK, error) {
	if key, ok := c.keys.Get(jwksURL + "#" + kid); ok {
		return key, nil
	}
	if _, fresh := c.docs.Get(jwksURL); fresh {
		return nil, fmt.Errorf("jwks: key with kid %q not found at %s", kid, jwksURL)
	}

	keys, err, _ := c.fetching.Do(jwksURL, func() (any, error) {
		return c.fetchFromRemote(ctx, jwksURL)
	})
	if err != nil {
		return nil, err
	}

	for _, key := range keys.([]*JWK) {
		c.keys.Put(jwksURL+"#"+key.Kid, key)
	}
	c.docs.Put(jwksURL, struct{}{})

	for _, key := range keys.([]*JWK) {
		if key.Kid == kid {
			return key, nil
		}
	}
	return nil, fmt.Errorf("jwks: key with kid %q not found at %s", kid, jwksURL)
}

func (c *JWKSCache) fetchFromRemote(ctx context.Context, jwksURL string) ([]*JWK, error) {
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

	return jwks.Keys, nil
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

	// A caller that brings no cache gets one for this call, so the document is
	// still read once and an unpublished kid still costs nothing.
	if jwksCache == nil {
		jwksCache = NewJWKSCache(10 * time.Minute)
	}
	key, err := jwksCache.FetchKey(ctx, jwksURL, kid)
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
