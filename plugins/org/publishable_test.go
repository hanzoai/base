package org

import (
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/hanzoai/base/core"
)

// theConnection is what a route off the allowlist can hand back. It is a
// PostgreSQL connection string, admin password and all, because that is the
// shape of the answer the old rule had no opinion about.
const theConnection = "postgres://hanzo:server-admin-password@sql:5432/t_alpha"

// bothWays is the two spellings one publishable key arrives in. It needs no
// Authorization header at all, which is what makes the page source the
// credential.
var bothWays = []struct{ name, token, query string }{
	{"as a bearer", "pk-in-the-page", ""},
	{"in the query", "", "?key=pk-in-the-page"},
}

// TestAPublishableKeyReachesOnlyPublic pins the whole rule: an address a
// publishable key may reach is on a list, and every address off it is refused.
//
// A rule that instead asked whether some OTHER rule recognised the address had
// no opinion about most of the process. It refused a Base and admitted a
// database's connection string, an identity provider and a backup — not because
// those were judged safe, but because nothing judged them.
func TestAPublishableKeyReachesOnlyPublic(t *testing.T) {
	_, _, mux := keyed(t, "alpha", func(e *core.ServeEvent) {
		// A route family registered the way a plugin registers one, handing
		// back what such a family hands back.
		e.Router.GET("/v1/cloud-sql/databases/{id}/connection", func(re *core.RequestEvent) error {
			return re.JSON(http.StatusOK, map[string]string{"connectionString": theConnection})
		})
		e.Router.GET("/v1/meta/{path...}", func(re *core.RequestEvent) error {
			return re.JSON(http.StatusOK, map[string]string{"connectionString": theConnection})
		})
	})

	// Addresses this process really answers at, plus the two above. None is
	// public, so a key printed in a web page reaches none of them.
	refused := []string{
		"/v1/cloud-sql/databases/anything/connection",
		"/v1/meta/tables",
		"/v1/iam/oauth/userinfo", // the proxy unbinds the doors that resolve a key
		"/v1/idv/status",
		"/v1/bases",
		"/v1/bases/alpha/creds/stripe",
		"/v1/backups/dump.zip",
		"/v1/settings",
		"/v1/collections",
		"/v1/functions",
		"/v1/realtime",
	}

	for _, path := range refused {
		for _, how := range bothWays {
			code, body := call(t, mux, http.MethodGet, path+how.query, how.token, nil)
			if code != http.StatusForbidden {
				t.Errorf("GET %s %s answered %d %s, want 403", path, how.name, code, body)
			}
			if strings.Contains(body, "server-admin-password") {
				t.Errorf("GET %s %s handed a web page's key a database password: %s", path, how.name, body)
			}
		}
	}
}

// publicRows puts a collection anyone may read into one org's Base, with a row
// in it, the way a page's own data is declared.
func publicRows(t *testing.T, dataDir, collection string) string {
	t.Helper()

	app := core.NewBaseApp(core.BaseAppConfig{DataDir: dataDir})
	if err := app.Bootstrap(); err != nil {
		t.Fatal(err)
	}
	defer app.ResetBootstrapState()

	open := ""
	c := core.NewBaseCollection(collection)
	c.ListRule = &open
	c.ViewRule = &open
	c.Fields.Add(&core.TextField{Name: "title"})
	if err := app.Save(c); err != nil {
		t.Fatal(err)
	}

	r := core.NewRecord(c)
	r.Set("title", "on the page")
	if err := app.Save(r); err != nil {
		t.Fatal(err)
	}

	return r.Id
}

