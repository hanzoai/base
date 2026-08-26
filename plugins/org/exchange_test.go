package org

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// stubIAM answers the token endpoint and records the form it was posted.
func stubIAM(t *testing.T, status int, body string) (*httptest.Server, *url.Values) {
	t.Helper()
	var got url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/iam/oauth/token" {
			t.Errorf("posted to %s, want /v1/iam/oauth/token", r.URL.Path)
		}
		_ = r.ParseForm()
		got = r.PostForm
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv, &got
}

// A service that owns a login form gets its token FROM IAM. Base verifies every
// bearer against IAM's JWKS, so this is the only kind of token that
// authenticates anything — one a service mints itself does not.
func TestExchangePasswordAsksIAMAndReturnsItsTokens(t *testing.T) {
	srv, form := stubIAM(t, 200, `{"access_token":"at-1","refresh_token":"rt-1"}`)

	at, rt, err := ExchangePassword("z@lux.financial", "secret", "lux", Config{
		IAMEndpoint:     srv.URL,
		IAMClientID:     "lux-bank",
		IAMClientSecret: "shh",
	})
	if err != nil {
		t.Fatalf("ExchangePassword: %v", err)
	}
	if at != "at-1" || rt != "rt-1" {
		t.Fatalf("tokens = %q/%q, want at-1/rt-1", at, rt)
	}
	for k, want := range map[string]string{
		"grant_type":    "password",
		"username":      "z@lux.financial",
		"password":      "secret",
		"organization":  "lux",
		"client_id":     "lux-bank",
		"client_secret": "shh",
	} {
		if got := form.Get(k); got != want {
			t.Errorf("form[%s] = %q, want %q", k, got, want)
		}
	}
}

// Empty org lets IAM resolve the user in the client's own organization, so the
// field is omitted rather than sent blank — a blank organization is a different
// request from an absent one.
func TestExchangePasswordOmitsAnEmptyOrg(t *testing.T) {
	srv, form := stubIAM(t, 200, `{"access_token":"at"}`)
	if _, _, err := ExchangePassword("u", "p", "", Config{IAMEndpoint: srv.URL}); err != nil {
		t.Fatalf("ExchangePassword: %v", err)
	}
	if _, present := (*form)["organization"]; present {
		t.Fatalf("organization was sent as %q when none was asked for", form.Get("organization"))
	}
}

// A refusal from IAM is a refusal here. Returning an empty token with no error
// would sign the caller in as nobody.
func TestExchangePasswordRefusesWhenIAMDoes(t *testing.T) {
	srv, _ := stubIAM(t, 400, `{"error":"invalid_grant"}`)
	at, _, err := ExchangePassword("u", "wrong", "", Config{IAMEndpoint: srv.URL})
	if err == nil {
		t.Fatal("a rejected credential returned no error")
	}
	if at != "" {
		t.Fatalf("a rejected credential returned the token %q", at)
	}
}

// A 200 carrying no token is not a sign-in either.
func TestExchangePasswordRefusesATokenlessSuccess(t *testing.T) {
	srv, _ := stubIAM(t, 200, `{"token_type":"Bearer"}`)
	if _, _, err := ExchangePassword("u", "p", "", Config{IAMEndpoint: srv.URL}); err == nil {
		t.Fatal("a response with no access_token was accepted")
	}
}

func TestExchangePasswordRequiresBothHalvesOfTheCredential(t *testing.T) {
	for _, tc := range []struct{ user, pass string }{{"", "p"}, {"u", ""}, {"", ""}} {
		if _, _, err := ExchangePassword(tc.user, tc.pass, "", Config{IAMEndpoint: "https://iam.example"}); err == nil {
			t.Errorf("ExchangePassword(%q, %q) was accepted", tc.user, tc.pass)
		}
	}
}

// Minting has to address the brand's public origin: IAM derives the issuer from
// the request host, and a relying party that discovered through one brand
// rejects a token issued by another. An in-cluster address is refused rather
// than quietly minting under the default brand.
func TestMintingRefusesTheInClusterAddress(t *testing.T) {
	for _, endpoint := range []string{"iam.hanzo.svc.cluster.local:9653", "zap://iam:9999"} {
		_, _, err := ExchangePassword("u", "p", "", Config{IAMEndpoint: endpoint})
		if err == nil {
			t.Fatalf("minting through %q was allowed", endpoint)
		}
		if !strings.Contains(err.Error(), "public origin") {
			t.Fatalf("err = %v, want it to name the public origin", err)
		}
	}
}

// Both grants go through the same door, so a fix to one is a fix to both.
func TestBothGrantsUseOneTokenEndpoint(t *testing.T) {
	srv, form := stubIAM(t, 200, `{"access_token":"at"}`)
	if _, _, err := ExchangeOAuth2Token("code-1", "https://app/cb", Config{IAMEndpoint: srv.URL}); err != nil {
		t.Fatalf("ExchangeOAuth2Token: %v", err)
	}
	if got := form.Get("grant_type"); got != "authorization_code" {
		t.Fatalf("grant_type = %q", got)
	}
	if got := form.Get("code"); got != "code-1" {
		t.Fatalf("code = %q", got)
	}
}
