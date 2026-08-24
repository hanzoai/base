package org

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/hanzoai/base/apis"
	"github.com/hanzoai/base/core"
	"github.com/hanzoai/base/tests"
)

// issuer is the half of Hanzo IAM a Base actually depends on: a JWKS to verify
// against, and tokens signed by the matching key.
type issuer struct {
	url string
	key *rsa.PrivateKey
}

func newIssuer(t *testing.T) *issuer {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}

	jwks := map[string]any{"keys": []map[string]any{{
		"kty": "RSA",
		"kid": "iam-test",
		"alg": "RS256",
		"use": "sig",
		"n":   base64.RawURLEncoding.EncodeToString(key.PublicKey.N.Bytes()),
		"e":   base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.PublicKey.E)).Bytes()),
	}}}

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/iam/.well-known/jwks" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(jwks)
	}))
	t.Cleanup(ts.Close)

	return &issuer{url: ts.URL, key: key}
}

// token signs what IAM signs for a person who owns the orgs it belongs to: a
// subject and the membership set, home org first.
func (i *issuer) token(t *testing.T, sub string, orgs ...string) string {
	t.Helper()

	return i.as(t, "owner", sub, orgs...)
}

// member signs the same for an ordinary member, whose role admits reads and no
// authority over anybody else in the org.
func (i *issuer) member(t *testing.T, sub string, orgs ...string) string {
	t.Helper()

	return i.as(t, "member", sub, orgs...)
}

// as signs for a subject spelled the way IAM spells an account, <owner>/<name>,
// so the username is the half after the slash.
func (i *issuer) as(t *testing.T, role, sub string, orgs ...string) string {
	t.Helper()

	_, name, _ := strings.Cut(sub, "/")

	return i.signed(t, role, sub, name, orgs...)
}

// signed is the one place a token with a USERNAME is built. IAM puts the
// account's stable id in `sub` and its username in `name` — two claims, because
// the id it issues an account is not the name it files that account under.
func (i *issuer) signed(t *testing.T, role, sub, name string, orgs ...string) string {
	t.Helper()

	p := payload(role, sub, orgs...)
	p["name"] = name

	return i.sign(t, p)
}

// display signs for an account whose token carries no username at all, only the
// free-form text a UI shows.
func (i *issuer) display(t *testing.T, role, sub, displayName string, orgs ...string) string {
	t.Helper()

	p := payload(role, sub, orgs...)
	p["displayName"] = displayName

	return i.sign(t, p)
}

// payload is what IAM signs about a person, apart from the names: a subject,
// and the membership set with the role it holds in each.
func payload(role, sub string, orgs ...string) jwt.MapClaims {
	memberships := make([]any, 0, len(orgs))
	for _, o := range orgs {
		memberships = append(memberships, map[string]any{"org": o, "role": role})
	}

	return jwt.MapClaims{
		"sub":   sub,
		"email": sub + "@example.test",
		"owner": "hanzo", // the org of the APP the token was minted through
		"orgs":  memberships,
		"iat":   time.Now().Unix(),
		"exp":   time.Now().Add(time.Hour).Unix(),
	}
}

// sign puts the issuer's signature on a claim set.
func (i *issuer) sign(t *testing.T, p jwt.MapClaims) string {
	t.Helper()

	jt := jwt.NewWithClaims(jwt.SigningMethodRS256, p)
	jt.Header["kid"] = "iam-test"

	signed, err := jt.SignedString(i.key)
	if err != nil {
		t.Fatal(err)
	}
	return signed
}

// seed puts one collection into one org's Base, by opening that Base the way
// anything opens a Base. It is deliberately not routed through the request path
// under test: the point is to write a fact to a file and then ask whether the
// request path can read it.
func seed(t *testing.T, dataDir, collection string) {
	t.Helper()

	app := core.NewBaseApp(core.BaseAppConfig{DataDir: dataDir})
	if err := app.Bootstrap(); err != nil {
		t.Fatal(err)
	}
	defer app.ResetBootstrapState()

	c := core.NewBaseCollection(collection)
	rule := "@request.auth.id != ''"
	c.ListRule = &rule
	c.ViewRule = &rule
	c.Fields.Add(&core.TextField{Name: "title"})
	if err := app.Save(c); err != nil {
		t.Fatal(err)
	}

	r := core.NewRecord(c)
	r.Set("title", collection+" is here")
	if err := app.Save(r); err != nil {
		t.Fatal(err)
	}
}

// serve wires the app the way a running Base does and returns something to
// issue requests against. Anything passed in gets to add routes of its own,
// after the plugin has bound its middleware.
func serve(t *testing.T, app core.App, extra ...func(*core.ServeEvent)) http.Handler {
	t.Helper()

	r, err := apis.NewRouter(app)
	if err != nil {
		t.Fatal(err)
	}

	e := new(core.ServeEvent)
	e.App = app
	e.Router = r
	if err := app.OnServe().Trigger(e, func(*core.ServeEvent) error { return nil }); err != nil {
		t.Fatal(err)
	}
	for _, f := range extra {
		f(e)
	}

	mux, err := e.Router.BuildMux()
	if err != nil {
		t.Fatal(err)
	}
	return mux
}

func get(t *testing.T, mux http.Handler, path, token string, headers map[string]string) (int, string) {
	t.Helper()

	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}

