package apis_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/golang-jwt/jwt/v5"
	"github.com/hanzoai/base/apis"
	"github.com/hanzoai/base/core"
	"github.com/hanzoai/base/tests"
	"github.com/hanzoai/base/tools/types"
)

// A rule reads the caller's authority in the org it acts in: whether it
// administers the org (`@request.auth.admin`) and its role there
// (`@request.auth.role`). Both come off the verified token, onto the auth record
// IAM's token is mirrored into, which is never saved.
func TestRulesReadTheCallersOrgAuthority(t *testing.T) {
	t.Parallel()

	member := []any{map[string]any{"org": "acme", "role": "member"}}
	admin := []any{map[string]any{"org": "acme", "role": "admin"}}
	owner := []any{map[string]any{"org": "acme", "role": "owner"}}

	scenarios := []struct {
		name   string
		rule   string
		orgs   []any
		expect string
	}{
		{"an admin rule admits an admin", `@request.auth.admin = true`, admin, `"totalItems":1`},
		{"an admin rule admits an owner", `@request.auth.admin = true`, owner, `"totalItems":1`},
		{"an admin rule refuses a member", `@request.auth.admin = true`, member, `"totalItems":0`},
		{"a role rule reads the role", `@request.auth.role = "owner"`, owner, `"totalItems":1`},
		{"a role rule refuses another role", `@request.auth.role = "owner"`, admin, `"totalItems":0`},
		{"a member rule reads the role", `@request.auth.role = "member"`, member, `"totalItems":1`},
		{"the org is read off the record, not a missing row", `@request.auth.org_id = "acme"`, member, `"totalItems":1`},
	}

	for _, s := range scenarios {
		token, jwksURL := mintIAMToken(t, jwt.MapClaims{"sub": "acme/ann", "email": "ann@acme.test", "name": "ann", "orgs": s.orgs})

		scenario := tests.ApiScenario{
			Name:            s.name,
			Method:          http.MethodGet,
			URL:             "/v1/collections/secrets/records",
			Headers:         map[string]string{"Authorization": "Bearer " + token},
			ExpectedStatus:  200,
			ExpectedContent: []string{s.expect},
			BeforeTestFunc: func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
				app.Store().Set(apis.StoreKeyExternalAuthOnly, true)
				app.Store().Set(apis.StoreKeyJWKSURL, jwksURL)

				c := core.NewBaseCollection("secrets")
				c.ListRule = types.Pointer(s.rule)
				c.Fields.Add(&core.TextField{Name: "title"})
				if err := app.Save(c); err != nil {
					t.Fatal(err)
				}
				r := core.NewRecord(c)
				r.Set("title", "the plan")
				if err := app.Save(r); err != nil {
					t.Fatal(err)
				}
			},
		}

		scenario.Test(t)
	}
}

// A Base its host opened for one org answers that org and no other, whatever
// org the token says it acts in.
func TestATenantBaseRefusesAnotherOrg(t *testing.T) {
	t.Parallel()

	scenarios := []struct {
		name   string
		orgs   []any
		status int
	}{
		{"its own org", []any{map[string]any{"org": "acme", "role": "member"}}, 200},
		{"another org", []any{map[string]any{"org": "zoo", "role": "owner"}}, 403},
	}

	for _, s := range scenarios {
		token, jwksURL := mintIAMToken(t, jwt.MapClaims{"sub": "x/ann", "email": "ann@x.test", "orgs": s.orgs})

		scenario := tests.ApiScenario{
			Name:           s.name,
			Method:         http.MethodGet,
			URL:            "/v1/collections/demo1/records",
			Headers:        map[string]string{"Authorization": "Bearer " + token},
			ExpectedStatus: s.status,
			ExpectedContent: []string{map[bool]string{
				true:  `"items"`,
				false: `"status":403`,
			}[s.status == 200]},
			BeforeTestFunc: func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
				app.Store().Set(apis.StoreKeyExternalAuthOnly, true)
				app.Store().Set(apis.StoreKeyJWKSURL, jwksURL)
				app.Store().Set(apis.StoreKeyTenant, "acme")

				c, err := app.FindCollectionByNameOrId("demo1")
				if err != nil {
					t.Fatal(err)
				}
				c.ListRule = types.Pointer("")
				if err := app.Save(c); err != nil {
					t.Fatal(err)
				}
			},
		}

		scenario.Test(t)
	}
}

// A record a person creates through the API names them as its writer, and
// nothing in the request body can name someone else.
func TestARequestStatesItsWriter(t *testing.T) {
	t.Parallel()

	token, jwksURL := mintIAMToken(t, jwt.MapClaims{
		"sub": "acme/ann", "email": "ann@acme.test", "name": "Ann Example",
		"orgs": []any{map[string]any{"org": "acme", "role": "member"}},
	})

	scenario := tests.ApiScenario{
		Method:         http.MethodPost,
		URL:            "/v1/collections/deals/records",
		Body:           strings.NewReader(`{"title":"big","createdBy":{"source":"manual","id":"mallory","name":"Mallory"}}`),
		Headers:        map[string]string{"Authorization": "Bearer " + token},
		ExpectedStatus: 200,
		ExpectedContent: []string{
			`"createdBy":{"source":"manual","id":"`,
			`"name":"Ann Example"}`,
		},
		NotExpectedContent: []string{"mallory", "Mallory"},
		BeforeTestFunc: func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
			app.Store().Set(apis.StoreKeyExternalAuthOnly, true)
			app.Store().Set(apis.StoreKeyJWKSURL, jwksURL)

			c := core.NewBaseCollection("deals")
			c.CreateRule = types.Pointer(`@request.auth.id != ""`)
			c.Fields.Add(&core.TextField{Name: "title"}, &core.ActorField{Name: "createdBy", OnCreate: true})
			if err := app.Save(c); err != nil {
				t.Fatal(err)
			}
		},
	}

	scenario.Test(t)
}
