package apis_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/hanzoai/base/apis"
	"github.com/hanzoai/base/core"
	"github.com/hanzoai/base/tests"
)

// orgBase readies a test app as an org's Base: IAM is its only auth, and it
// holds a collection of the org's own and a system one.
func orgBase(t testing.TB, app *tests.TestApp, jwksURL, tenant string) {
	app.Store().Set(apis.StoreKeyExternalAuthOnly, true)
	app.Store().Set(apis.StoreKeyJWKSURL, jwksURL)
	if tenant != "" {
		app.Store().Set(apis.StoreKeyTenant, tenant)
	}

	own := core.NewBaseCollection("leads")
	own.Fields.Add(&core.TextField{Name: "title"})
	if err := app.Save(own); err != nil {
		t.Fatal(err)
	}

	sys := core.NewBaseCollection("sysbox")
	sys.System = true
	sys.Fields.Add(&core.TextField{Name: "title"})
	if err := app.Save(sys); err != nil {
		t.Fatal(err)
	}
}

// An org admin changes its own Base's schema; a member does not; and what an
// org admin may change is the org's own base collections and nothing Base keeps
// for itself.
func TestOrgAdminSchemaAuthority(t *testing.T) {
	t.Parallel()

	type call struct {
		name, method, url, body string
		status                  int
		content                 string
	}

	member := []call{
		{"member lists", http.MethodGet, "/v1/collections", "", 403, `"status":403`},
		{"member creates", http.MethodPost, "/v1/collections", `{"name":"mine","type":"base"}`, 403, `"status":403`},
		{"member updates", http.MethodPatch, "/v1/collections/leads", `{"fields":[{"name":"title","type":"text"},{"name":"stage","type":"text"}]}`, 403, `"status":403`},
		{"member deletes", http.MethodDelete, "/v1/collections/leads", "", 403, `"status":403`},
		{"member truncates", http.MethodDelete, "/v1/collections/leads/truncate", "", 403, `"status":403`},
		{"member imports", http.MethodPut, "/v1/collections/import", `{"collections":[{"name":"mine","type":"base"}]}`, 403, `"status":403`},
	}

	admin := []call{
		{"admin lists", http.MethodGet, "/v1/collections?filter=name%3D%27leads%27", "", 200, `"name":"leads"`},
		{"admin views a system collection", http.MethodGet, "/v1/collections/users", "", 200, `"name":"users"`},
		{"admin creates a base collection", http.MethodPost, "/v1/collections",
			`{"name":"deals","type":"base","fields":[{"name":"title","type":"text","label":"Title"}]}`, 200, `"name":"deals"`},
		{"admin adds a field", http.MethodPatch, "/v1/collections/leads",
			`{"fields":[{"name":"title","type":"text"},{"name":"stage","type":"select","values":["new","won"],"maxSelect":1}]}`, 200, `"name":"stage"`},
		{"admin deletes its collection", http.MethodDelete, "/v1/collections/leads", "", 204, ""},
		{"admin truncates its collection", http.MethodDelete, "/v1/collections/leads/truncate", "", 204, ""},
		{"admin imports a new collection", http.MethodPut, "/v1/collections/import",
			`{"collections":[{"name":"imported","type":"base","fields":[{"name":"title","type":"text"}]}]}`, 204, ""},

		{"admin cannot change users", http.MethodPatch, "/v1/collections/users", `{"listRule":""}`, 403, `cannot change the collection \"users\"`},
		{"admin cannot change _superusers", http.MethodPatch, "/v1/collections/_superusers", `{"listRule":""}`, 403, `"status":403`},
		{"admin cannot change _history", http.MethodPatch, "/v1/collections/_history", `{"listRule":""}`, 403, `"status":403`},
		{"admin cannot change a system collection", http.MethodPatch, "/v1/collections/sysbox", `{"listRule":""}`, 403, `"status":403`},
		{"admin cannot delete a system collection", http.MethodDelete, "/v1/collections/sysbox", "", 403, `"status":403`},
		{"admin cannot truncate users", http.MethodDelete, "/v1/collections/users/truncate", "", 403, `"status":403`},
		{"admin cannot rename into Base's own names", http.MethodPatch, "/v1/collections/leads", `{"name":"_leads"}`, 403, `"status":403`},
		{"admin cannot create a view", http.MethodPost, "/v1/collections",
			`{"name":"v","type":"view","viewQuery":"select id from leads"}`, 403, `"status":403`},
		{"admin cannot create an auth collection", http.MethodPost, "/v1/collections", `{"name":"staff","type":"auth"}`, 403, `"status":403`},
		{"admin cannot create a system collection", http.MethodPost, "/v1/collections", `{"name":"core","type":"base","system":true}`, 403, `"status":403`},
		{"admin cannot import with deleteMissing", http.MethodPut, "/v1/collections/import",
			`{"deleteMissing":true,"collections":[{"name":"leads","type":"base"}]}`, 403, `deleteMissing`},
		{"admin cannot import over users", http.MethodPut, "/v1/collections/import",
			`{"collections":[{"name":"users","listRule":""}]}`, 403, `"status":403`},
		{"admin cannot rotate token secrets", http.MethodPost, "/v1/collections/users/rotate", "", 403, `"status":403`},
	}

	run := func(t *testing.T, org, role, tenant string, calls []call) {
		for _, c := range calls {
			token, jwksURL := iamCaller(t, org, role)

			var body *strings.Reader
			if c.body != "" {
				body = strings.NewReader(c.body)
			}

			s := tests.ApiScenario{
				Name:           role + " of " + org + ": " + c.name,
				Method:         c.method,
				URL:            c.url,
				Headers:        map[string]string{"Authorization": "Bearer " + token},
				ExpectedStatus: c.status,
				BeforeTestFunc: func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
					orgBase(t, app, jwksURL, tenant)
				},
			}
			if body != nil {
				s.Body = body
			}
			if c.content != "" {
				s.ExpectedContent = []string{c.content}
			}
			s.Test(t)
		}
	}

	run(t, "acme", "member", "acme", member)
	run(t, "acme", "admin", "acme", admin)
	run(t, "acme", "owner", "acme", admin[:3])

	// an admin of another org reaches this Base's schema no more than its records
	run(t, "zoo", "owner", "acme", []call{
		{"another org's admin creates", http.MethodPost, "/v1/collections", `{"name":"mine","type":"base"}`, 403, `"status":403`},
		{"another org's admin lists", http.MethodGet, "/v1/collections", "", 403, `"status":403`},
	})

	anonymous := tests.ApiScenario{
		Name:            "anonymous",
		Method:          http.MethodPost,
		URL:             "/v1/collections",
		Body:            strings.NewReader(`{"name":"mine","type":"base"}`),
		ExpectedStatus:  401,
		ExpectedContent: []string{`"status":401`},
	}
	anonymous.Test(t)
}
