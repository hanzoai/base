package org

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/hanzoai/base/core"
	"github.com/hanzoai/base/tests"
	"github.com/hanzoai/base/tools/logger"
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

// TestAKeyActsInItsOwnOrg pins the other credential that opens these routes.
//
// A service fetching the provider credentials of the org it serves does it with
// an IAM key, which resolves to one org and no more. The rule reads the org the
// request acts in and never asks which door set it, so the key path needs no
// second statement of it — this is the test that the one statement covers both.
//
// It used to assert that a key reading its own org's stripe credentials was
// handed STRIPE_API_KEY from the process environment, and called that a key
// reaching its own org. It was the deployment's key, the same one for every
// org, and the test pinned the leak as the feature. What the key reaches now is
// its own org's KMS, which holds nothing here, so the read is a refusal to
// answer and never the platform's secret.
func TestAKeyActsInItsOwnOrg(t *testing.T) {
	t.Setenv("STRIPE_API_KEY", "the-deployment-key")

	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatal(err)
	}
	defer app.Cleanup()

	iam := newIssuer(t)

	// The half of IAM a key resolves against, beside the JWKS one.
	keys := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok","data":{"name":"keyuser","email":"k@example.test","owner":"alpha"}}`))
	}))
	defer keys.Close()

	if err := Register(app, Config{IAMEndpoint: keys.URL, KMSEndpoint: "127.0.0.1:1",
		IAMClientID: "svc", IAMClientSecret: "shh"}); err != nil {
		t.Fatal(err)
	}
	app.Store().Set("jwksURL", iam.url+"/v1/iam/.well-known/jwks")

	db := NewOrgDB(app, "")
	for _, org := range []string{"alpha", "beta"} {
		if _, err := db.ProvisionOrg(org); err != nil {
			t.Fatal(err)
		}
	}
	mux := serve(t, app)

	// Its own org is reached — not refused — and answers that it holds no such
	// credential rather than handing over the deployment's.
	code, body := call(t, mux, http.MethodGet, "/v1/bases/alpha/creds/stripe", "sk-service", nil)
	if code == http.StatusForbidden {
		t.Fatalf("a key was refused its own org: %d %s", code, body)
	}
	if strings.Contains(body, "the-deployment-key") {
		t.Fatalf("a key was handed the deployment's own credential: %d %s", code, body)
	}

	for _, r := range orgRoutes {
		path := strings.Replace(r.path, "%s", "beta", 1)
		code, body := call(t, mux, r.method, path, "sk-service", nil)
		if code != http.StatusForbidden {
			t.Errorf("%s %s with an alpha key answered %d %s, want 403", r.method, path, code, body)
		}
		if strings.Contains(body, "the-deployment-key") {
			t.Errorf("%s %s leaked a credential: %s", r.method, path, body)
		}
	}
}

// keyed stands up the half of IAM a key resolves against, so a test can issue
// one. Every key it answers for belongs to owner.
func keyed(t *testing.T, owner string, extra ...func(*core.ServeEvent)) (core.App, http.Handler) {
	t.Helper()

	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(app.Cleanup)

	iam := newIssuer(t)
	keys := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok","data":{"name":"keyuser","email":"k@example.test","owner":"` + owner + `"}}`))
	}))
	t.Cleanup(keys.Close)

	if err := Register(app, Config{IAMEndpoint: keys.URL, KMSEndpoint: "127.0.0.1:1",
		IAMClientID: "svc", IAMClientSecret: "shh"}); err != nil {
		t.Fatal(err)
	}
	app.Store().Set("jwksURL", iam.url+"/v1/iam/.well-known/jwks")

	db := NewOrgDB(app, "")
	for _, org := range []string{"alpha", "beta"} {
		if _, err := db.ProvisionOrg(org); err != nil {
			t.Fatal(err)
		}
	}

	return app, serve(t, app, extra...)
}

