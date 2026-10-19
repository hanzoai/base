package apis_test

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/hanzoai/base/core"
	"github.com/hanzoai/base/tests"
	"github.com/hanzoai/base/tools/types"
)

const (
	// users, test@example.com (id 4q1xlclmfloku33)
	aggregateMemberToken = "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJjb2xsZWN0aW9uSWQiOiJfdXNlcnNfYXV0aF8iLCJleHAiOjI1MjQ2MDQ0NjEsImlkIjoiNHExeGxjbG1mbG9rdTMzIiwicmVmcmVzaGFibGUiOnRydWUsInR5cGUiOiJhdXRoIn0.AuFTIzCsdLEy-5adFzpjZzbqAdTP6Iu9B1wPBAxLBgo"
	aggregateMemberId    = "4q1xlclmfloku33"
	// _superusers, test@example.com
	aggregateSuperToken = "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJjb2xsZWN0aW9uSWQiOiJoYmNfMzE0MjYzNTgyMyIsImV4cCI6MjUyNDYwNDQ2MSwiaWQiOiJzeXdiaGVjbmg0NnJobTAiLCJyZWZyZXNoYWJsZSI6dHJ1ZSwidHlwZSI6ImF1dGgifQ.CXBf8BazmUeg2RnJW8OEs1UFYF41rbCMOa6YZa4wZio"
)

// seedDeals builds a deals collection whose list rule admits a member's own
// deals, four deals (three of them the member's), and two members of one deal.
func seedDeals(t testing.TB, app *tests.TestApp) {
	t.Helper()

	companies := core.NewBaseCollection("firms")
	companies.Fields.Add(&core.NumberField{Name: "employees"})
	if err := app.Save(companies); err != nil {
		t.Fatal(err)
	}

	deals := core.NewBaseCollection("deals")
	deals.ListRule = types.Pointer("owner = @request.auth.id")
	deals.Fields.Add(
		&core.TextField{Name: "title"},
		&core.TextField{Name: "owner"},
		&core.TextField{Name: "secret", Hidden: true},
		&core.NumberField{Name: "amount"},
		&core.RatingField{Name: "rating"},
		&core.BoolField{Name: "won"},
		&core.SelectField{Name: "stage", MaxSelect: 1, Values: []string{"new", "won", "lost"}},
		&core.DateField{Name: "closed"},
		&core.CurrencyField{Name: "money"},
		&core.EmailsField{Name: "emails"},
		&core.RelationField{Name: "company", CollectionId: companies.Id, MaxSelect: 1},
	)
	if err := app.Save(deals); err != nil {
		t.Fatal(err)
	}

	members := core.NewBaseCollection("deal_members")
	members.Fields.Add(
		&core.RelationField{Name: "deal", CollectionId: deals.Id, MaxSelect: 1},
		&core.TextField{Name: "label"},
	)
	if err := app.Save(members); err != nil {
		t.Fatal(err)
	}

	rows := []map[string]any{
		{"title": "A", "owner": aggregateMemberId, "amount": 100, "rating": 5, "won": true, "stage": "new",
			"closed": "2026-10-01 00:00:00.000Z", "money": types.Currency{AmountMicros: 1_500_000, CurrencyCode: "USD"},
			"emails": types.Emails{PrimaryEmail: "a@x.test"}},
		{"title": "B", "owner": aggregateMemberId, "amount": 50, "rating": 3, "won": false, "stage": "new",
			"closed": "2026-09-01 00:00:00.000Z"},
		{"title": "C", "owner": aggregateMemberId, "amount": 30, "won": true, "stage": "won",
			"money": types.Currency{AmountMicros: 9_000_000, CurrencyCode: "EUR"}, "emails": types.Emails{PrimaryEmail: "c@x.test"}},
		{"title": "D", "owner": "someone-else", "amount": 5, "rating": 1, "won": false, "stage": "lost",
			"closed": "2026-11-01 00:00:00.000Z", "emails": types.Emails{PrimaryEmail: "d@x.test"}},
	}

	var first string
	for _, row := range rows {
		r := core.NewRecord(deals)
		for k, v := range row {
			r.Set(k, v)
		}
		if err := app.Save(r); err != nil {
			t.Fatal(err)
		}
		if first == "" {
			first = r.Id
		}
	}

	for _, label := range []string{"one", "done"} {
		r := core.NewRecord(members)
		r.Set("deal", first)
		r.Set("label", label)
		if err := app.Save(r); err != nil {
			t.Fatal(err)
		}
	}
}

