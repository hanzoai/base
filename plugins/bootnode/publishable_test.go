package bootnode

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hanzoai/base/apis"
	"github.com/hanzoai/base/core"
	"github.com/hanzoai/base/plugins/bootnode/models"
	"github.com/hanzoai/base/plugins/org"
	"github.com/hanzoai/base/tests"
)

// theConnection is what a route off the declared surface can hand back — a
// PostgreSQL connection string, admin password and all.
const theConnection = "postgres://hanzo:server-admin-password@sql:5432/t_lux"

// oneProcess stands up the org rule that refuses a publishable key and
// bootnode's routes on one router, which is the only arrangement in which the
// two can be seen to agree. A rule tested apart from the routes it governs is a
// rule tested against nothing.
func oneProcess(t *testing.T) http.Handler {
	t.Helper()

	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(app.Cleanup)
	if err := models.EnsureAll(app); err != nil {
		t.Fatal(err)
	}

	// The half of IAM a key resolves against. Every key here belongs to lux.
	keys := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok","data":{"id":"user-123","name":"keyuser","email":"k@lux.network","owner":"lux"}}`))
	}))
	t.Cleanup(keys.Close)

	if err := org.Register(app, org.Config{IAMEndpoint: keys.URL, KMSEndpoint: "127.0.0.1:1",
		IAMClientID: "svc", IAMClientSecret: "shh"}); err != nil {
		t.Fatal(err)
	}
	if _, err := org.NewOrgDB(app, "").ProvisionOrg("lux"); err != nil {
		t.Fatal(err)
	}
	if err := Register(app, Config{Enabled: true, IAMEndpoint: keys.URL,
		APIKeySalt: "test-salt", KubeNamespace: "bootnode"}); err != nil {
		t.Fatal(err)
	}

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

	// A route family registered the way another product registers one, handing
	// back what such a family hands back. Nobody declared it public.
	e.Router.GET("/v1/cloud-sql/databases/{id}/connection", func(re *core.RequestEvent) error {
		return re.JSON(http.StatusOK, map[string]string{"connectionString": theConnection})
	})

	mux, err := e.Router.BuildMux()
	if err != nil {
		t.Fatal(err)
	}
	return mux
}

// ask sends one request carrying a publishable key.
func ask(t *testing.T, mux http.Handler, method, path string) (int, string) {
	t.Helper()

	req := httptest.NewRequest(method, path, strings.NewReader("{}"))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer pk-in-the-page")

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	body, _ := io.ReadAll(rec.Body)
	return rec.Code, string(body)
}

// refused is the rule's own words, which is how a refusal BY THE RULE is told
// apart from a handler's own answer.
const refused = "reaches only what is public"

// reads is every bootnode address a publishable key is meant to reach.
var reads = []string{
	"/v1/auth/me",
	"/v1/auth/projects/p1",
	"/v1/auth/keys?projectId=p1",
	"/v1/team",
	"/v1/chains",
	"/v1/chains/lux",
	"/v1/networks",
	"/v1/networks/n1",
	"/v1/nodes",
	"/v1/nodes/n1",
	"/v1/keys/status",
	"/v1/keys/k1",
}

// writes is every bootnode address a publishable key must not reach.
var writes = []struct{ method, path string }{
	{http.MethodPost, "/v1/auth/projects"},
	{http.MethodPost, "/v1/auth/keys"},
	{http.MethodDelete, "/v1/auth/keys/k1"},
	{http.MethodPost, "/v1/team"},
	{http.MethodPatch, "/v1/team/m1"},
	{http.MethodDelete, "/v1/team/m1"},
	{http.MethodPost, "/v1/networks"},
	{http.MethodDelete, "/v1/networks/n1"},
	{http.MethodPost, "/v1/nodes"},
	{http.MethodDelete, "/v1/nodes/n1"},
	{http.MethodPost, "/v1/keys"},
}

// TestAPublishableKeyReachesTheReadsBootnodeDeclares pins the agreement. These
// reads answer within the key's own org and hand back no key material, which is
// what a key printed in a page is for — and bootnode says so beside them, so
// the rule and the routes cannot fall out of step.
func TestAPublishableKeyReachesTheReadsBootnodeDeclares(t *testing.T) {
	mux := oneProcess(t)

	for _, path := range reads {
		code, body := ask(t, mux, http.MethodGet, path)
		if strings.Contains(body, refused) {
			t.Errorf("GET %s was refused the address: %d %s", path, code, body)
		}
	}

	// The catalogue takes no credential at all, so it answers outright.
	if code, body := ask(t, mux, http.MethodGet, "/v1/chains"); code != http.StatusOK {
		t.Errorf("GET /v1/chains answered %d %s, want 200", code, body)
	}
}

// TestAPublishableKeyWritesNothing pins that no bootnode write is on the
// declared surface.
func TestAPublishableKeyWritesNothing(t *testing.T) {
	mux := oneProcess(t)

	for _, w := range writes {
		if code, body := ask(t, mux, w.method, w.path); code != http.StatusForbidden {
			t.Errorf("%s %s answered %d %s, want 403", w.method, w.path, code, body)
		}
	}
}

// TestReadOnlyFollowsTheCredential pins the second layer, at its own level: the
// handler refuses a publishable write on its own, so the address list is not
// the only thing standing.
//
// The identity headers say WHO. They are written by the plugin that resolved
// the key, and it writes the same ones for every kind of key, so reading only
// them made a key printed in a web page a caller with a person's privileges and
// left every read-only refusal below unreachable.
func TestReadOnlyFollowsTheCredential(t *testing.T) {
	p, _, cleanup := newTestPlugin(t)
	defer cleanup()

	for _, c := range []struct {
		cred string
		want bool
	}{
		{"pk-in-the-page", true},
		{"sk-the-server", false},
		{"hk-a-person", false},
	} {
		e := &core.RequestEvent{}
		e.Request = httptest.NewRequest(http.MethodPost, "/v1/networks", nil)
		e.Response = httptest.NewRecorder()
		e.Request.Header.Set("X-User-Id", "user-123")
		e.Request.Header.Set("X-Org-Id", "lux")
		e.Request.Header.Set("Authorization", "Bearer "+c.cred)

		id, err := p.requireUser(e)
		if err != nil {
			t.Fatalf("%s: %v", c.cred, err)
		}
		if id.ReadOnly != c.want {
			t.Errorf("%s handed over identity headers: ReadOnly = %v, want %v", c.cred, id.ReadOnly, c.want)
		}
	}
}

// TestAPublishableKeyReachesNothingElse pins the default. An address nobody
// declared is refused, whatever it would have handed back.
func TestAPublishableKeyReachesNothingElse(t *testing.T) {
	mux := oneProcess(t)

	code, body := ask(t, mux, http.MethodGet, "/v1/cloud-sql/databases/anything/connection")
	if code != http.StatusForbidden {
		t.Errorf("the connection string answered %d %s, want 403", code, body)
	}
	if strings.Contains(body, "server-admin-password") {
		t.Errorf("a web page's key was handed a database password: %s", body)
	}
}
