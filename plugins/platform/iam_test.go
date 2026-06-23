package platform

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestOAuthBase asserts the canonical IAM OAuth base path. The live Hanzo IAM
// (hanzo.id) AND the embedded provider both mount OIDC at ${IAMEndpoint}/v1/iam
// — NOT at the root. Constructing authorize/token/userinfo URLs without the
// /v1/iam prefix hits the SPA/static handler (HTML 200) instead of the OAuth
// endpoints, which is the root cause of team-go's error=exchange_failed.
func TestOAuthBase(t *testing.T) {
	cases := []struct {
		endpoint string
		want     string
	}{
		{"https://hanzo.id", "https://hanzo.id/v1/iam"},
		{"https://hanzo.id/", "https://hanzo.id/v1/iam"},
		{"", "https://hanzo.id/v1/iam"},
		{"http://localhost:8080", "http://localhost:8080/v1/iam"},
	}
	for _, c := range cases {
		got := PlatformConfig{IAMEndpoint: c.endpoint}.OAuthBase()
		if got != c.want {
			t.Errorf("OAuthBase(%q) = %q, want %q", c.endpoint, got, c.want)
		}
	}
}

// TestExchangeOAuth2TokenHitsCanonicalEndpoint proves the token exchange POSTs
// to ${IAMEndpoint}/v1/iam/oauth/token (the JSON OAuth endpoint), not the bare
// ${IAMEndpoint}/oauth/token (the SPA). Regression guard for exchange_failed.
func TestExchangeOAuth2TokenHitsCanonicalEndpoint(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		if r.URL.Path == "/v1/iam/oauth/token" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"access_token":"AT","refresh_token":"RT"}`))
			return
		}
		// Simulate the SPA fall-through that the bare /oauth/token path hits
		// on the real IAM: HTTP 200 text/html (would break JSON decode).
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<!DOCTYPE html><html></html>"))
	}))
	defer srv.Close()

	access, refresh, err := ExchangeOAuth2Token("code123", "https://app/cb", PlatformConfig{
		IAMEndpoint:     srv.URL,
		IAMClientID:     "hanzo-team",
		IAMClientSecret: "s3cr3t",
	})
	if err != nil {
		t.Fatalf("ExchangeOAuth2Token returned error: %v", err)
	}
	if gotPath != "/v1/iam/oauth/token" {
		t.Fatalf("token exchange hit %q, want /v1/iam/oauth/token", gotPath)
	}
	if access != "AT" || refresh != "RT" {
		t.Fatalf("got access=%q refresh=%q, want AT/RT", access, refresh)
	}
}

// TestValidateIAMTokenHitsCanonicalUserinfo proves token validation hits the
// canonical ${IAMEndpoint}/v1/iam/oauth/userinfo, not the dead Casdoor
// /api/userinfo (which now returns SPA HTML on hanzo.id).
func TestValidateIAMTokenHitsCanonicalUserinfo(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		if !strings.HasSuffix(r.URL.Path, "/v1/iam/oauth/userinfo") {
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte("<!DOCTYPE html>"))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"u1","email":"z@hanzo.ai","name":"Z"}`))
	}))
	defer srv.Close()

	u, err := ValidateIAMToken("tok", PlatformConfig{IAMEndpoint: srv.URL})
	if err != nil {
		t.Fatalf("ValidateIAMToken returned error: %v", err)
	}
	if gotPath != "/v1/iam/oauth/userinfo" {
		t.Fatalf("userinfo hit %q, want /v1/iam/oauth/userinfo", gotPath)
	}
	if u.ID != "u1" {
		t.Fatalf("got user id %q, want u1", u.ID)
	}
}