// seedOrgConfig puts a real config row in, so a read that is admitted has
// something to hand back and the test measures a disclosure rather than a
// status code.
func seedOrgConfig(t *testing.T, app core.App, org, secret string) {
	t.Helper()

	if err := app.Bootstrap(); err != nil {
		t.Fatal(err)
	}

	c, err := app.FindCollectionByNameOrId(collectionOrgConfigs)
	if err != nil {
		t.Fatal(err)
	}

	r := core.NewRecord(c)
	r.Set("org_id", org)
	r.Set("display_name", org+" Inc")
	r.Set("kms_project_id", secret)
	if err := app.Save(r); err != nil {
		t.Fatal(err)
	}
}

// TestAPublishableKeyReachesNoBase pins what publishable means.
//
// A pk- key is the one that goes in a web page, and it travels in ?key=, so
// reaching a Base with it needs no Authorization header — the page source IS
// the credential. The only thing that stood in its way was a check that a
// publishable key may not WRITE, and every read here is a GET, so the key
// printed in a customer's HTML returned that org's provider secrets and every
// member's billing identity. Read-only was the wrong reading of publishable.
func TestAPublishableKeyReachesNoBase(t *testing.T) {
	app, mux := keyed(t, "alpha", func(e *core.ServeEvent) {
		// An address this file does not publish, registered the way an
		// extension does it. What a publishable key reaches is a property of
		// the prefix, so it holds for whatever route sits under it.
		e.Router.GET("/v1/bases/{orgId}/usage", func(re *core.RequestEvent) error {
			return re.JSON(http.StatusOK, map[string]string{"secret": "alpha-kms-project"})
		})
	})
	seedOrgConfig(t, app, "alpha", "alpha-kms-project")

	for _, r := range orgRoutes {
		// Its OWN org, which is the point: the org rule is not what refuses it.
		path := strings.Replace(r.path, "%s", "alpha", 1)

		for _, how := range []struct{ name, token, query string }{
			{"as a bearer", "pk-in-the-page", ""},
			{"in the query", "", "?key=pk-in-the-page"},
		} {
			code, body := call(t, mux, r.method, path+how.query, how.token, nil)
			if code != http.StatusForbidden {
				t.Errorf("%s %s %s answered %d %s, want 403", r.method, path, how.name, code, body)
			}
			if strings.Contains(body, "alpha-kms-project") {
				t.Errorf("%s %s %s handed a web page's key the org's config: %s", r.method, path, how.name, body)
			}
		}
	}

	for _, how := range []struct{ name, token, query string }{
		{"as a bearer", "pk-in-the-page", ""},
		{"in the query", "", "?key=pk-in-the-page"},
	} {
		code, body := call(t, mux, http.MethodGet, "/v1/bases/alpha/usage"+how.query, how.token, nil)
		if code != http.StatusForbidden {
			t.Errorf("a route registered off the router under /v1/bases answered a publishable key %s: %d %s", how.name, code, body)
		}
		if strings.Contains(body, "alpha-kms-project") {
			t.Errorf("that route handed a web page's key the org's config %s: %s", how.name, body)
		}
	}

	// The collection root too, which names no org and so has no wildcard in its
	// address. What a publishable key reaches is a property of the prefix and
	// not of any one segment in it.
	if code, body := call(t, mux, http.MethodGet, basesPath, "pk-in-the-page", nil); code != http.StatusForbidden {
		t.Errorf("the collection root answered a publishable key: %d %s", code, body)
	}

	// And it is refused by address rather than by being a pk- key at all: the
	// same key still reaches what is not a Base.
	if code, body := call(t, mux, http.MethodGet, "/v1/health", "pk-in-the-page", nil); code == http.StatusForbidden {
		t.Errorf("a publishable key was refused something that is not a Base: %s", body)
	}
}

