package tests

import (
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

// IAM signs tokens the way Hanzo IAM puts them on the wire (RS256, with the key
// id in the header) and serves the JWKS that verifies them at URL, which is what
// an app's apis.StoreKeyJWKSURL names.
type IAM struct {
	URL string
	key *rsa.PrivateKey
}

const iamKeyID = "iam-test"

// NewIAM starts an IAM for one test.
func NewIAM(t testing.TB) *IAM {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}

	jwks := map[string]any{"keys": []map[string]any{{
		"kty": "RSA",
		"kid": iamKeyID,
		"alg": "RS256",
		"use": "sig",
		"n":   base64.RawURLEncoding.EncodeToString(key.PublicKey.N.Bytes()),
		"e":   base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.PublicKey.E)).Bytes()),
	}}}

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(jwks)
	}))
	t.Cleanup(ts.Close)

	// A process caches a JWKS document by its address, and a closed test
	// server's port is soon another's: two IAMs at one address would read each
	// other's keys. A path of its own keeps every IAM's address its own.
	return &IAM{URL: ts.URL + "/" + rand.Text() + "/jwks", key: key}
}

// Token signs claims, valid for the next hour.
func (i *IAM) Token(t testing.TB, claims jwt.MapClaims) string {
	t.Helper()

	signed := jwt.MapClaims{"iat": time.Now().Unix(), "exp": time.Now().Add(time.Hour).Unix()}
	for k, v := range claims {
		signed[k] = v
	}

	jt := jwt.NewWithClaims(jwt.SigningMethodRS256, signed)
	jt.Header["kid"] = iamKeyID

	token, err := jt.SignedString(i.key)
	if err != nil {
		t.Fatal(err)
	}
	return token
}
