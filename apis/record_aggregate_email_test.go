package apis_test

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/hanzoai/base/core"
	"github.com/hanzoai/base/tests"
	"github.com/hanzoai/base/tools/types"
)

// targets is an auth collection a member may list whose records keep their
// emails private (emailVisibility off).
func targets(t testing.TB, app *tests.TestApp) {
	t.Helper()

	c := core.NewAuthCollection("targets")
	c.ListRule = types.Pointer("")
	if err := app.Save(c); err != nil {
		t.Fatal(err)
	}

	for _, email := range []string{"ceo@target.test", "cfo@target.test", "cto@target.test"} {
		r := core.NewRecord(c)
		r.Set("email", email)
		r.Set("emailVisibility", false)
		r.Set("password", "secret123456")
		if err := app.Save(r); err != nil {
			t.Fatal(err)
		}
	}
}

var privateEmails = []string{"ceo@target.test", "cfo@target.test", "cto@target.test"}

// An address a record keeps private reaches nobody but a superuser by any
// route: not in a list, not through a filter, and not as a figure or a group.
func TestAPrivateEmailIsNotAFigure(t *testing.T) {
	t.Parallel()

	scenarios := []tests.ApiScenario{
		{
			Name:               "the list masks it",
			Method:             http.MethodGet,
			URL:                "/v1/collections/targets/records",
			ExpectedStatus:     200,
			ExpectedContent:    []string{`"totalItems":3`},
			NotExpectedContent: privateEmails,
		},
		{
			Name:            "a filter finds none of it",
			Method:          http.MethodGet,
			URL:             "/v1/collections/targets/records?filter=" + url.QueryEscape(`email != ""`),
			ExpectedStatus:  200,
			ExpectedContent: []string{`"totalItems":0`},
		},
		{
			Name:               "a group by it is refused",
			Method:             http.MethodGet,
			URL:                "/v1/collections/targets/aggregate?" + url.Values{"ops": {"count"}, "group": {"email"}}.Encode(),
			ExpectedStatus:     400,
			ExpectedContent:    []string{`not filterable`},
			NotExpectedContent: privateEmails,
		},
		{
			Name:               "a count of it is refused",
			Method:             http.MethodGet,
			URL:                "/v1/collections/targets/aggregate?" + url.Values{"ops": {"countUnique(email)"}}.Encode(),
			ExpectedStatus:     400,
			ExpectedContent:    []string{`not filterable`},
			NotExpectedContent: []string{`"countUnique(email)":3`},
		},
	}

	for _, s := range scenarios {
		s.Headers = map[string]string{"Authorization": aggregateMemberToken}
		s.BeforeTestFunc = func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) { targets(t, app) }
		s.Test(t)
	}
}