func aggregateURL(params map[string]string) string {
	v := url.Values{}
	for k, x := range params {
		v.Set(k, x)
	}
	return "/v1/collections/deals/aggregate?" + v.Encode()
}

func TestRecordsAggregate(t *testing.T) {
	t.Parallel()

	everyOp := "count,sum(amount),avg(amount),min(amount),max(amount),countEmpty(emails),countNotEmpty(emails)," +
		"percentEmpty(emails),percentNotEmpty(emails),countUnique(stage),countTrue(won),countFalse(won)," +
		"sum(money.amountMicros),min(closed),max(closed),avg(rating)"

	scenarios := []tests.ApiScenario{
		{
			Name:           "every operation, as a superuser",
			Method:         http.MethodGet,
			URL:            aggregateURL(map[string]string{"ops": everyOp}),
			Headers:        map[string]string{"Authorization": aggregateSuperToken},
			ExpectedStatus: 200,
			ExpectedContent: []string{
				`"count":4`,
				`"sum(amount)":185`,
				`"avg(amount)":46.25`,
				`"min(amount)":5`,
				`"max(amount)":100`,
				`"countEmpty(emails)":1`,
				`"countNotEmpty(emails)":3`,
				`"percentEmpty(emails)":25`,
				`"percentNotEmpty(emails)":75`,
				`"countUnique(stage)":3`,
				`"countTrue(won)":2`,
				`"countFalse(won)":2`,
				`"sum(money.amountMicros)":10500000`,
				`"min(closed)":"2026-09-01 00:00:00.000Z"`,
				`"max(closed)":"2026-11-01 00:00:00.000Z"`,
				`"avg(rating)":2.25`,
				`"groups":[]`,
			},
		},
		{
			Name:           "grouped by a select, largest group first",
			Method:         http.MethodGet,
			URL:            aggregateURL(map[string]string{"ops": "count,sum(amount),countEmpty(emails)", "group": "stage"}),
			Headers:        map[string]string{"Authorization": aggregateSuperToken},
			ExpectedStatus: 200,
			ExpectedContent: []string{
				`"groups":[{"count":2,"countEmpty(emails)":1,"sum(amount)":150,"value":"new"},` +
					`{"count":1,"countEmpty(emails)":0,"sum(amount)":5,"value":"lost"},` +
					`{"count":1,"countEmpty(emails)":0,"sum(amount)":30,"value":"won"}]`,
				`"total":{"count":4,"countEmpty(emails)":1,"sum(amount)":185}`,
			},
		},
		{
			Name:           "grouped by a composite part",
			Method:         http.MethodGet,
			URL:            aggregateURL(map[string]string{"ops": "count", "group": "money.currencyCode"}),
			Headers:        map[string]string{"Authorization": aggregateSuperToken},
			ExpectedStatus: 200,
			ExpectedContent: []string{
				`{"count":2,"value":null}`,
				`{"count":1,"value":"EUR"}`,
				`{"count":1,"value":"USD"}`,
			},
		},
		{
			Name:           "a member counts only what its list rule admits",
			Method:         http.MethodGet,
			URL:            aggregateURL(map[string]string{"ops": "count,sum(amount)"}),
			Headers:        map[string]string{"Authorization": aggregateMemberToken},
			ExpectedStatus: 200,
			ExpectedContent: []string{
				`"total":{"count":3,"sum(amount)":180}`,
			},
		},
		{
			Name:   "narrowed by filter and where",
			Method: http.MethodGet,
			URL: aggregateURL(map[string]string{
				"ops":    "count,sum(amount)",
				"filter": `amount > 10`,
				"where":  `{"field":"stage","op":"is","value":"new"}`,
			}),
			Headers:         map[string]string{"Authorization": aggregateMemberToken},
			ExpectedStatus:  200,
			ExpectedContent: []string{`"total":{"count":2,"sum(amount)":150}`},
		},
		{
			Name:   "a record joined twice is counted once",
			Method: http.MethodGet,
			URL: aggregateURL(map[string]string{
				"ops":    "count,sum(amount)",
				"filter": `deal_members_via_deal.label ?~ "one"`,
			}),
			Headers:         map[string]string{"Authorization": aggregateSuperToken},
			ExpectedStatus:  200,
			ExpectedContent: []string{`"total":{"count":1,"sum(amount)":100}`},
		},
		{
			Name:            "a figure through a relation is refused",
			Method:          http.MethodGet,
			URL:             aggregateURL(map[string]string{"ops": "sum(company.employees)"}),
			Headers:         map[string]string{"Authorization": aggregateSuperToken},
			ExpectedStatus:  400,
			ExpectedContent: []string{`"status":400`},
		},
		{
			Name:            "a hidden field is refused to a member",
			Method:          http.MethodGet,
			URL:             aggregateURL(map[string]string{"ops": "countEmpty(secret)"}),
			Headers:         map[string]string{"Authorization": aggregateMemberToken},
			ExpectedStatus:  400,
			ExpectedContent: []string{`"status":400`},
		},
		{
			Name:            "a hidden field is a superuser's",
			Method:          http.MethodGet,
			URL:             aggregateURL(map[string]string{"ops": "countEmpty(secret)"}),
			Headers:         map[string]string{"Authorization": aggregateSuperToken},
			ExpectedStatus:  200,
			ExpectedContent: []string{`"countEmpty(secret)":4`},
		},
		{
			Name:            "a sum of text is refused",
			Method:          http.MethodGet,
			URL:             aggregateURL(map[string]string{"ops": "sum(title)"}),
			Headers:         map[string]string{"Authorization": aggregateSuperToken},
			ExpectedStatus:  400,
			ExpectedContent: []string{`"status":400`},
		},
		{
			Name:            "an unknown operation is refused",
			Method:          http.MethodGet,
			URL:             aggregateURL(map[string]string{"ops": "median(amount)"}),
			Headers:         map[string]string{"Authorization": aggregateSuperToken},
			ExpectedStatus:  400,
			ExpectedContent: []string{`"status":400`},
		},
		{
			Name:            "a where that does not compile is refused",
			Method:          http.MethodGet,
			URL:             aggregateURL(map[string]string{"ops": "count", "where": `{"field":"nope","op":"is","value":1}`}),
			Headers:         map[string]string{"Authorization": aggregateSuperToken},
			ExpectedStatus:  400,
			ExpectedContent: []string{`"status":400`},
		},
		{
			Name:            "a guest is refused where only a superuser may list",
			Method:          http.MethodGet,
			URL:             "/v1/collections/demo2/aggregate?ops=count",
			ExpectedStatus:  403,
			ExpectedContent: []string{`"status":403`},
		},
	}

	for _, s := range scenarios {
		s.BeforeTestFunc = func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
			seedDeals(t, app)
			if c, err := app.FindCollectionByNameOrId("demo2"); err == nil {
				c.ListRule = nil
				if err := app.Save(c); err != nil {
					t.Fatal(err)
				}
			}
		}
		s.Test(t)
	}
}