// TestAPublishableKeyReachesWhatIsPublic is the half that makes the first one
// worth having. A rule that refuses everyone is not a rule, it is an outage.
//
// Each address answers on the Base's own collection rules — the collection says
// the rows are public, and the key only says which org's Base to ask.
func TestAPublishableKeyReachesWhatIsPublic(t *testing.T) {
	app, _, mux := keyed(t, "alpha")
	id := publicRows(t, NewOrgDB(app, "").OrgDir("alpha"), "posts")

	rows := []struct{ what, path string }{
		{"the list", "/v1/collections/posts/records"},
		{"the row", "/v1/collections/posts/records/" + id},
	}
	for _, r := range rows {
		for _, how := range bothWays {
			code, body := call(t, mux, http.MethodGet, r.path+how.query, how.token, nil)
			if code != http.StatusOK {
				t.Errorf("%s %s answered %d %s, want 200", r.what, how.name, code, body)
			}
			if !strings.Contains(body, "on the page") {
				t.Errorf("%s %s did not hand back the public row: %s", r.what, how.name, body)
			}
		}
	}

	// The same rows on the rest wire, which decides them on the same listRule.
	// A bearer only: that wire reads every query parameter as a filter
	// predicate, so a key spelled `?key=` there is a malformed filter and never
	// a credential.
	code, body := call(t, mux, http.MethodGet, "/v1/rest/posts", "pk-in-the-page", nil)
	if code != http.StatusOK {
		t.Errorf("the rest wire answered %d %s, want 200", code, body)
	}
	if !strings.Contains(body, "on the page") {
		t.Errorf("the rest wire did not hand back the public row: %s", body)
	}

	// Liveness answers a request carrying nothing at all, so a key widens
	// nothing and is not refused for holding one.
	for _, path := range []string{"/v1/health", "/healthz"} {
		if code, body := call(t, mux, http.MethodGet, path, "pk-in-the-page", nil); code != http.StatusOK {
			t.Errorf("%s answered a publishable key %d %s, want 200", path, code, body)
		}
	}

	// A file the public rows name. There is none to serve, and 404 is that
	// answer; 403 would be the rule refusing the address.
	if code, body := call(t, mux, http.MethodGet,
		"/v1/files/posts/"+id+"/none.png", "pk-in-the-page", nil); code == http.StatusForbidden {
		t.Errorf("a publishable key was refused a public row's file: %s", body)
	}
}

// TestOnlyAPublishableKeyIsHeldToTheList pins that the rule is about the
// credential and nothing else. A secret key is the org's own server credential
// and reaches what it always did; so does a member's token.
func TestOnlyAPublishableKeyIsHeldToTheList(t *testing.T) {
	_, iam, mux := keyed(t, "alpha")

	const id = "9f3c21b8-7e4d-4a02-9c15-6b0f8d2e1a77"
	for _, c := range []struct{ what, token string }{
		{"a secret key", "sk-service"},
		{"a member's token", iam.signed(t, "member", id, "keyuser", "alpha")},
	} {
		if code, body := call(t, mux, http.MethodGet, "/v1/bases/alpha", c.token, nil); code == http.StatusForbidden {
			t.Errorf("%s was refused a Base it acts in: %s", c.what, body)
		}
	}
}

// spelling is one credential and how a caller wrote it.
type spelling struct {
	how     string
	headers map[string]string
	query   string
}

// spellings is one key written every way this process accepts a credential.
//
// The spelling is the caller's choice, so what a credential reaches cannot
// depend on it. Reading only the canonical one left the other eleven to be read
// by the doors — and, past a proxy that forwards headers and query verbatim, by
// whatever answers upstream.
func spellings(key string) []spelling {
	return []spelling{
		{"Bearer", map[string]string{"Authorization": "Bearer " + key}, ""},
		{"a lowercase scheme", map[string]string{"Authorization": "bearer " + key}, ""},
		{"an uppercase scheme", map[string]string{"Authorization": "BEARER " + key}, ""},
		{"two spaces", map[string]string{"Authorization": "Bearer  " + key}, ""},
		{"a tab", map[string]string{"Authorization": "Bearer\t" + key}, ""},
		{"bare", map[string]string{"Authorization": key}, ""},
		{"the alias header", map[string]string{"X-Authorization": "Bearer " + key}, ""},
		{"the alias header bare", map[string]string{"X-Authorization": key}, ""},
		{"the legacy header", map[string]string{"X-Auth-Token": key}, ""},
		{"the machine header", map[string]string{"X-API-Key": key}, ""},
		{"the query", nil, "?key=" + key},
		{"the query after a semicolon", nil, "?a=1;key=" + key},
	}
}

// TestNoSpellingOfAPublishableKeyReachesAnUpstream pins the rule against the
// two mounts where it is the only thing standing: /v1/iam and /v1/idv forward
// headers and query to their issuer verbatim, with the doors that resolve a
// credential deliberately unbound, so a key the rule fails to recognise is a key
// the issuer authenticates.
//
// It measures the BYTES the upstream received, not the status that came back. A
// status can be a refusal for some other reason; only the upstream's own record
// says whether a key printed in a web page was handed on.
func TestNoSpellingOfAPublishableKeyReachesAnUpstream(t *testing.T) {
	var mu sync.Mutex
	var handed []string

	idv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		for _, h := range []string{"Authorization", "X-Authorization", "X-Auth-Token", "X-API-Key"} {
			if v := r.Header.Get(h); v != "" {
				handed = append(handed, h+": "+v)
			}
		}
		if r.URL.RawQuery != "" {
			handed = append(handed, "?"+r.URL.RawQuery)
		}
		mu.Unlock()
		_, _ = w.Write([]byte(`{"upstream":"answered"}`))
	}))
	t.Cleanup(idv.Close)
	t.Setenv("IDV_ENDPOINT", idv.URL)

	_, _, mux := keyed(t, "alpha")

	// The IAM mount forwards to the same server keyed stands up for key
	// resolution, and that server answers every path — so a 200 carrying its
	// envelope is the issuer having answered a key printed in a web page.
	for _, s := range spellings("pk-in-the-page") {
		for _, path := range []string{"/v1/iam/oauth/userinfo", "/v1/idv/verify"} {
			code, body := call(t, mux, http.MethodGet, path+s.query, "", s.headers)
			if code != http.StatusForbidden {
				t.Errorf("GET %s spelled %s answered %d %s, want 403", path, s.how, code, body)
			}
			if strings.Contains(body, "upstream") || strings.Contains(body, `"status":"ok"`) {
				t.Errorf("GET %s spelled %s was answered by the upstream: %s", path, s.how, body)
			}
		}
	}

	mu.Lock()
	defer mu.Unlock()
	if len(handed) != 0 {
		t.Fatalf("a key printed in a web page was handed to the upstream: %q", handed)
	}
}

