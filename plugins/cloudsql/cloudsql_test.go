package cloudsql

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/hanzoai/base/apis"
	"github.com/hanzoai/base/core"
	"github.com/hanzoai/base/tests"
)

// adminPassword opens every database on the server. It is what the plugin is
// configured with, and no request may be answered on it.
const adminPassword = "server-admin-password"

// upstream stands in for postgres-meta and keeps the connection each request
// arrived on, which is the thing worth asserting about: postgres-meta runs
// whatever it is handed, inside whatever database the connection opens.
type upstream struct {
	mu   sync.Mutex
	seen []string
	url  string
}

func (u *upstream) connections() []string {
	u.mu.Lock()
	defer u.mu.Unlock()
	return append([]string(nil), u.seen...)
}

func newUpstream(t *testing.T) *upstream {
	t.Helper()

	u := &upstream{}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u.mu.Lock()
		u.seen = append(u.seen, r.Header.Get("X-Connection-Encrypted"))
		u.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"upstream":"reached"}`))
	}))
	t.Cleanup(ts.Close)
	u.url = ts.URL

	return u
}

// iam is the half of Hanzo IAM a Base depends on: a JWKS to verify against and
// tokens signed by the matching key.
type iam struct {
	url string
	key *rsa.PrivateKey
}

func newIAM(t *testing.T) *iam {
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
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(jwks)
	}))
	t.Cleanup(ts.Close)

	return &iam{url: ts.URL, key: key}
}

func (i *iam) token(t *testing.T, sub, org string) string {
	t.Helper()

	claims := jwt.MapClaims{
		"sub":   sub,
		"name":  sub,
		"email": sub + "@example.test",
		"owner": "hanzo",
		"orgs":  []any{map[string]any{"org": org, "role": "admin"}},
		"iat":   time.Now().Unix(),
		"exp":   time.Now().Add(time.Hour).Unix(),
	}

	jt := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	jt.Header["kid"] = "iam-test"

	signed, err := jt.SignedString(i.key)
	if err != nil {
		t.Fatal(err)
	}
	return signed
}

// stand brings up one process holding two orgs' databases, the way a deployment
// does, and hands back what is needed to ask it questions.
func stand(t *testing.T) (*iam, http.Handler, *upstream, map[string]string) {
	t.Helper()

	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(app.Cleanup)

	meta := newUpstream(t)
	if err := Register(app, Config{
		MetaURL:       meta.url,
		ComputeHost:   "cloud-sql.test",
		DefaultPGUser: "hanzo",
		DefaultPGPass: adminPassword,
	}); err != nil {
		t.Fatal(err)
	}

	// The collection the plugin creates at boot, created the way boot creates it.
	if err := app.OnBootstrap().Trigger(
		&core.BootstrapEvent{App: app},
		func(*core.BootstrapEvent) error { return nil },
	); err != nil {
		t.Fatal(err)
	}

	col, err := app.FindCollectionByNameOrId(collectionCloudSQLDBs)
	if err != nil {
		t.Fatal(err)
	}

	ids := map[string]string{}
	for _, org := range []string{"alpha", "beta"} {
		r := core.NewRecord(col)
		r.Set("orgId", org)
		r.Set("databaseName", "t_"+org)
		r.Set("host", "cloud-sql.test")
		r.Set("port", 5432)
		r.Set("pgUser", org+"_user")
		r.Set("pgPassword", org+"-password")
		r.Set("sslMode", "require")
		r.Set("status", "ready")
		if err := app.Save(r); err != nil {
			t.Fatal(err)
		}
		ids[org] = r.Id
	}

	i := newIAM(t)
	app.Store().Set(apis.StoreKeyExternalAuthOnly, true)
	app.Store().Set(apis.StoreKeyJWKSURL, i.url)

	router, err := apis.NewRouter(app)
	if err != nil {
		t.Fatal(err)
	}
	e := new(core.ServeEvent)
	e.App = app
	e.Router = router
	if err := app.OnServe().Trigger(e, func(*core.ServeEvent) error { return nil }); err != nil {
		t.Fatal(err)
	}
	mux, err := e.Router.BuildMux()
	if err != nil {
		t.Fatal(err)
	}

	return i, mux, meta, ids
}

func call(t *testing.T, mux http.Handler, method, path, token string) (int, string) {
	t.Helper()

	r := httptest.NewRequest(method, path, strings.NewReader("{}"))
	r.Header.Set("Content-Type", "application/json")
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, r)
	return rec.Code, rec.Body.String()
}

// routes is every address this plugin publishes. %s is a database id.
var routes = []struct{ method, path string }{
	{http.MethodPost, "/v1/cloud-sql/databases"},
	{http.MethodGet, "/v1/cloud-sql/databases"},
	{http.MethodGet, "/v1/cloud-sql/databases/%s"},
	{http.MethodDelete, "/v1/cloud-sql/databases/%s"},
	{http.MethodGet, "/v1/cloud-sql/databases/%s/connection"},
	{http.MethodPost, "/v1/cloud-sql/databases/%s/branches"},
	{http.MethodGet, "/v1/cloud-sql/databases/%s/branches"},
	{http.MethodGet, "/v1/meta/tables"},
	{http.MethodPost, "/v1/meta/query"},
	{http.MethodPut, "/v1/meta/tables/1"},
	{http.MethodPatch, "/v1/meta/tables/1"},
	{http.MethodDelete, "/v1/meta/tables/1"},
}

// TestNoCredentialReachesNothing is the whole point: a request carrying nothing
// names no org, so it names no database and reaches no address here.
func TestNoCredentialReachesNothing(t *testing.T) {
	_, mux, meta, ids := stand(t)

	for _, r := range routes {
		path := strings.Replace(r.path, "%s", ids["alpha"], 1)
		code, body := call(t, mux, r.method, path, "")
		if code != http.StatusUnauthorized {
			t.Errorf("%s %s anonymous answered %d %s, want 401", r.method, path, code, body)
		}
		for _, secret := range []string{"alpha-password", "beta-password", adminPassword} {
			if strings.Contains(body, secret) {
				t.Errorf("%s %s handed a password to a caller with no credential: %s", r.method, path, body)
			}
		}
	}

	if seen := meta.connections(); len(seen) != 0 {
		t.Errorf("postgres-meta was reached for a caller with no credential: %v", seen)
	}
}

// TestAnotherOrgsDatabaseIsRefused pins the boundary. A caller who holds a real
// credential still reaches one org's databases: its own.
func TestAnotherOrgsDatabaseIsRefused(t *testing.T) {
	iam, mux, meta, ids := stand(t)
	alpha := iam.token(t, "alpha/ann", "alpha")

	for _, r := range routes {
		if !strings.Contains(r.path, "%s") {
			continue
		}
		path := fmt.Sprintf(strings.Replace(r.path, "%s", "%[1]s", 1), ids["beta"])
		code, body := call(t, mux, r.method, path, alpha)
		if code != http.StatusForbidden {
			t.Errorf("%s %s as alpha answered %d %s, want 403", r.method, path, code, body)
		}
		if strings.Contains(body, "beta-password") {
			t.Errorf("%s %s handed alpha beta's password: %s", r.method, path, body)
		}
	}

	// The list is the org's own and not the estate's.
	code, body := call(t, mux, http.MethodGet, "/v1/cloud-sql/databases", alpha)
	if code != http.StatusOK {
		t.Fatalf("alpha could not list its own databases: %d %s", code, body)
	}
	if strings.Contains(body, "t_beta") || strings.Contains(body, `"beta"`) {
		t.Errorf("alpha's list named beta's database: %s", body)
	}
	if !strings.Contains(body, "t_alpha") {
		t.Errorf("alpha's list omitted its own database: %s", body)
	}

	// A statement of intent buys nothing: naming another org on create is
	// refused rather than filed under the caller's own.
	req := httptest.NewRequest(http.MethodPost, "/v1/cloud-sql/databases",
		strings.NewReader(`{"orgId":"beta","databaseName":"grab"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+alpha)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("alpha created a database for beta: %d %s", rec.Code, rec.Body.String())
	}

	for _, c := range meta.connections() {
		if strings.Contains(c, "beta-password") || strings.Contains(c, adminPassword) {
			t.Errorf("postgres-meta was lent a connection alpha may not open: %s", c)
		}
	}
}

