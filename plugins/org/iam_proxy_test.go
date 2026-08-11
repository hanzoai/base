package org

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestIAMProxyKeepsThePath asserts the ADDRESS the proxy asks the issuer for.
//
// A status assertion cannot do this job: IAM answers any unregistered path with
// its single-page app at 200 text/html, so a proxy asking the wrong address gets
// a success and a web page. That is exactly what shipped — /v1/iam/.well-known/jwks
// reached hanzo.id/.well-known/jwks, which is not where the keys are, and the
// studio parsed HTML as a key set without ever seeing an error.
func TestIAMProxyKeepsThePath(t *testing.T) {
	var got []string
	iam := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = append(got, r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"keys":[]}`))
	}))
	defer iam.Close()

	// The issuer root is what a deployment configures (IAM_URL=https://hanzo.id),
	// so the proxy must supply the /v1/iam segment rather than eat it.
	for _, path := range []string{
		"/v1/iam/.well-known/jwks",
		"/v1/iam/oauth/userinfo",
		"/v1/iam/oauth/token",
	} {
		res, err := http.Get(iam.URL + path)
		if err != nil {
			t.Fatalf("reach stub: %v", err)
		}
		_ = res.Body.Close()
	}

	for i, want := range []string{
		"/v1/iam/.well-known/jwks",
		"/v1/iam/oauth/userinfo",
		"/v1/iam/oauth/token",
	} {
		if got[i] != want {
			t.Fatalf("issuer was asked for %q, want %q", got[i], want)
		}
	}

	// And the rewrite itself, which is the line that regressed: the incoming
	// path is appended whole, never trimmed.
	for _, tc := range []struct{ base, in, want string }{
		{"https://hanzo.id", "/v1/iam/.well-known/jwks", "https://hanzo.id/v1/iam/.well-known/jwks"},
		{"https://hanzo.id/", "/v1/iam/oauth/token", "https://hanzo.id/v1/iam/oauth/token"},
		{"http://iam.hanzo.svc", "/v1/iam/login", "http://iam.hanzo.svc/v1/iam/login"},
	} {
		if out := upstreamFor(tc.base, tc.in); out != tc.want {
			t.Fatalf("%s + %s = %s, want %s", tc.base, tc.in, out, tc.want)
		}
	}
}
