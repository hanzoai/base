package org

import (
	"net/http"
	"testing"

	"github.com/hanzoai/base/core"
	"github.com/hanzoai/base/tests"
)

// TestTheLimitIsTheProcessAndReachesTenants pins whose rate limit a request is
// held to.
//
// The limiter runs after the request has moved onto the Base its credential
// names, so reading the limit off that Base asked each tenant how hard the
// process may be hit — and a Base opened moments ago answers "no limit". The
// rule below is audience @auth, so only an authenticated caller can match it,
// and an authenticated caller is exactly the one that was exempt.
func TestTheLimitIsTheProcessAndReachesTenants(t *testing.T) {
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatal(err)
	}
	defer app.Cleanup()

	iam := newIssuer(t)
	if err := Register(app, Config{IAMEndpoint: iam.url}); err != nil {
		t.Fatal(err)
	}

	s := app.Settings()
	s.RateLimits.Enabled = true
	s.RateLimits.Rules = []core.RateLimitRule{{
		Label:       "/v1/health",
		MaxRequests: 2,
		Duration:    60,
		Audience:    core.RateLimitRuleAudienceAuth,
	}}
	if err := app.Save(s); err != nil {
		t.Fatal(err)
	}

	db := NewOrgDB(app, "")
	if _, err := db.ProvisionOrg("alpha"); err != nil {
		t.Fatal(err)
	}

	mux := serve(t, app)
	alpha := iam.token(t, "alpha/ann", "alpha")

	limited := 0
	for i := 0; i < 10; i++ {
		if code, _ := call(t, mux, http.MethodGet, "/v1/health", alpha, nil); code == http.StatusTooManyRequests {
			limited++
		}
	}
	if limited != 8 {
		t.Fatalf("a tenant's authenticated traffic was limited %d times in 10, want 8", limited)
	}

	// The tenant's own Base still says the limiter is off, which is what makes
	// the assertion above about where the answer is read rather than about
	// settings happening to agree.
	tenant, err := (&bases{p: &plugin{app: app, orgDB: db}, open: map[string]core.App{}}).base("alpha")
	if err != nil {
		t.Fatal(err)
	}
	defer tenant.ResetBootstrapState()
	if tenant.Settings().RateLimits.Enabled {
		t.Fatal("the tenant Base carries the limit itself, so this proves nothing")
	}
}

// TestTwoTenantsBehindOneIngressDoNotShareOneBucket pins WHOSE bucket a request
// is counted in, which is a different question from whose rule it is held to.
//
// The rule and the counters moved to the deployment; the key did not. RealIP
// asks which proxy headers to believe, and it asked e.App — the tenant's Base
// by then, whose TrustedProxy is empty and can never be anything else, because
// only a superuser writes settings and a tenant's Base has none. So behind an
// ingress every authenticated request fell back to the socket peer, which is
// the ingress for all of them: one bucket for the estate, and any one tenant
// could spend the whole budget and lock out every other.
//
// Both callers here arrive on the same socket, which is what an ingress looks
// like, and are told apart only by the forwarded header the DEPLOYMENT trusts.
func TestTwoTenantsBehindOneIngressDoNotShareOneBucket(t *testing.T) {
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatal(err)
	}
	defer app.Cleanup()

	iam := newIssuer(t)
	if err := Register(app, Config{IAMEndpoint: iam.url}); err != nil {
		t.Fatal(err)
	}

	s := app.Settings()
	s.TrustedProxy.Headers = []string{"X-Forwarded-For"}
	s.RateLimits.Enabled = true
	s.RateLimits.Rules = []core.RateLimitRule{{
		Label:       "/v1/health",
		MaxRequests: 3,
		Duration:    60,
		Audience:    core.RateLimitRuleAudienceAuth,
	}}
	if err := app.Save(s); err != nil {
		t.Fatal(err)
	}

	db := NewOrgDB(app, "")
	for _, org := range []string{"alpha", "beta"} {
		if _, err := db.ProvisionOrg(org); err != nil {
			t.Fatal(err)
		}
	}

	mux := serve(t, app)

	spend := func(token, client string, n int) (refused int) {
		for i := 0; i < n; i++ {
			code, _ := call(t, mux, http.MethodGet, "/v1/health", token,
				map[string]string{"X-Forwarded-For": client})
			if code == http.StatusTooManyRequests {
				refused++
			}
		}
		return refused
	}

	// alpha spends its whole budget and is then refused, which is the rule
	// working.
	if refused := spend(iam.token(t, "alpha/ann", "alpha"), "203.0.113.10", 3); refused != 0 {
		t.Fatalf("alpha was refused %d of its first 3 requests", refused)
	}
	if refused := spend(iam.token(t, "alpha/ann", "alpha"), "203.0.113.10", 1); refused != 1 {
		t.Fatal("alpha was not held to a budget it had spent")
	}

	// beta arrives from a different address and has spent nothing, so its own
	// budget is whole.
	if refused := spend(iam.token(t, "beta/bob", "beta"), "203.0.113.20", 3); refused != 0 {
		t.Fatalf("beta was refused %d of 3 requests on a budget alpha spent", refused)
	}
}
