package crm_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/golang-jwt/jwt/v5"
	"github.com/hanzoai/base/apis"
	"github.com/hanzoai/base/modules"
	"github.com/hanzoai/base/modules/crm"
	"github.com/hanzoai/base/tests"
	"github.com/hanzoai/base/tools/types"
)

// A private view is its maker's: nobody else lists, reads, changes or deletes
// it, an org admin included, while an org view is every member's to read and
// its maker's or an org admin's to change.
func TestAPrivateViewIsItsMakersOwn(t *testing.T) {
	app := install(t)
	iam := tests.NewIAM(t)
	app.Store().Set(apis.StoreKeyExternalAuthOnly, true)
	app.Store().Set(apis.StoreKeyJWKSURL, iam.URL)
	mux := tests.Router(t, app)

	person := func(name, role string) string {
		return "Bearer " + iam.Token(t, jwt.MapClaims{"sub": "acme/" + name, "email": name + "@acme.test", "name": name,
			"orgs": []any{map[string]any{"org": "acme", "role": role}}})
	}
	ann, bob, cat := person("ann", "member"), person("bob", "member"), person("cat", "admin")

	do := func(token, method, path, body string) (int, string) {
		t.Helper()
		var req *http.Request
		if body == "" {
			req = httptest.NewRequest(method, path, nil)
		} else {
			req = httptest.NewRequest(method, path, strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
		}
		req.Header.Set("Authorization", token)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)
		return w.Code, w.Body.String()
	}
	id := func(body string) string {
		var r struct{ Id string }
		if err := json.Unmarshal([]byte(body), &r); err != nil || r.Id == "" {
			t.Fatalf("no record in %s", body)
		}
		return r.Id
	}

	code, body := do(ann, http.MethodPost, "/v1/collections/views/records", `{"name":"Mine","object":"people","visibility":"private"}`)
	if code != 200 {
		t.Fatalf("a member's private view: %d %s", code, body)
	}
	private := id(body)
	code, body = do(ann, http.MethodPost, "/v1/collections/views/records", `{"name":"Ours","object":"people","visibility":"org"}`)
	if code != 200 {
		t.Fatalf("an org view: %d %s", code, body)
	}
	org := id(body)
	code, body = do(ann, http.MethodPost, "/v1/collections/views/records", `{"name":"Old","object":"people"}`)
	if code != 200 {
		t.Fatalf("a view that says nothing: %d %s", code, body)
	}

	for _, s := range []struct {
		name, token, method, path, body string
		status                          int
		has, lacks                      string
	}{
		{"its maker lists it", ann, http.MethodGet, "/v1/collections/views/records", "", 200, `"name":"Mine"`, ""},
		{"another member lists the org's views only", bob, http.MethodGet, "/v1/collections/views/records", "", 200, `"totalItems":2`, `"name":"Mine"`},
		{"an admin lists the org's views only", cat, http.MethodGet, "/v1/collections/views/records", "", 200, `"totalItems":2`, `"name":"Mine"`},
		{"another member cannot read it", bob, http.MethodGet, "/v1/collections/views/records/" + private, "", 404, "", "Mine"},
		{"an admin cannot read it", cat, http.MethodGet, "/v1/collections/views/records/" + private, "", 404, "", "Mine"},
		{"an admin cannot change it", cat, http.MethodPatch, "/v1/collections/views/records/" + private, `{"name":"Taken"}`, 404, "", ""},
		{"an admin cannot make it the org's", cat, http.MethodPatch, "/v1/collections/views/records/" + private, `{"visibility":"org"}`, 404, "", ""},
		{"an admin cannot delete it", cat, http.MethodDelete, "/v1/collections/views/records/" + private, "", 404, "", ""},
		{"another member cannot change an org view", bob, http.MethodPatch, "/v1/collections/views/records/" + org, `{"name":"Mine now"}`, 404, "", ""},
		{"an admin changes an org view", cat, http.MethodPatch, "/v1/collections/views/records/" + org, `{"name":"Ours, renamed"}`, 200, "Ours, renamed", ""},
		{"its maker changes it", ann, http.MethodPatch, "/v1/collections/views/records/" + private, `{"name":"Still mine"}`, 200, "Still mine", ""},
		{"nobody signed in lists none", "", http.MethodGet, "/v1/collections/views/records", "", 200, `"totalItems":0`, ""},
	} {
		t.Run(s.name, func(t *testing.T) {
			code, body := do(s.token, s.method, s.path, s.body)
			if code != s.status || !strings.Contains(body, s.has) || (s.lacks != "" && strings.Contains(body, s.lacks)) {
				t.Fatalf("%d %s", code, body)
			}
		})
	}
}

// A reinstall tightens a rule the module shipped earlier and keeps one an org
// wrote: a Base that installed the CRM before views could be private gets the
// rules that keep them so.
func TestAReinstallTightensTheRulesItShipped(t *testing.T) {
	app := install(t)

	views := mustCollection(t, app, "views")
	want := *views.ListRule
	shipped := `@request.auth.id != ""`
	ours := `@request.auth.admin = true`
	views.ListRule, views.ViewRule, views.UpdateRule = types.Pointer(shipped), types.Pointer(shipped), types.Pointer(ours)
	if err := app.Save(views); err != nil {
		t.Fatal(err)
	}

	g, err := modules.Install(app, crm.Name)
	if err != nil {
		t.Fatal(err)
	}
	if got := g.Grown["views"]; !slices.Contains(got, "rule:listRule") || !slices.Contains(got, "rule:viewRule") || slices.Contains(got, "rule:updateRule") {
		t.Fatalf("grown %v", got)
	}

	views = mustCollection(t, app, "views")
	if *views.ListRule != want || *views.ViewRule != want || *views.UpdateRule != ours {
		t.Fatalf("rules %q %q %q", *views.ListRule, *views.ViewRule, *views.UpdateRule)
	}
	if again, err := modules.Install(app, crm.Name); err != nil || len(again.Grown) != 0 {
		t.Fatalf("a third install changed %v %v", again, err)
	}
}
