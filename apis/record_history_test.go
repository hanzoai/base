package apis_test

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/golang-jwt/jwt/v5"
	"github.com/hanzoai/base/apis"
	"github.com/hanzoai/base/core"
	"github.com/hanzoai/base/tests"
	"github.com/hanzoai/base/tools/types"
)

// iamCaller is a person of org with role there, as IAM signs one.
func iamCaller(t *testing.T, org, role string) (token, jwksURL string) {
	t.Helper()
	return mintIAMToken(t, jwt.MapClaims{
		"sub": org + "/ann", "email": "ann@" + org + ".test", "name": "ann",
		"orgs": []any{map[string]any{"org": org, "role": role}},
	})
}

// A record's history is read by whoever may view the record, and nobody else.
func TestRecordHistoryRoute(t *testing.T) {
	t.Parallel()

	const id = "deal00000000001"

	ready := func(t testing.TB, app *tests.TestApp, jwksURL string, viewRule *string, deleted bool) {
		app.Store().Set(apis.StoreKeyExternalAuthOnly, true)
		app.Store().Set(apis.StoreKeyJWKSURL, jwksURL)

		c := core.NewBaseCollection("deals")
		c.History = true
		c.ViewRule = viewRule
		c.Fields.Add(&core.TextField{Name: "title"}, &core.TextField{Name: "note"})
		if err := app.Save(c); err != nil {
			t.Fatal(err)
		}

		ann := core.WithActor(context.Background(), types.Actor{Source: core.ActorManual, Id: "ann", Name: "Ann"})
		bob := core.WithActor(context.Background(), types.Actor{Source: core.ActorImport, Id: "bob", Name: "Bob"})

		r := core.NewRecord(c)
		r.Id = id
		r.Set("title", "Acme")
		r.Set("note", "call back")
		if err := app.SaveWithContext(ann, r); err != nil {
			t.Fatal(err)
		}
		r, _ = app.FindRecordById(c, id)
		r.Set("title", "Acme Inc")
		r.Set("note", "called")
		if err := app.SaveWithContext(bob, r); err != nil {
			t.Fatal(err)
		}

		// the note is hidden after it changed: its change is no reader's now
		c.Fields.GetByName("note").SetHidden(true)
		if err := app.Save(c); err != nil {
			t.Fatal(err)
		}

		if deleted {
			if err := app.DeleteWithContext(ann, r); err != nil {
				t.Fatal(err)
			}
		}
	}

	adminOnly := types.Pointer(`@request.auth.role = "admin"`)
	url := "/v1/collections/deals/records/" + id + "/history"

	scenarios := []struct {
		name     string
		role     string
		rule     *string
		deleted  bool
		status   int
		content  []string
		excluded []string
		order    []string
	}{
		{
			name: "a viewer reads it, newest first", role: "admin", rule: adminOnly, status: 200,
			content: []string{
				`"totalItems":2`, `"action":"update"`, `"action":"create"`, `"name":"Bob"`, `"source":"import"`,
				`"title":{"from":"Acme","to":"Acme Inc"}`, `"title":{"to":"Acme"}`,
			},
			excluded: []string{"call back", "called"},
			order:    []string{`"action":"update"`, `"action":"create"`},
		},
		{name: "a caller the view rule refuses", role: "member", rule: adminOnly, status: 404, content: []string{`"status":404`}},
		{name: "a collection only superusers view", role: "admin", rule: nil, status: 403, content: []string{`"status":403`}},
		{name: "a deleted record", role: "admin", rule: adminOnly, deleted: true, status: 404, content: []string{`"status":404`}},
	}

	for _, s := range scenarios {
		token, jwksURL := iamCaller(t, "acme", s.role)

		scenario := tests.ApiScenario{
			Name:               s.name,
			Method:             http.MethodGet,
			URL:                url,
			Headers:            map[string]string{"Authorization": "Bearer " + token},
			ExpectedStatus:     s.status,
			ExpectedContent:    s.content,
			NotExpectedContent: s.excluded,
			BeforeTestFunc: func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
				ready(t, app, jwksURL, s.rule, s.deleted)
			},
			AfterTestFunc: func(t testing.TB, app *tests.TestApp, res *http.Response) {
				if len(s.order) == 0 {
					return
				}
				raw, _ := io.ReadAll(res.Body)
				body := string(raw)
				last := -1
				for _, want := range s.order {
					at := strings.Index(body, want)
					if at <= last {
						t.Fatalf("expected %v in that order in %s", s.order, body)
					}
					last = at
				}
			},
		}
		scenario.Test(t)
	}
}