// TestAMemberReachesOnlyItsOwnCustomerRow pins the second half of the customer
// route, which compared orgs and said nothing whatever about users.
//
// Belonging to an org is not the same fact as being a person in it. The row
// holds a customer id, a broker account, a commerce customer and a vault id, so
// an ordinary member reading a colleague's is a real disclosure inside a tenant
// that the org boundary cannot see.
func TestAMemberReachesOnlyItsOwnCustomerRow(t *testing.T) {
	app, iam, mux, _ := twoOrgs(t)
	if err := app.Bootstrap(); err != nil {
		t.Fatal(err)
	}

	ann := iam.member(t, "alpha/ann", "alpha")

	code, body := call(t, mux, http.MethodGet, "/v1/bases/alpha/customers/alpha%2Fceo", ann, nil)
	if code != http.StatusForbidden {
		t.Errorf("a member read another member's row: %d %s", code, body)
	}
	code, body = call(t, mux, http.MethodPost, "/v1/bases/alpha/customers/alpha%2Fceo", ann, nil)
	if code != http.StatusForbidden {
		t.Errorf("a member provisioned another member's row: %d %s", code, body)
	}

	// Its own row is its own business.
	if code, body := call(t, mux, http.MethodPost, "/v1/bases/alpha/customers/alpha%2Fann", ann, nil); code == http.StatusForbidden {
		t.Errorf("a member was refused its own row: %s", body)
	}

	// An org's owner acts for the org and reaches every member's.
	owner := iam.token(t, "alpha/boss", "alpha")
	if code, body := call(t, mux, http.MethodGet, "/v1/bases/alpha/customers/alpha%2Fann", owner, nil); code == http.StatusForbidden {
		t.Errorf("an org owner was refused a member's row: %s", body)
	}
}

// TestTheRuleFollowsTheAddressAndNotTheGroup pins that what defends
// /v1/bases/{org} is the address and not the subtree it happened to be
// registered under.
//
// tools/router inherits middleware down the GROUP tree, not the URL. A rule
// stated on the group that declares these seven routes therefore covers those
// seven and nothing else, so a route registered off the router at the identical
// address carried none of it — and jsvm's routerAdd takes an arbitrary path
// string from an extension with no validation at all, which is exactly that
// registration.
func TestTheRuleFollowsTheAddressAndNotTheGroup(t *testing.T) {
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(app.Cleanup)

	iam := newIssuer(t)
	if err := Register(app, Config{IAMEndpoint: iam.url, KMSEndpoint: "127.0.0.1:1"}); err != nil {
		t.Fatal(err)
	}

	db := NewOrgDB(app, "")
	for _, org := range []string{"alpha", "beta"} {
		if _, err := db.ProvisionOrg(org); err != nil {
			t.Fatal(err)
		}
	}

	// Two routes at addresses this file does not publish, registered the way an
	// extension does it. The second continues past a user, which is a shape
	// only an extension writes.
	echo := func(re *core.RequestEvent) error {
		return re.JSON(http.StatusOK, map[string]string{"org": re.Request.PathValue("orgId")})
	}
	mux := serve(t, app, func(e *core.ServeEvent) {
		e.Router.GET("/v1/bases/{orgId}/usage", echo)
		e.Router.GET("/v1/bases/{orgId}/customers/{userId}/notes", echo)
	})

	alpha := iam.token(t, "alpha/ann", "alpha")

	if code, body := call(t, mux, http.MethodGet, "/v1/bases/beta/usage", alpha, nil); code != http.StatusForbidden {
		t.Errorf("a route registered off the router at a /v1/bases address answered %d %s, want 403", code, body)
	}
	if code, body := call(t, mux, http.MethodGet, "/v1/bases/alpha/usage", alpha, nil); code == http.StatusForbidden {
		t.Errorf("the same route refused alpha its own org: %s", body)
	}

	// The user a path names is the caller, at whatever depth the address
	// continues past it.
	ann := iam.member(t, "alpha/ann", "alpha")

	if code, body := call(t, mux, http.MethodGet, "/v1/bases/alpha/customers/alpha%2Fceo/notes", ann, nil); code != http.StatusForbidden {
		t.Errorf("a member reached another member through a deeper address: %d %s", code, body)
	}
	if code, body := call(t, mux, http.MethodGet, "/v1/bases/alpha/customers/alpha%2Fann/notes", ann, nil); code == http.StatusForbidden {
		t.Errorf("the same address refused a member itself: %s", body)
	}
}

