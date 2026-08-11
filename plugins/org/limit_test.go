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
