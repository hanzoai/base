package security

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// generateTestRSAKey creates an RSA key pair for testing.
func generateTestRSAKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	return key
}

// serveJWKS starts a test HTTP server that serves a JWKS with the given key.
func serveJWKS(t *testing.T, key *rsa.PublicKey, kid, alg string) *httptest.Server {
	t.Helper()

	jwks := map[string]any{
		"keys": []map[string]any{
			{
				"kty": "RSA",
				"kid": kid,
				"alg": alg,
				"use": "sig",
				"n":   base64.RawURLEncoding.EncodeToString(key.N.Bytes()),
				"e":   base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.E)).Bytes()),
			},
		},
	}

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(jwks)
	}))
	t.Cleanup(ts.Close)

	return ts
}

func TestJWKPublicKey(t *testing.T) {
	privKey := generateTestRSAKey(t)

	k := &JWK{
		Kty: "RSA",
		Kid: "test-kid",
		Alg: "RS256",
		N:   base64.RawURLEncoding.EncodeToString(privKey.PublicKey.N.Bytes()),
		E:   base64.RawURLEncoding.EncodeToString(big.NewInt(int64(privKey.PublicKey.E)).Bytes()),
	}

	pubKey, err := k.PublicKey()
	if err != nil {
		t.Fatal(err)
	}
	if pubKey.N.Cmp(privKey.PublicKey.N) != 0 {
		t.Fatal("public key modulus mismatch")
	}
	if pubKey.E != privKey.PublicKey.E {
		t.Fatal("public key exponent mismatch")
	}
}

func TestJWKPublicKey_UnsupportedKty(t *testing.T) {
	k := &JWK{Kty: "EC"}
	_, err := k.PublicKey()
	if err == nil {
		t.Fatal("expected error for unsupported key type")
	}
}