// TestEachOrgReadsItsOwnBase is the test the bug survived for want of.
//
// Two tokens, two orgs, one process, one URL. Each token must reach its own
// org's Base and no other. Before this, every read landed on the shared
// platform Base whatever org the token carried — so both tokens answered
// identically, and the per-org files on disk were written by nothing and read
// by no one.
func TestEachOrgReadsItsOwnBase(t *testing.T) {
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatal(err)
	}
	defer app.Cleanup()

	iam := newIssuer(t)
	if err := Register(app, Config{IAMEndpoint: iam.url}); err != nil {
		t.Fatal(err)
	}

	db := NewOrgDB(app, "")
	for _, org := range []string{"alpha", "beta"} {
		if _, err := db.ProvisionOrg(org); err != nil {
			t.Fatal(err)
		}
		seed(t, db.OrgDir(org), org+"_notes")
	}

	mux := serve(t, app)

	alpha := iam.token(t, "alpha/ann", "alpha")
	beta := iam.token(t, "beta/bob", "beta")

	// Its own collection, which exists only in its own Base.
	if code, body := get(t, mux, "/v1/collections/alpha_notes/records", alpha, nil); code != 200 ||
		!strings.Contains(body, "alpha_notes is here") {
		t.Fatalf("alpha could not read its own Base: %d %s", code, body)
	}
	if code, body := get(t, mux, "/v1/collections/beta_notes/records", beta, nil); code != 200 ||
		!strings.Contains(body, "beta_notes is here") {
		t.Fatalf("beta could not read its own Base: %d %s", code, body)
	}

	// And nothing of the other's. A 404 is the honest answer: in beta's Base
	// there is no such collection.
	if code, body := get(t, mux, "/v1/collections/beta_notes/records", alpha, nil); code != 404 {
		t.Fatalf("alpha reached beta's collection: %d %s", code, body)
	}
	if code, body := get(t, mux, "/v1/collections/alpha_notes/records", beta, nil); code != 404 {
		t.Fatalf("beta reached alpha's collection: %d %s", code, body)
	}
}

// TestNamingAnotherOrgIsRefused pins the answer a caller gets when it names an
// org its token does not carry.
//
// It is a refusal, not an empty list. An empty list reads as "this Base is
// empty" and is a different, false statement — and it is the one the caller
// would have acted on.
func TestNamingAnotherOrgIsRefused(t *testing.T) {
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatal(err)
	}
	defer app.Cleanup()

	iam := newIssuer(t)
	if err := Register(app, Config{IAMEndpoint: iam.url}); err != nil {
		t.Fatal(err)
	}

	db := NewOrgDB(app, "")
	for _, org := range []string{"alpha", "beta"} {
		if _, err := db.ProvisionOrg(org); err != nil {
			t.Fatal(err)
		}
		seed(t, db.OrgDir(org), org+"_notes")
	}

	mux := serve(t, app)
	alpha := iam.token(t, "alpha/ann", "alpha")

	code, body := get(t, mux, "/v1/collections/beta_notes/records", alpha,
		map[string]string{"X-Org-Id": "beta"})
	if code != http.StatusForbidden {
		t.Fatalf("naming beta with an alpha token answered %d %s, want 403", code, body)
	}

	// The same header naming the caller's own org changes nothing: the token
	// already said so.
	if code, body := get(t, mux, "/v1/collections/alpha_notes/records", alpha,
		map[string]string{"X-Org-Id": "alpha"}); code != 200 ||
		!strings.Contains(body, "alpha_notes is here") {
		t.Fatalf("alpha naming alpha answered %d %s", code, body)
	}
}

// TestClientIdentityHeadersDoNotSurvive pins what a handler sees when a client
// sends the identity headers itself.
//
// It sees the token's answer or nothing. The headers are the gateway's to write
// from a token it validated; inbound they are a client saying who it is, which
// is the one thing a caller does not get to say.
func TestClientIdentityHeadersDoNotSurvive(t *testing.T) {
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatal(err)
	}
	defer app.Cleanup()

	iam := newIssuer(t)
	if err := Register(app, Config{IAMEndpoint: iam.url}); err != nil {
		t.Fatal(err)
	}

	seen := func(e *core.ServeEvent) {
		e.Router.GET("/probe", func(re *core.RequestEvent) error {
			return re.JSON(http.StatusOK, map[string]string{
				"org":   re.Request.Header.Get("X-Org-Id"),
				"user":  re.Request.Header.Get("X-User-Id"),
				"email": re.Request.Header.Get("X-User-Email"),
				"admin": re.Request.Header.Get("X-User-IsAdmin"),
			})
		})
	}

	db := NewOrgDB(app, "")
	if _, err := db.ProvisionOrg("alpha"); err != nil {
		t.Fatal(err)
	}
	seed(t, db.OrgDir("alpha"), "alpha_notes")

	mux := serve(t, app, seen)

	forged := map[string]string{
		"X-Org-Id":           "alpha",
		"X-User-Id":          "root",
		"X-User-Email":       "root@example.test",
		"X-User-IsAdmin":     "true",
		"X-Hanzo-Cleverness": "1",
	}

	// With no token at all nothing replaces them, so the handler sees nothing.
	req := httptest.NewRequest(http.MethodGet, "/probe", nil)
	for k, v := range forged {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if body := rec.Body.String(); strings.Contains(body, "root") || strings.Contains(body, "alpha") {
		t.Fatalf("a client's own identity reached the handler: %s", body)
	}

	// With a token, what the handler sees is the token's, not the client's.
	code, body := get(t, mux, "/probe", iam.token(t, "alpha/ann", "alpha"), forged)
	if code != 200 {
		t.Fatalf("probe answered %d %s", code, body)
	}
	if strings.Contains(body, "root") || strings.Contains(body, `"admin":"true"`) {
		t.Fatalf("a client's own identity survived beside the token's: %s", body)
	}
	if !strings.Contains(body, `"org":"alpha"`) || !strings.Contains(body, `"user":"alpha/ann"`) {
		t.Fatalf("the token's identity did not reach the handler: %s", body)
	}
}