// TestItsOwnDatabaseIsReachable is the other half. A rule that refuses everyone
// is not a rule, it is an outage.
func TestItsOwnDatabaseIsReachable(t *testing.T) {
	iam, mux, _, ids := stand(t)
	alpha := iam.token(t, "alpha/ann", "alpha")

	code, body := call(t, mux, http.MethodGet,
		"/v1/cloud-sql/databases/"+ids["alpha"]+"/connection", alpha)
	if code != http.StatusOK {
		t.Fatalf("alpha was refused its own connection: %d %s", code, body)
	}
	if !strings.Contains(body, "alpha-password") {
		t.Errorf("alpha's own connection string carried no password: %s", body)
	}

	if code, body := call(t, mux, http.MethodGet,
		"/v1/cloud-sql/databases/"+ids["alpha"], alpha); code != http.StatusOK {
		t.Errorf("alpha was refused its own database: %d %s", code, body)
	}
	if code, body := call(t, mux, http.MethodGet,
		"/v1/cloud-sql/databases/"+ids["alpha"]+"/branches", alpha); code != http.StatusOK {
		t.Errorf("alpha was refused its own branches: %d %s", code, body)
	}
}

// TestMetaProxyOpensOneDatabase pins what postgres-meta is lent: the connection
// to the caller's own database, and never the admin connection that opens every
// database on the server.
func TestMetaProxyOpensOneDatabase(t *testing.T) {
	iam, mux, meta, _ := stand(t)

	if code, body := call(t, mux, http.MethodGet, "/v1/meta/tables",
		iam.token(t, "alpha/ann", "alpha")); code != http.StatusOK {
		t.Fatalf("alpha was refused its own schema: %d %s", code, body)
	}

	seen := meta.connections()
	if len(seen) != 1 {
		t.Fatalf("postgres-meta was reached %d times, want 1: %v", len(seen), seen)
	}
	if !strings.Contains(seen[0], "alpha-password") || !strings.Contains(seen[0], "/t_alpha?") {
		t.Errorf("postgres-meta was lent %q, want alpha's own database", seen[0])
	}

	// An org with no database of its own is told so, on no connection at all.
	code, body := call(t, mux, http.MethodGet, "/v1/meta/tables",
		iam.token(t, "gamma/gil", "gamma"))
	if code != http.StatusNotFound {
		t.Errorf("an org with no database answered %d %s, want 404", code, body)
	}
	if seen := meta.connections(); len(seen) != 1 {
		t.Errorf("postgres-meta was reached for an org with no database: %v", seen)
	}
}

// TestACreatedDatabaseRequiresTLS pins that a handed-out connection string never
// turns TLS off. ConnectionString decides what an unstated mode means, and the
// create path stating one of its own is how it came to say disable.
func TestACreatedDatabaseRequiresTLS(t *testing.T) {
	iam, mux, _, _ := stand(t)

	req := httptest.NewRequest(http.MethodPost, "/v1/cloud-sql/databases",
		strings.NewReader(`{"databaseName":"gamma"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+iam.token(t, "gamma/g", "gamma"))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	body := rec.Body.String()
	if rec.Code != http.StatusCreated {
		t.Fatalf("gamma could not create its database: %d %s", rec.Code, body)
	}
	if strings.Contains(body, "sslmode=disable") {
		t.Errorf("a created database was handed back with TLS off: %s", body)
	}
	if !strings.Contains(body, "sslmode=require") {
		t.Errorf("a created database's connection string did not require TLS: %s", body)
	}
}