func TestParseJWTWithJWKS(t *testing.T) {
	privKey := generateTestRSAKey(t)
	kid := "test-kid-1"

	ts := serveJWKS(t, &privKey.PublicKey, kid, "RS256")

	// Create a valid JWT signed with the private key.
	claims := jwt.MapClaims{
		"sub":   "user-123",
		"owner": "org-456",
		"email": "test@example.com",
		"name":  "Test User",
		"exp":   time.Now().Add(time.Hour).Unix(),
		"iat":   time.Now().Unix(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	token.Header["kid"] = kid

	tokenStr, err := token.SignedString(privKey)
	if err != nil {
		t.Fatal(err)
	}

	// Test with no cache.
	parsed, err := ParseJWTWithJWKS(context.Background(), tokenStr, ts.URL, nil)
	if err != nil {
		t.Fatalf("ParseJWTWithJWKS failed: %v", err)
	}

	if sub, _ := parsed["sub"].(string); sub != "user-123" {
		t.Fatalf("expected sub=user-123, got %q", sub)
	}
	if owner, _ := parsed["owner"].(string); owner != "org-456" {
		t.Fatalf("expected owner=org-456, got %q", owner)
	}
	if email, _ := parsed["email"].(string); email != "test@example.com" {
		t.Fatalf("expected email=test@example.com, got %q", email)
	}
}

func TestParseJWTWithJWKS_Cached(t *testing.T) {
	privKey := generateTestRSAKey(t)
	kid := "cached-kid"

	ts := serveJWKS(t, &privKey.PublicKey, kid, "RS256")

	claims := jwt.MapClaims{
		"sub": "user-abc",
		"exp": time.Now().Add(time.Hour).Unix(),
		"iat": time.Now().Unix(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	token.Header["kid"] = kid

	tokenStr, err := token.SignedString(privKey)
	if err != nil {
		t.Fatal(err)
	}

	cache := NewJWKSCache(5 * time.Minute)

	// First call — fetches from server.
	parsed, err := ParseJWTWithJWKS(context.Background(), tokenStr, ts.URL, cache)
	if err != nil {
		t.Fatalf("first call failed: %v", err)
	}
	if sub, _ := parsed["sub"].(string); sub != "user-abc" {
		t.Fatalf("expected sub=user-abc, got %q", sub)
	}

	// Close the server — second call should use cache.
	ts.Close()

	parsed, err = ParseJWTWithJWKS(context.Background(), tokenStr, ts.URL, cache)
	if err != nil {
		t.Fatalf("cached call failed: %v", err)
	}
	if sub, _ := parsed["sub"].(string); sub != "user-abc" {
		t.Fatalf("expected sub=user-abc 2nd call, got %q", sub)
	}
}

func TestParseJWTWithJWKS_ExpiredToken(t *testing.T) {
	privKey := generateTestRSAKey(t)
	kid := "exp-kid"

	ts := serveJWKS(t, &privKey.PublicKey, kid, "RS256")

	claims := jwt.MapClaims{
		"sub": "user-exp",
		"exp": time.Now().Add(-time.Hour).Unix(), // expired
		"iat": time.Now().Add(-2 * time.Hour).Unix(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	token.Header["kid"] = kid

	tokenStr, err := token.SignedString(privKey)
	if err != nil {
		t.Fatal(err)
	}

	_, err = ParseJWTWithJWKS(context.Background(), tokenStr, ts.URL, nil)
	if err == nil {
		t.Fatal("expected error for expired token")
	}
}

func TestParseJWTWithJWKS_WrongKey(t *testing.T) {
	signingKey := generateTestRSAKey(t)
	wrongKey := generateTestRSAKey(t)
	kid := "wrong-kid"

	// Serve the wrong key's public key.
	ts := serveJWKS(t, &wrongKey.PublicKey, kid, "RS256")

	claims := jwt.MapClaims{
		"sub": "user-wrong",
		"exp": time.Now().Add(time.Hour).Unix(),
		"iat": time.Now().Unix(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	token.Header["kid"] = kid

	// Sign with the signing key (not the wrong key).
	tokenStr, err := token.SignedString(signingKey)
	if err != nil {
		t.Fatal(err)
	}

	_, err = ParseJWTWithJWKS(context.Background(), tokenStr, ts.URL, nil)
	if err == nil {
		t.Fatal("expected error for token signed with wrong key")
	}
}

func TestParseJWTWithJWKS_MissingKid(t *testing.T) {
	privKey := generateTestRSAKey(t)

	claims := jwt.MapClaims{
		"sub": "user-nokid",
		"exp": time.Now().Add(time.Hour).Unix(),
		"iat": time.Now().Unix(),
	}
	// Don't set kid header.
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)

	tokenStr, err := token.SignedString(privKey)
	if err != nil {
		t.Fatal(err)
	}

	_, err = ParseJWTWithJWKS(context.Background(), tokenStr, "http://localhost/jwks", nil)
	if err == nil {
		t.Fatal("expected error for missing kid")
	}
}

func TestParseJWTWithJWKS_EmptyInputs(t *testing.T) {
	_, err := ParseJWTWithJWKS(context.Background(), "", "http://example.com", nil)
	if err == nil {
		t.Fatal("expected error for empty token")
	}

	_, err = ParseJWTWithJWKS(context.Background(), "some.token.here", "", nil)
	if err == nil {
		t.Fatal("expected error for empty JWKS URL")
	}
}

func TestJWKSCache_Eviction(t *testing.T) {
	cache := NewJWKSCache(1 * time.Millisecond)

	privKey := generateTestRSAKey(t)
	kid := "evict-kid"

	ts := serveJWKS(t, &privKey.PublicKey, kid, "RS256")
	defer ts.Close()

	// Populate cache.
	_, err := cache.FetchKey(context.Background(), ts.URL, kid)
	if err != nil {
		t.Fatal(err)
	}

	// Wait for TTL to expire.
	time.Sleep(5 * time.Millisecond)

	// Cache should be expired, fetch again.
	key, err := cache.FetchKey(context.Background(), ts.URL, kid)
	if err != nil {
		t.Fatal(err)
	}
	if key.Kid != kid {
		t.Fatalf("expected kid=%q, got %q", kid, key.Kid)
	}
}
