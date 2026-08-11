package org

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hanzoai/base/core"
	"github.com/hanzoai/base/tests"
)

// twoOrgs stands up one process serving alpha and beta, the way a deployment
// does, and hands back an issuer to mint tokens with.
func twoOrgs(t *testing.T) (core.App, *issuer, http.Handler, *OrgDB) {
	t.Helper()

	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(app.Cleanup)

	iam := newIssuer(t)
	// A KMS address that refuses at once. The credential routes reach KMS on
	// the path where they are ALLOWED to, and the default address is a cluster
	// name that resolves nowhere here, so every such call would sit out a dial
	// timeout — twenty seconds per read, to prove a refusal that never got near
	// a secret.
	if err := Register(app, Config{IAMEndpoint: iam.url, KMSEndpoint: "127.0.0.1:1"}); err != nil {
		t.Fatal(err)
	}

	db := NewOrgDB(app, "")
	for _, org := range []string{"alpha", "beta"} {
		if _, err := db.ProvisionOrg(org); err != nil {
			t.Fatal(err)
		}
	}

	return app, iam, serve(t, app), db
}

func call(t *testing.T, mux http.Handler, method, path, token string, headers map[string]string) (int, string) {
	t.Helper()

	r := httptest.NewRequest(method, path, strings.NewReader("{}"))
	r.Header.Set("Content-Type", "application/json")
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	for k, v := range headers {
		r.Header.Set(k, v)
	}

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, r)
	return rec.Code, rec.Body.String()
}

// orgRoutes is every route that names an org, which is every route the rule
// governs. Adding one without adding it here is the mistake the table exists to
// catch: three of these seven shipped with no check at all.
var orgRoutes = []struct{ method, path string }{
	{http.MethodGet, "/v1/bases/%s"},
	{http.MethodGet, "/v1/bases/%s/config"},
	{http.MethodGet, "/v1/bases/%s/creds/stripe"},
	{http.MethodPost, "/v1/bases/%s/creds/stripe"},
	{http.MethodDelete, "/v1/bases/%s/creds"},
	{http.MethodGet, "/v1/bases/%s/customers/beta%2Fbob"},
	{http.MethodPost, "/v1/bases/%s/customers/beta%2Fbob"},
}

// TestNamingAnotherOrgIsRefusedOnEveryRoute pins the rule for the whole
// subtree: the org a request names is the org it acts in.
//
// The refusal must be 403 and not 404. A 404 is an answer about the data, so a
// caller who reads one learns the check passed and the org was simply empty —
// which is what /v1/bases/{org}/config answered against production, and the
// only reason the read looked harmless is that no org has a config row yet.
func TestNamingAnotherOrgIsRefusedOnEveryRoute(t *testing.T) {
	// A credential store beta could have, so a read that gets through has
	// something to return.
	t.Setenv("STRIPE_API_KEY", "beta-secret")

	_, iam, mux, _ := twoOrgs(t)
	alpha := iam.token(t, "alpha/ann", "alpha")

	for _, r := range orgRoutes {
		path := strings.Replace(r.path, "%s", "beta", 1)
		code, body := call(t, mux, r.method, path, alpha, nil)
		if code != http.StatusForbidden {
			t.Errorf("%s %s as alpha answered %d %s, want 403", r.method, path, code, body)
		}
		if strings.Contains(body, "beta-secret") {
			t.Errorf("%s %s handed alpha beta's credential: %s", r.method, path, body)
		}
	}
}

// TestActingInAnOrgReachesIt is the other half, and the half that makes the
// first one worth having: the same routes answer for the org the caller does
// act in. A rule that refuses everyone is not a rule, it is an outage.
func TestActingInAnOrgReachesIt(t *testing.T) {
	_, iam, mux, _ := twoOrgs(t)
	alpha := iam.token(t, "alpha/ann", "alpha")

	for _, r := range orgRoutes {
		path := strings.Replace(r.path, "%s", "alpha", 1)
		if code, body := call(t, mux, r.method, path, alpha, nil); code == http.StatusForbidden {
			t.Errorf("%s %s refused alpha its own org: %s", r.method, path, body)
		}
	}
}

// TestOperatorReachesTheOrgItNames pins the one cross-tenant scope in the
// estate. An operator is a member of the reserved admin org, says which org it
// means, and reaches it — through exactly the same rule as everyone else, since
// selecting an org is what EffectiveOrg grants a platform operator over any org
// and everyone else over their own.
func TestOperatorReachesTheOrgItNames(t *testing.T) {
	_, iam, mux, _ := twoOrgs(t)

	// Home is a brand org; membership in `admin` is what confers the scope.
	op := iam.token(t, "hanzo/operator", "hanzo", "admin")
	beta := map[string]string{"X-Org-Id": "beta"}

	for _, r := range orgRoutes {
		path := strings.Replace(r.path, "%s", "beta", 1)
		if code, body := call(t, mux, r.method, path, op, beta); code == http.StatusForbidden {
			t.Errorf("%s %s refused a platform operator acting as beta: %s", r.method, path, body)
		}
	}

	// And naming one org while acting in another is refused for an operator
	// too. The scope is over which org it may act in, not over the rule.
	code, body := call(t, mux, http.MethodGet, "/v1/bases/alpha/config", op, beta)
	if code != http.StatusForbidden {
		t.Errorf("an operator acting as beta read alpha: %d %s", code, body)
	}
}

// TestAnUncredentialedCallerNamesNoOrg pins the failure direction. A request
// carrying nothing resolves no org, and no org matches every org rather than
// none unless the comparison says so.
func TestAnUncredentialedCallerNamesNoOrg(t *testing.T) {
	_, _, mux, _ := twoOrgs(t)

	for _, r := range orgRoutes {
		path := strings.Replace(r.path, "%s", "alpha", 1)
		if code, body := call(t, mux, r.method, path, "", nil); code != http.StatusForbidden {
			t.Errorf("%s %s with no credential answered %d %s, want 403", r.method, path, code, body)
		}
	}
}