// requestLog waits for the row a served request writes and hands it back.
//
// The write is handed to a goroutine and then held by a batching handler, so it
// is neither synchronous with the response nor written on the first flush.
func requestLog(t *testing.T, app core.App) *core.Log {
	t.Helper()

	h, ok := app.SlogLogger().Handler().(*logger.BatchHandler)
	if !ok {
		t.Fatal("the app's log handler does not batch, so there is nothing to flush")
	}

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if err := h.WriteAll(context.Background()); err != nil {
			t.Fatal(err)
		}

		logs := []*core.Log{}
		if err := app.LogQuery().All(&logs); err != nil {
			t.Fatal(err)
		}
		for _, l := range logs {
			if l.Data["type"] == "request" {
				return l
			}
		}

		time.Sleep(10 * time.Millisecond)
	}

	return nil
}

// TestTheAuditLogIsTheOperatorsAndCarriesNoSecret pins three things about one
// row, all of which were wrong at once.
//
// The row belongs on the Base the process serves from. logRequest read
// e.App, which a credential naming an org has already moved to that tenant's
// file, so an authenticated request logged itself into the tenant's Base and
// the operator's log held only traffic that never authenticated.
//
// It carries no credential. A key arrives in ?key= on a request with no
// Authorization header, and the row recorded RequestURI() whole — so a live
// secret key sat in _logs in plaintext for Logs.MaxDays, was served by
// GET /v1/logs, and went into every backup.
//
// And it names who acted. A key mints no auth record, so every keyed action was
// attributed to nobody at all.
func TestTheAuditLogIsTheOperatorsAndCarriesNoSecret(t *testing.T) {
	app, mux := keyed(t, "alpha")

	s := app.Settings()
	s.Logs.MaxDays = 5
	s.Logs.LogAuthId = true
	if err := app.Save(s); err != nil {
		t.Fatal(err)
	}

	const secret = "sk-live-never-in-a-log"
	if code, body := call(t, mux, http.MethodGet, "/v1/health?key="+secret, "", nil); code != http.StatusOK {
		t.Fatalf("the keyed request did not reach /v1/health: %d %s", code, body)
	}

	row := requestLog(t, app)
	if row == nil {
		t.Fatal("the deployment's log holds no row for a request its tenant served")
	}

	if url, _ := row.Data["url"].(string); strings.Contains(url, secret) {
		t.Errorf("a live key was written to the log: %s", url)
	}
	if auth, _ := row.Data["auth"].(string); auth != "key" {
		t.Errorf("a keyed request was attributed to %q", auth)
	}
	if id, _ := row.Data["authId"].(string); id != "alpha/keyuser" {
		t.Errorf("a keyed request named the subject %q", id)
	}
}

