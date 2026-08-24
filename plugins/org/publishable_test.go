package org

import (
	"net/http"
	"strings"
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