func TestRecordsListWhere(t *testing.T) {
	t.Parallel()

	scenarios := []tests.ApiScenario{
		{
			Name:            "where narrows a member's list, still under its list rule",
			Method:          http.MethodGet,
			URL:             "/v1/collections/deals/records?where=" + url.QueryEscape(`{"field":"won","op":"is","value":true}`),
			Headers:         map[string]string{"Authorization": aggregateMemberToken},
			ExpectedStatus:  200,
			ExpectedContent: []string{`"totalItems":2`, `"title":"A"`, `"title":"C"`},
		},
		{
			Name:            "where beside a filter",
			Method:          http.MethodGet,
			URL:             "/v1/collections/deals/records?filter=" + url.QueryEscape(`amount < 60`) + "&where=" + url.QueryEscape(`{"field":"emails","op":"empty"}`),
			Headers:         map[string]string{"Authorization": aggregateMemberToken},
			ExpectedStatus:  200,
			ExpectedContent: []string{`"totalItems":1`, `"title":"B"`},
		},
		{
			Name:            "a where that does not compile is refused",
			Method:          http.MethodGet,
			URL:             "/v1/collections/deals/records?where=" + url.QueryEscape(`{"field":"title","op":"gt","value":"x"}`),
			Headers:         map[string]string{"Authorization": aggregateMemberToken},
			ExpectedStatus:  400,
			ExpectedContent: []string{`"status":400`},
		},
		{
			Name:            "a where cannot name a hidden field",
			Method:          http.MethodGet,
			URL:             "/v1/collections/deals/records?where=" + url.QueryEscape(`{"field":"secret","op":"empty"}`),
			Headers:         map[string]string{"Authorization": aggregateMemberToken},
			ExpectedStatus:  400,
			ExpectedContent: []string{`"status":400`},
		},
	}

	for _, s := range scenarios {
		s.BeforeTestFunc = func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
			seedDeals(t, app)
		}
		s.Test(t)
	}
}