// TestEverySpellingOfAWorkingCredentialStillWorks is the other half. One reader
// is only right if it reads MORE spellings, not fewer: a secret key and a
// member's token authenticate in every one of them.
func TestEverySpellingOfAWorkingCredentialStillWorks(t *testing.T) {
	_, iam, mux := keyed(t, "alpha")

	const id = "9f3c21b8-7e4d-4a02-9c15-6b0f8d2e1a77"
	for _, c := range []struct{ what, token string }{
		{"a secret key", "sk-service"},
		{"a member's token", iam.signed(t, "member", id, "keyuser", "alpha")},
	} {
		for _, s := range spellings(c.token) {
			code, body := call(t, mux, http.MethodGet, "/v1/bases/alpha"+s.query, "", s.headers)
			if code == http.StatusForbidden || code == http.StatusUnauthorized {
				t.Errorf("%s spelled %s was refused its own org: %d %s", c.what, s.how, code, body)
			}
		}
	}
}

// recorder is an upstream that remembers every credential channel it was handed
// and the query it arrived with. What a proxy FORWARDS is what the far side
// authenticates on, so the measurement is the bytes that got there.
func recorder(t *testing.T) (*httptest.Server, func() []string, func() string) {
	t.Helper()

	var mu sync.Mutex
	var handed []string
	var last string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		for _, h := range []string{"Authorization", "X-Authorization", "X-Auth-Token", "X-API-Key"} {
			for _, v := range r.Header.Values(h) {
				handed = append(handed, h+": "+v)
			}
		}
		last = r.URL.RawQuery
		mu.Unlock()
		_, _ = w.Write([]byte(`{"upstream":"answered"}`))
	}))
	t.Cleanup(srv.Close)

	all := func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), handed...)
	}
	query := func() string {
		mu.Lock()
		defer mu.Unlock()
		return last
	}

	return srv, all, query
}

// channels is a publishable key hidden behind a decoy in every channel a
// credential can arrive on.
//
// The decoy is what makes each of these work: `nonsense` is a bare token by any
// reading, so a rule that judges only the credential a door would resolve judges
// the decoy, finds it harmless, and forwards the key beside it.
func channels(key string) []spelling {
	return []spelling{
		{"a repeat Authorization value", nil, ""}, // filled in below
		{"a decoy over X-API-Key", map[string]string{"Authorization": "nonsense", "X-API-Key": key}, ""},
		{"a decoy over X-Auth-Token", map[string]string{"Authorization": "nonsense", "X-Auth-Token": key}, ""},
		{"a decoy over X-Authorization", map[string]string{"Authorization": "nonsense", "X-Authorization": "Bearer " + key}, ""},
		{"a scheme we do not read over X-API-Key", map[string]string{"Authorization": "Basic dXNlcjpwYXNz", "X-API-Key": key}, ""},
		{"a decoy over the query", map[string]string{"Authorization": "nonsense"}, "?key=" + key},
		{"a decoy query parameter", nil, "?key=nonsense&key=" + key},
		{"a decoy query parameter after a semicolon", nil, "?key=nonsense;key=" + key},
		{"a decoy query parameter under an escaped name", nil, "?key=nonsense&k%65y=" + key},
		{"X-Auth-Token alone", map[string]string{"X-Auth-Token": key}, ""},
		{"X-API-Key alone", map[string]string{"X-API-Key": key}, ""},
	}
}

