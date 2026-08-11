package apis_test

import (
	"net/http"
	"testing"

	"github.com/hanzoai/base/core"
	"github.com/hanzoai/base/tests"
)

// TestAnonymousOwnerRuleDoesNotLeak pins the behaviour of the most common rule
// anyone writes — `owner = @request.auth.id` — for a caller who is not signed
// in.
//
// Postgres answers this by excluding every row: `owner = NULL` is NULL, and a
// NULL predicate is not true, so an anonymous caller sees nothing. A Supabase
// user porting a policy expects exactly that, and the rows they are protecting
// are precisely the ones this rule is written to protect.
//
// Base has been answering the opposite for rows whose owner is empty or NULL.
// `@request.auth.id` resolves to the identifier NULL when there is no auth
// (core/record_field_resolver_runner.go), the filter compiler counts the literal
// "null" as an empty identifier (tools/search/filter.go), and equality against
// an empty identifier is rewritten to `(owner = '' OR owner IS NULL)` so that a
// missing value can be compared. Each of those three is reasonable alone. Read
// together they hand every unowned row to anybody who asks.
//
// The row with a real owner is here to prove the rule still filters — a fix that
// returns nothing at all would pass a test that only checked the leak.
func TestAnonymousOwnerRuleDoesNotLeak(t *testing.T) {
	t.Parallel()

	setup := func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
		c := core.NewBaseCollection("rlsprobe")
		c.Fields.Add(&core.TextField{Name: "owner"})
		c.Fields.Add(&core.TextField{Name: "marker"})
		rule := "owner = @request.auth.id"
		c.ListRule = &rule
		if err := app.Save(c); err != nil {
			t.Fatal(err)
		}

		// Never written to, so `owner` holds the zero value — the shape any row
		// takes before somebody claims it.
		unowned := core.NewRecord(c)
		unowned.Set("marker", "UNOWNED_ROW")
		if err := app.Save(unowned); err != nil {
			t.Fatal(err)
		}

		owned := core.NewRecord(c)
		owned.Set("owner", "4q1xlclmfloku33")
		owned.Set("marker", "OWNED_ROW")
		if err := app.Save(owned); err != nil {
			t.Fatal(err)
		}
	}

	scenarios := []tests.ApiScenario{
		{
			Name:           "anonymous caller sees no rows, not the unowned ones",
			Method:         http.MethodGet,
			URL:            "/v1/collections/rlsprobe/records",
			BeforeTestFunc: setup,
			ExpectedStatus: 200,
			ExpectedContent: []string{
				`"totalItems":0`,
				`"items":[]`,
			},
			NotExpectedContent: []string{
				"UNOWNED_ROW",
				"OWNED_ROW",
			},
			ExpectedEvents: map[string]int{"*": 0, "OnRecordsListRequest": 1},
		},
	}

	for _, scenario := range scenarios {
		scenario.Test(t)
	}
}