// TestTheRuleReadsTheAddressTheRouterMatched pins the rule and the handler to
// ONE reading of the path.
//
// http.ServeMux matches by splitting the escaped path on "/" and unescaping
// each segment, so every spelling below reaches the same handler with the same
// orgId. A rule that compares the escaped string against a literal prefix reads
// a different address than the router did, and the handler that runs is the
// router's — so the rule reads what the router matched: the pattern it chose
// and the values it filled.
func TestTheRuleReadsTheAddressTheRouterMatched(t *testing.T) {
	app, iam, mux, _ := twoOrgs(t)
	seedOrgConfig(t, app, "alpha", "alpha-kms-project")
	seedOrgConfig(t, app, "beta", "beta-kms-project")

	alpha := iam.token(t, "alpha/ann", "alpha")
	ann := iam.member(t, "alpha/ann", "alpha")

	// Every spelling of an address that routes to beta's config or beta's
	// provider credentials, and what each must answer.
	for _, c := range []struct{ name, path, token string }{
		{"an encoded segment, uncredentialed", "/v1/%62ases/beta/config", ""},
		{"an encoded prefix, uncredentialed", "/%76%31/%62%61%73%65%73/beta/config", ""},
		{"an encoded segment, credentialed elsewhere", "/v1/%62ases/beta/config", alpha},
		{"credentials by an encoded segment", "/v1/%62ases/beta/creds/stripe", ""},
		{"credentials spelled plainly", "/v1/bases/beta/creds/stripe", ""},
		{"the config spelled plainly", "/v1/bases/beta/config", ""},
	} {
		code, body := call(t, mux, http.MethodGet, c.path, c.token, nil)
		if code != http.StatusForbidden {
			t.Errorf("%s: GET %s answered %d %s, want 403", c.name, c.path, code, body)
		}
		if strings.Contains(body, "beta-kms-project") {
			t.Errorf("%s: GET %s handed over beta's config: %s", c.name, c.path, body)
		}
	}

	// A path that climbs reaches no org but its own. The router canonicalises
	// one spelling before it matches and matches nothing at all on the other,
	// so neither becomes a read of beta.
	for _, path := range []string{
		"/v1/bases/alpha/../beta/config",
		"/v1/bases/alpha/%2e%2e/beta/config",
	} {
		code, body := call(t, mux, http.MethodGet, path, alpha, nil)
		if code == http.StatusOK || strings.Contains(body, "beta-kms-project") {
			t.Errorf("GET %s reached beta: %d %s", path, code, body)
		}
	}

	// The member rule holds through an encoded address too: a user segment
	// names a person however the prefix in front of it is spelled.
	if code, body := call(t, mux, http.MethodGet,
		"/v1/%62ases/alpha/customers/alpha%2Fceo", ann, nil); code != http.StatusForbidden {
		t.Errorf("a member read another member's row through an encoded address: %d %s", code, body)
	}

	// And the org it does act in still answers, spelled either way.
	for _, path := range []string{"/v1/bases/alpha/config", "/v1/%62ases/alpha/config"} {
		code, body := call(t, mux, http.MethodGet, path, alpha, nil)
		if code != http.StatusOK {
			t.Errorf("GET %s refused alpha its own config: %d %s", path, code, body)
		}
		if !strings.Contains(body, "alpha-kms-project") {
			t.Errorf("GET %s did not answer alpha its own config: %s", path, body)
		}
	}
}

// publicForm puts a collection anyone may create in into one org's Base, the
// way a contact form or a signup list is declared.
func publicForm(t *testing.T, dataDir, collection string) {
	t.Helper()

	app := core.NewBaseApp(core.BaseAppConfig{DataDir: dataDir})
	if err := app.Bootstrap(); err != nil {
		t.Fatal(err)
	}
	defer app.ResetBootstrapState()

	open := ""
	c := core.NewBaseCollection(collection)
	c.CreateRule = &open
	c.Fields.Add(&core.TextField{Name: "email"})
	if err := app.Save(c); err != nil {
		t.Fatal(err)
	}
}

// TestAPublishableKeyWritesOnlyAtTheCreateRoute pins the one write a publishable
// key is allowed, and the address that allows it.
//
// The exception is the public submit path — a collection whose createRule
// admits anyone — and what makes a request that path is the route the router
// matched. A string that merely appears somewhere in a URL is not an address,
// and granting the exception on one hands the key whatever the router did
// match.
func TestAPublishableKeyWritesOnlyAtTheCreateRoute(t *testing.T) {
	app, mux := keyed(t, "alpha", func(e *core.ServeEvent) {
		// A POST at an address that is not the create route, registered the way
		// an extension does it.
		e.Router.POST("/v1/notes/{note}", func(re *core.RequestEvent) error {
			return re.JSON(http.StatusOK, map[string]string{"wrote": re.Request.PathValue("note")})
		})
	})
	publicForm(t, NewOrgDB(app, "").OrgDir("alpha"), "signups")

	if code, body := call(t, mux, http.MethodPost, "/v1/collections/signups/records",
		"pk-in-the-page", nil); code == http.StatusForbidden {
		t.Errorf("a publishable key was refused the public submit path: %d %s", code, body)
	}

	if code, body := call(t, mux, http.MethodPost, "/v1/notes/..%2Fcollections%2Fsignups%2Frecords",
		"pk-in-the-page", nil); code != http.StatusForbidden {
		t.Errorf("a publishable key wrote at a route that is not the create route: %d %s", code, body)
	}
}
