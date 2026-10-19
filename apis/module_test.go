package apis_test

import (
	"net/http"
	"testing"

	"github.com/golang-jwt/jwt/v5"
	"github.com/hanzoai/base/apis"
	"github.com/hanzoai/base/core"
	"github.com/hanzoai/base/modules"
	_ "github.com/hanzoai/base/modules/crm"
	"github.com/hanzoai/base/tests"
)

// The CRM is installed into an org's own Base by that org's admin. A member is
// refused, and a module cannot be the way an org admin reaches a collection
// only a superuser manages.
func TestModuleInstall(t *testing.T) {
	t.Parallel()

	const superuserToken = "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJjb2xsZWN0aW9uSWQiOiJoYmNfMzE0MjYzNTgyMyIsImV4cCI6MjUyNDYwNDQ2MSwiaWQiOiJzeXdiaGVjbmg0NnJobTAiLCJyZWZyZXNoYWJsZSI6dHJ1ZSwidHlwZSI6ImF1dGgifQ.CXBf8BazmUeg2RnJW8OEs1UFYF41rbCMOa6YZa4wZio"

	iam := func(role string) (map[string]string, func(testing.TB, *tests.TestApp, *core.ServeEvent)) {
		token, jwksURL := mintIAMToken(t, jwt.MapClaims{
			"sub": "acme/ann", "email": "ann@acme.test",
			"orgs": []any{map[string]any{"org": "acme", "role": role}},
		})
		return map[string]string{"Authorization": "Bearer " + token},
			func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
				app.Store().Set(apis.StoreKeyExternalAuthOnly, true)
				app.Store().Set(apis.StoreKeyJWKSURL, jwksURL)
			}
	}

	adminHeaders, adminSetup := iam("admin")
	ownerHeaders, ownerSetup := iam("owner")
	memberHeaders, memberSetup := iam("member")

	scenarios := []tests.ApiScenario{
		{
			Name:            "anonymous is refused",
			Method:          http.MethodPost,
			URL:             "/v1/modules/crm",
			ExpectedStatus:  401,
			ExpectedContent: []string{`"status":401`},
		},
		{
			Name:            "a member is refused",
			Method:          http.MethodPost,
			URL:             "/v1/modules/crm",
			Headers:         memberHeaders,
			BeforeTestFunc:  memberSetup,
			ExpectedStatus:  403,
			ExpectedContent: []string{`"status":403`},
			AfterTestFunc: func(t testing.TB, app *tests.TestApp, res *http.Response) {
				if _, err := app.FindCollectionByNameOrId("people"); err == nil {
					t.Fatal("a refused install created collections")
				}
			},
		},
		{
			Name:           "the org admin lists the modules",
			Method:         http.MethodGet,
			URL:            "/v1/modules",
			Headers:        adminHeaders,
			BeforeTestFunc: adminSetup,
			ExpectedStatus: 200,
			ExpectedContent: []string{
				`"name":"crm"`,
				`"title":"CRM"`,
				`"installed":false`,
			},
		},
		{
			Name:           "the org admin installs the CRM",
			Method:         http.MethodPost,
			URL:            "/v1/modules/crm",
			Headers:        adminHeaders,
			BeforeTestFunc: adminSetup,
			ExpectedStatus: 200,
			ExpectedContent: []string{
				`"module":"crm"`,
				`"created":["companies","people","opportunities","candidates","applications","segments","lists","list_members","activities","notes","tasks","attachments","views"]`,
				`"grown":{}`,
				`"kept":{}`,
			},
		},
		{
			Name:    "the org owner installs it again, which changes nothing",
			Method:  http.MethodPost,
			URL:     "/v1/modules/crm",
			Headers: ownerHeaders,
			BeforeTestFunc: func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
				ownerSetup(t, app, e)
				if _, err := core.GrowCollections(app, crmDocs(t)); err != nil {
					t.Fatal(err)
				}
			},
			ExpectedStatus:  200,
			ExpectedContent: []string{`"created":[]`, `"grown":{}`, `"kept":{}`},
		},
		{
			Name:            "a superuser installs it",
			Method:          http.MethodPost,
			URL:             "/v1/modules/crm",
			Headers:         map[string]string{"Authorization": superuserToken},
			ExpectedStatus:  200,
			ExpectedContent: []string{`"module":"crm"`, `"people"`},
		},
		{
			Name:            "an unknown module",
			Method:          http.MethodPost,
			URL:             "/v1/modules/nothing",
			Headers:         adminHeaders,
			BeforeTestFunc:  adminSetup,
			ExpectedStatus:  404,
			ExpectedContent: []string{`"status":404`},
		},
		{
			Name:    "an org admin cannot grow a collection only a superuser manages",
			Method:  http.MethodPost,
			URL:     "/v1/modules/crm",
			Headers: adminHeaders,
			BeforeTestFunc: func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
				adminSetup(t, app, e)
				views := core.NewAuthCollection("views")
				if err := app.Save(views); err != nil {
					t.Fatal(err)
				}
			},
			ExpectedStatus:  403,
			ExpectedContent: []string{`"status":403`, `views`},
		},
	}

	for _, s := range scenarios {
		s.Test(t)
	}
}

func crmDocs(t testing.TB) []map[string]any {
	t.Helper()
	m, ok := modules.Get("crm")
	if !ok {
		t.Fatal("the crm module is not registered")
	}
	return m.Collections
}
