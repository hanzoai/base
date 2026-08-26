package org

import (
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/hanzoai/base/tests"
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

// TestIAMProxyIsNotBaseToAuthenticate pins that Base decides nothing about a
// caller of the endpoint whose whole job is to ask IAM who that caller is.
//
// A machine token carries no membership, so resolving one refuses the request
// before the proxy runs: after the org plugin began resolving an org on every
// route, a service calling IAM through Base got a 403 from Base instead of an
// answer from IAM. And a token that did carry an org bootstrapped that org's
// Base — a fresh SQLite file and a full migration run — as a side effect of
// asking IAM for a token.
func TestIAMProxyIsNotBaseToAuthenticate(t *testing.T) {
	var seen http.Header
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r.Header.Clone()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"keys":[]}`))
	}))
	defer upstream.Close()

	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatal(err)
	}
	defer app.Cleanup()

	iam := newIssuer(t)
	if err := Register(app, Config{IAMEndpoint: upstream.URL}); err != nil {
		t.Fatal(err)
	}
	// The issuer Base validates against, which is a different address from the
	// one it proxies to only because the stub above must observe the request.
	app.Store().Set("jwksURL", iam.url+"/v1/iam/.well-known/jwks")

	db := NewOrgDB(app, "")
	mux := serve(t, app)

	machine := iam.token(t, "app/kms-sync") // no membership: a machine
	code, body := call(t, mux, http.MethodGet, "/v1/iam/oauth/userinfo", machine, nil)
	if code != http.StatusOK {
		t.Fatalf("a machine token reaching IAM through Base answered %d %s, want the issuer's 200", code, body)
	}

	// A person's token reaches IAM too, and asking IAM a question does not open
	// a Base for the org that person belongs to.
	person := iam.token(t, "alpha/ann", "alpha")
	if code, body := call(t, mux, http.MethodGet, "/v1/iam/oauth/userinfo", person, nil); code != http.StatusOK {
		t.Fatalf("a person's token reaching IAM through Base answered %d %s", code, body)
	}
	if _, err := os.Stat(db.OrgDir("alpha")); err == nil {
		t.Fatal("a call to IAM opened a Base for the caller's org")
	}

	// What a client says about itself does not reach the issuer, which is the
	// one question this endpoint exists to ask.
	forged := map[string]string{
		"X-User-Id":          "root",
		"X-Org-Id":           "admin",
		"X-User-IsAdmin":     "true",
		"X-User-Permissions": "*",
		"X-Hanzo-Cleverness": "1",
	}
	if code, body := call(t, mux, http.MethodGet, "/v1/iam/oauth/userinfo", "", forged); code != http.StatusOK {
		t.Fatalf("proxy answered %d %s", code, body)
	}
	for name := range forged {
		if got := seen.Get(name); got != "" {
			t.Errorf("the issuer was told %s: %q", name, got)
		}
	}
}

// A reverse proxy hands a redirect back; it must not take one itself.
//
// /v1/iam/oauth/authorize answers 302 to the redirect_uri, and that redirect IS
// the code flow — the browser has to receive it. Go's default client follows up
// to ten, so this process fetched the app's own callback URL instead and the
// caller got 500 "iam unreachable" whenever that URL was not reachable from the
// server, which is the normal case for a location meant for a browser. Sign-in
// could not complete through the proxy at all.
func TestIAMProxyHandsBackARedirectRatherThanFollowingIt(t *testing.T) {
	var followed bool
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/followed" {
			followed = true
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("the proxy chased the redirect"))
			return
		}
		http.Redirect(w, r, "/followed?code=abc&state=xyz", http.StatusFound)
	}))
	defer upstream.Close()

	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatal(err)
	}
	defer app.Cleanup()

	if err := Register(app, Config{IAMEndpoint: upstream.URL}); err != nil {
		t.Fatal(err)
	}
	mux := serve(t, app)

	code, body := call(t, mux, http.MethodGet, "/v1/iam/oauth/authorize?redirect_uri=http://app/callback", "", nil)
	if followed {
		t.Fatal("the proxy fetched the redirect target itself; the browser never sees the location")
	}
	if code != http.StatusFound {
		t.Fatalf("status = %d, want 302 — the redirect must reach the caller, body: %s", code, body)
	}
}