// TestAPublishableKeyIsRefusedInEveryChannel pins the whole class. A key printed
// in a web page is refused wherever it sits, because the proxies forward every
// channel and only one of them was ever judged.
func TestAPublishableKeyIsRefusedInEveryChannel(t *testing.T) {
	idv, handed, _ := recorder(t)
	t.Setenv("IDV_ENDPOINT", idv.URL)

	_, _, mux := keyed(t, "alpha")

	for _, c := range channels("pk-in-the-page") {
		for _, path := range []string{"/v1/iam/oauth/userinfo", "/v1/idv/verify"} {
			req := httptest.NewRequest(http.MethodGet, path+c.query, nil)
			for k, v := range c.headers {
				req.Header.Set(k, v)
			}
			if c.how == "a repeat Authorization value" {
				req.Header.Set("Authorization", "nonsense")
				req.Header.Add("Authorization", "Bearer pk-in-the-page")
			}

			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)

			if rec.Code != http.StatusForbidden {
				t.Errorf("GET %s with %s answered %d %s, want 403", path, c.how, rec.Code, rec.Body.String())
			}
			if strings.Contains(rec.Body.String(), "upstream") || strings.Contains(rec.Body.String(), `"status":"ok"`) {
				t.Errorf("GET %s with %s was answered by the upstream: %s", path, c.how, rec.Body.String())
			}
		}
	}

	for _, got := range handed() {
		if strings.Contains(got, "pk-") {
			t.Errorf("a key printed in a web page was handed to the upstream: %q", got)
		}
	}
}

// TestAnUpstreamsOwnCredentialIsUntouched is the half that makes the rule usable.
// Nothing is dropped and no other credential is disturbed — the IDV service
// authenticates its own callers on a key of its own format, and that key is not
// a pk-, so it reaches the service exactly as it was sent.
func TestAnUpstreamsOwnCredentialIsUntouched(t *testing.T) {
	idv, handed, query := recorder(t)
	t.Setenv("IDV_ENDPOINT", idv.URL)

	_, _, mux := keyed(t, "alpha")

	for _, c := range []struct {
		what    string
		headers map[string]string
		want    string
	}{
		{"the vendor's own key", map[string]string{"X-API-Key": "idv_live_7f3c"}, "X-API-Key: idv_live_7f3c"},
		{"a webhook signature beside it", map[string]string{
			"X-API-Key": "idv_live_7f3c", "X-Auth-Token": "sig_9a1b"}, "X-Auth-Token: sig_9a1b"},
		{"a scheme we do not read", map[string]string{"Authorization": "Basic dXNlcjpwYXNz"}, "Authorization: Basic dXNlcjpwYXNz"},
	} {
		code, body := call(t, mux, http.MethodGet, "/v1/idv/verify", "", c.headers)
		if code != http.StatusOK {
			t.Errorf("%s answered %d %s, want 200", c.what, code, body)
		}
		if !slices.Contains(handed(), c.want) {
			t.Errorf("%s did not reach the service: upstream got %q", c.what, handed())
		}
	}

	// And a legitimate single `key` travels unchanged.
	if code, body := call(t, mux, http.MethodGet, "/v1/idv/verify?key=idv_live_7f3c&a=1", "", nil); code != http.StatusOK {
		t.Errorf("a vendor key in the query answered %d %s, want 200", code, body)
	}
	if got := query(); got != "key=idv_live_7f3c&a=1" {
		t.Errorf("the upstream received %q, want it unchanged", got)
	}
}

// TestAProxyForwardsTheQueryItJudged pins the forward side, which holds for
// every credential and not only a publishable one: a request presenting `key`
// twice is read here by the first and by a last-occurrence server upstream by
// the second, so only the one that was read goes on.
func TestAProxyForwardsTheQueryItJudged(t *testing.T) {
	idv, _, query := recorder(t)
	t.Setenv("IDV_ENDPOINT", idv.URL)

	_, _, mux := keyed(t, "alpha")

	for _, c := range []struct{ how, query, want string }{
		{"a decoy in front", "?key=nonsense&key=sk-service", "key=nonsense"},
		{"a decoy after a semicolon", "?key=nonsense;key=sk-service", "key=nonsense"},
		{"a decoy under an escaped name", "?key=nonsense&k%65y=sk-service", "key=nonsense"},
		{"two secret keys", "?key=sk-service&key=sk-other", "key=sk-service"},
		{"the rest of the query kept", "?a=1&key=sk-service&b=2&key=sk-other", "a=1&key=sk-service&b=2"},
		{"one key, untouched", "?key=sk-service&a=1", "key=sk-service&a=1"},
		{"no key, untouched", "?a=1;b=2", "a=1;b=2"},
	} {
		if code, body := call(t, mux, http.MethodGet, "/v1/idv/verify"+c.query, "", nil); code != http.StatusOK {
			t.Fatalf("%s answered %d %s, want 200", c.how, code, body)
		}
		if got := query(); got != c.want {
			t.Errorf("%s: the upstream received %q, want %q", c.how, got, c.want)
		}
	}
}
