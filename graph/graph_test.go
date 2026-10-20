package graph_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/hanzoai/base/apis"
	"github.com/hanzoai/base/core"
	"github.com/hanzoai/base/graph"
	"github.com/hanzoai/base/modules"
	"github.com/hanzoai/base/modules/crm"
	"github.com/hanzoai/base/tests"
	"github.com/hanzoai/base/tools/types"
	"github.com/zap-proto/zip"
)

// fx is one org's Base with the CRM installed and served the way a host serves
// it: the REST router, and the graph mounted on zip's engine beside it.
type fx struct {
	app *tests.TestApp
	mux http.Handler
	zip *zip.App
	iam *tests.IAM
	ann string // a member of acme
	bob string // an admin of acme
	zed string // the owner of zoo, another org
	ids map[string]string
}

func setup(t *testing.T) *fx {
	t.Helper()

	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(app.Cleanup)

	if _, err := modules.Install(app, crm.Name); err != nil {
		t.Fatal(err)
	}

	iam := tests.NewIAM(t)
	app.Store().Set(apis.StoreKeyExternalAuthOnly, true)
	app.Store().Set(apis.StoreKeyJWKSURL, iam.URL)
	app.Store().Set(apis.StoreKeyTenant, "acme")

	member := func(sub, name, org, role string) string {
		return iam.Token(t, jwt.MapClaims{"sub": sub, "email": name + "@" + org + ".test", "name": name,
			"orgs": []any{map[string]any{"org": org, "role": role}}})
	}

	f := &fx{
		app: app, iam: iam, ids: map[string]string{},
		ann: member("acme/ann", "Ann", "acme", "member"),
		bob: member("acme/bob", "Bob", "acme", "admin"),
		zed: member("zoo/zed", "Zed", "zoo", "owner"),
	}
	f.mux = tests.Router(t, app)

	g := graph.New(app, f.mux)
	f.zip = zip.New(zip.Config{DisableStartupMessage: true})
	f.zip.MountGraphFor("/v1/graphql", g.Schema, g.Resolve)
	return f
}

type answer struct {
	Data   map[string]any   `json:"data"`
	Errors []zip.GraphError `json:"errors"`
	status int
}

func (a answer) messages() string {
	var out []string
	for _, e := range a.Errors {
		out = append(out, e.Message)
	}
	return strings.Join(out, " | ")
}

// gql posts one GraphQL request as the bearer of token ("" for nobody).
func (f *fx) gql(t *testing.T, token, query string, vars map[string]any) answer {
	t.Helper()

	body, _ := json.Marshal(map[string]any{"query": query, "variables": vars})
	req := httptest.NewRequest(http.MethodPost, "/v1/graphql", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	res, err := f.zip.Test(req, zip.TestConfig{Timeout: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)

	var a answer
	if err := json.Unmarshal(raw, &a); err != nil {
		t.Fatalf("not a GraphQL answer (%d): %s", res.StatusCode, raw)
	}
	a.status = res.StatusCode
	return a
}

// ok runs a request that must answer without an error.
func (f *fx) ok(t *testing.T, token, query string, vars map[string]any) map[string]any {
	t.Helper()
	a := f.gql(t, token, query, vars)
	if len(a.Errors) > 0 || a.Data == nil {
		t.Fatalf("errors: %s\n%s", a.messages(), query)
	}
	return a.Data
}

// rest makes one REST request of the same Base, as the bearer of token.
func (f *fx) rest(t *testing.T, token, method, path, body string) (int, map[string]any) {
	t.Helper()

	var rd io.Reader
	if body != "" {
		rd = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, rd)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	f.mux.ServeHTTP(w, req)
	var out map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	return w.Code, out
}

// save writes a record through the Go API.
func (f *fx) save(t *testing.T, coll string, data map[string]any) string {
	t.Helper()
	c, err := f.app.FindCollectionByNameOrId(coll)
	if err != nil {
		t.Fatal(err)
	}
	r := core.NewRecord(c)
	for k, v := range data {
		r.Set(k, v)
	}
	if err := f.app.Save(r); err != nil {
		t.Fatalf("save %s: %v", coll, err)
	}
	return r.Id
}

// people is the fixture's records: two companies, three people, a list with
// Ada on it, a segment of the leads and the qualified, an opportunity and a
// note.
func (f *fx) people(t *testing.T) map[string]string {
	t.Helper()
	ids := f.ids
	ids["acme"] = f.save(t, "companies", map[string]any{
		"name": "Acme", "employees": 50, "icp": true,
		"annual_revenue": types.Currency{AmountMicros: 5_000_000_000_000, CurrencyCode: "USD"},
		"address":        types.Address{AddressCity: "London", AddressCountry: "GB"},
	})
	ids["globex"] = f.save(t, "companies", map[string]any{
		"name": "Globex", "employees": 500,
		"annual_revenue": types.Currency{AmountMicros: 2_000_000_000_000, CurrencyCode: "EUR"},
	})
	ids["ada"] = f.save(t, "people", map[string]any{
		"name": types.FullName{FirstName: "Ada", LastName: "Lovelace"}, "job_title": "Engineer",
		"emails": types.Emails{PrimaryEmail: "ada@acme.test"}, "phones": types.Phones{PrimaryPhoneNumber: "+1 (415) 555-0100"},
		"company": ids["acme"], "lifecycle": "lead", "consent": map[string]any{"email": true},
	})
	ids["grace"] = f.save(t, "people", map[string]any{
		"name": types.FullName{FirstName: "Grace", LastName: "Hopper"}, "job_title": "Admiral",
		"emails":  types.Emails{PrimaryEmail: "grace@globex.test", AdditionalEmails: []string{"g@navy.mil"}},
		"company": ids["globex"], "lifecycle": "customer",
	})
	ids["alan"] = f.save(t, "people", map[string]any{
		"name": types.FullName{FirstName: "Alan", LastName: "Turing"}, "lifecycle": "qualified",
	})
	ids["list"] = f.save(t, "lists", map[string]any{"name": "Launch", "object": "people"})
	f.save(t, "list_members", map[string]any{"list": ids["list"], "person": ids["ada"], "source": "manual"})
	ids["segment"] = f.save(t, "segments", map[string]any{
		"name": "Warm", "object": "people",
		"where": map[string]any{"field": "lifecycle", "op": "is", "value": []any{"lead", "qualified"}},
	})
	ids["deal"] = f.save(t, "opportunities", map[string]any{
		"name": "Acme expansion", "stage": "proposal", "close_date": time.Now().UTC().Format("2006-01-02") + " 12:00:00.000Z",
		"amount": types.Currency{AmountMicros: 120_000_000_000, CurrencyCode: "USD"}, "company": ids["acme"], "contact": ids["ada"],
	})
	f.save(t, "opportunities", map[string]any{
		"name": "Globex pilot", "stage": "new",
		"amount": types.Currency{AmountMicros: 30_000_000_000, CurrencyCode: "EUR"}, "company": ids["globex"],
	})
	ids["note"] = f.save(t, "notes", map[string]any{"title": "Met Ada", "people": []string{ids["ada"]}})
	return ids
}

// names reads the first names off a people connection.
func names(t *testing.T, conn any) []string {
	t.Helper()
	var out []string
	edges, _ := conn.(map[string]any)["edges"].([]any)
	for _, e := range edges {
		node := e.(map[string]any)["node"].(map[string]any)
		name, _ := node["name"].(map[string]any)
		out = append(out, fmt.Sprint(name["firstName"]))
	}
	return out
}

func sorted(s []string) []string {
	s = slices.Clone(s)
	sort.Strings(s)
	return s
}

// ---------------------------------------------------------------------------

// The schema is the Base's own: a Base with the CRM types its people, one
// without does not, a field added to a collection is in the next schema, and
// each is a schema a client can build against.
func TestTheSchemaIsEachBasesOwn(t *testing.T) {
	t.Parallel()

	f := setup(t)

	plain, err := tests.NewTestApp()
	if err != nil {
		t.Fatal(err)
	}
	defer plain.Cleanup()
	other := graph.New(plain, tests.Router(t, plain))

	crmSDL, err := graph.New(f.app, f.mux).Schema(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	plainSDL, err := other.Schema(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	for _, sdl := range []string{crmSDL, plainSDL} {
		if err := zip.CheckSchema(sdl); err != nil {
			t.Fatalf("not a schema a client can build against: %v\n%s", err, sdl)
		}
	}
	for _, want := range []string{
		"type BasePerson {", "people(filter: BasePersonFilter", "person(id: ID!): BasePerson",
		"createPerson(data: BasePersonInput!): BasePerson", "name: BaseFullName",
		"company: BaseCompany", "list_members_via_person: BaseListMemberFilter",
		"opportunities_via_contact(filter: BaseOpportunityFilter", "history(first: Int, after: String): BaseHistoryConnection!",
		"inSegment: ID", "input BaseCompanyRelationFilter {", "peopleGroupBy(field: String!, filter: BasePersonFilter): [BaseGroup!]!",
	} {
		if !strings.Contains(crmSDL, want) {
			t.Errorf("the CRM schema lacks %q", want)
		}
	}
	if strings.Contains(plainSDL, "BasePerson") || strings.Contains(plainSDL, "inSegment") {
		t.Fatal("a Base without the CRM has its types")
	}
	if !strings.Contains(plainSDL, "type BaseDemo1 {") {
		t.Fatal("a Base's own collections are not in its schema")
	}
	// no type a client should not see
	for _, never := range []string{"_superusers", "BaseUser ", "_history", "BaseUsers"} {
		if strings.Contains(crmSDL, never) {
			t.Errorf("the schema names %q", never)
		}
	}

	people, err := f.app.FindCollectionByNameOrId("people")
	if err != nil {
		t.Fatal(err)
	}
	people.Fields.Add(&core.TextField{Name: "nickname"})
	if err := f.app.Save(people); err != nil {
		t.Fatal(err)
	}
	a := f.ok(t, f.ann, `{ people { totalCount edges { node { nickname } } } }`, nil)
	if a["people"].(map[string]any)["totalCount"] != float64(0) {
		t.Fatalf("a field added to a collection is not in the graph: %v", a)
	}
}

// Each field type's operators select what the where clause they compile to
// selects.
func TestFiltersPerFieldType(t *testing.T) {
	t.Parallel()

	f := setup(t)
	ids := f.people(t)

	for _, s := range []struct {
		name   string
		filter string
		want   []string
	}{
		{"text is", `{job_title: {is: ["Engineer"]}}`, []string{"Ada"}},
		{"text contains ignores case", `{job_title: {contains: ["MIR"]}}`, []string{"Grace"}},
		{"text does not contain, empty included", `{job_title: {notContains: ["mir"]}}`, []string{"Ada", "Alan"}},
		{"text empty", `{job_title: {empty: true}}`, []string{"Alan"}},
		{"text not empty", `{job_title: {empty: false}}`, []string{"Ada", "Grace"}},
		{"select any of", `{lifecycle: {is: ["lead", "customer"]}}`, []string{"Ada", "Grace"}},
		{"select none of", `{lifecycle: {isNot: ["lead"]}}`, []string{"Alan", "Grace"}},
		{"a single value where a list is taken", `{lifecycle: {is: "lead"}}`, []string{"Ada"}},
		{"composite contains", `{emails: {contains: ["globex"]}}`, []string{"Grace"}},
		{"composite does not contain", `{emails: {notContains: ["acme"]}}`, []string{"Alan", "Grace"}},
		{"phone digits", `{phones: {contains: ["415 555"]}}`, []string{"Ada"}},
		{"composite part", `{emails: {primaryEmail: {is: ["ada@acme.test"]}}}`, []string{"Ada"}},
		{"composite list part", `{emails: {additionalEmails: {contains: ["navy"]}}}`, []string{"Grace"}},
		{"full name part", `{name: {lastName: {is: ["Turing"]}}}`, []string{"Alan"}},
		{"composite empty", `{address: {empty: true}}`, []string{"Ada", "Alan", "Grace"}},
		{"json contains", `{consent: {contains: ["email"]}}`, []string{"Ada"}},
		{"relation is", `{company: {is: ["` + ids["acme"] + `"]}}`, []string{"Ada"}},
		{"relation empty", `{company: {empty: true}}`, []string{"Alan"}},
		{"through a relation", `{company: {where: {name: {is: ["Globex"]}}}}`, []string{"Grace"}},
		{"through a relation's composite", `{company: {where: {annual_revenue: {currencyCode: {is: ["USD"]}}}}}`, []string{"Ada"}},
		{"in a list", `{list_members_via_person: {list: {is: ["` + ids["list"] + `"]}}}`, []string{"Ada"}},
		{"not in a list", `{not: {list_members_via_person: {list: {is: ["` + ids["list"] + `"]}}}}`, []string{"Alan", "Grace"}},
		{"in a segment", `{inSegment: "` + ids["segment"] + `"}`, []string{"Ada", "Alan"}},
		{"not in a segment", `{notInSegment: "` + ids["segment"] + `"}`, []string{"Grace"}},
		{"id is", `{id: {is: ["` + ids["ada"] + `"]}}`, []string{"Ada"}},
		{"id is not", `{id: {isNot: ["` + ids["ada"] + `"]}}`, []string{"Alan", "Grace"}},
		{"created today", `{created: {today: true}}`, []string{"Ada", "Alan", "Grace"}},
		{"created not today", `{created: {today: false}}`, nil},
		{"created this week, weeks from Sunday", `{created: {relative: {direction: "this", unit: "week"}, weekStart: 0}}`, []string{"Ada", "Alan", "Grace"}},
		{"created in the past", `{created: {past: true}}`, []string{"Ada", "Alan", "Grace"}},
		{"created after a day", `{created: {gt: "2000-01-01"}}`, []string{"Ada", "Alan", "Grace"}},
		{"created before a day", `{created: {lt: "2000-01-01"}}`, nil},
		{"an or of ands", `{or: [{and: [{lifecycle: {is: ["lead"]}}, {company: {is: ["` + ids["acme"] + `"]}}]}, {name: {lastName: {is: ["Turing"]}}}]}`, []string{"Ada", "Alan"}},
		{"keys of one object hold together", `{lifecycle: {is: ["lead", "customer"]}, job_title: {is: ["Admiral"]}}`, []string{"Grace"}},
		{"not of an or", `{not: {or: [{lifecycle: {is: ["lead"]}}, {lifecycle: {is: ["customer"]}}]}}`, []string{"Alan"}},
		{"an empty filter selects all", `{}`, []string{"Ada", "Alan", "Grace"}},
	} {
		t.Run(s.name, func(t *testing.T) {
			a := f.ok(t, f.ann, `{ people(filter: `+s.filter+`) { edges { node { name { firstName } } } } }`, nil)
			if got := sorted(names(t, a["people"])); strings.Join(got, ",") != strings.Join(s.want, ",") {
				t.Fatalf("got %v, want %v", got, s.want)
			}
		})
	}

	for _, s := range []struct {
		name   string
		coll   string
		filter string
		want   int
	}{
		{"number above", "companies", `{employees: {gt: 100}}`, 1},
		{"number at most", "companies", `{employees: {lte: 50}}`, 1},
		{"number one of", "companies", `{employees: {is: [50, 500]}}`, 2},
		{"bool", "companies", `{icp: {is: true}}`, 1},
		{"currency amount", "companies", `{annual_revenue: {amountMicros: {gte: 3000000000000}}}`, 1},
		{"currency code", "companies", `{annual_revenue: {currencyCode: {is: ["EUR"]}}}`, 1},
		{"address part", "companies", `{address: {addressCity: {is: ["London"]}}}`, 1},
		{"address contains", "companies", `{address: {contains: ["london"]}}`, 1},
		{"a date on a day", "opportunities", `{close_date: {is: "` + time.Now().UTC().Format("2006-01-02") + `"}}`, 1},
		{"a multi-relation holds one", "notes", `{people: {is: ["` + ids["ada"] + `"]}}`, 1},
		{"a back-relation's field", "companies", `{opportunities_via_company: {stage: {is: ["proposal"]}}}`, 1},
	} {
		t.Run(s.name, func(t *testing.T) {
			a := f.ok(t, f.ann, `{ c: `+s.coll+`(filter: `+s.filter+`) { totalCount } }`, nil)
			if got := a["c"].(map[string]any)["totalCount"]; got != float64(s.want) {
				t.Fatalf("got %v, want %d", got, s.want)
			}
		})
	}

	for _, s := range []struct{ name, filter, says string }{
		{"a field the collection has not", `{nope: {is: ["x"]}}`, `BasePersonFilter has no field "nope"`},
		{"an operator the type has not", `{lifecycle: {gt: "a"}}`, `has no operator "gt"`},
		{"a segment through a relation", `{company: {where: {inSegment: "x"}}}`, "not records reached through company"},
		{"a regex is not offered", `{job_title: {regex: "^A"}}`, `has no operator "regex"`},
	} {
		t.Run(s.name, func(t *testing.T) {
			a := f.gql(t, f.ann, `{ people(filter: `+s.filter+`) { totalCount } }`, nil)
			if !strings.Contains(a.messages(), s.says) {
				t.Fatalf("errors %q, want one saying %q", a.messages(), s.says)
			}
		})
	}

	t.Run("@me is the caller", func(t *testing.T) {
		created := f.ok(t, f.ann, `mutation { createOpportunity(data: {name: "Mine", stage: "new"}) { id created_by { id } } }`, nil)
		me := created["createOpportunity"].(map[string]any)["created_by"].(map[string]any)["id"]
		id := created["createOpportunity"].(map[string]any)["id"]
		f.ok(t, f.ann, `mutation($id: ID!, $me: String) { updateOpportunity(id: $id, data: {owner: $me}) { id } }`, map[string]any{"id": id, "me": me})

		a := f.ok(t, f.ann, `{ opportunities(filter: {owner: {is: ["@me"]}}) { edges { node { name } } } }`, nil)
		edges := a["opportunities"].(map[string]any)["edges"].([]any)
		if len(edges) != 1 || edges[0].(map[string]any)["node"].(map[string]any)["name"] != "Mine" {
			t.Fatalf("@me selected %v", edges)
		}
		b := f.ok(t, f.bob, `{ opportunities(filter: {owner: {is: ["@me"]}}) { totalCount } }`, nil)
		if b["opportunities"].(map[string]any)["totalCount"] != float64(0) {
			t.Fatal("@me selected another caller's record")
		}
	})
}

// A connection pages in the order asked for, a cursor is a position in it, and
// a total is counted only when asked for.
func TestOrderAndPages(t *testing.T) {
	t.Parallel()

	f := setup(t)
	f.people(t)

	order := func(by string) []string {
		a := f.ok(t, f.ann, `{ people(orderBy: `+by+`) { edges { node { name { firstName } } } } }`, nil)
		return names(t, a["people"])
	}
	if got := order(`[{name: {firstName: Asc}}]`); strings.Join(got, ",") != "Ada,Alan,Grace" {
		t.Fatalf("ascending: %v", got)
	}
	if got := order(`[{name: {firstName: Desc}}]`); strings.Join(got, ",") != "Grace,Alan,Ada" {
		t.Fatalf("descending: %v", got)
	}
	if got := order(`[{lifecycle: Asc}, {name: {lastName: Desc}}]`); strings.Join(got, ",") != "Grace,Ada,Alan" {
		t.Fatalf("two keys: %v", got)
	}
	if got := order(`[{company: {name: Desc}}]`); strings.Join(got[:2], ",") != "Grace,Ada" {
		t.Fatalf("through a relation: %v", got)
	}
	for by, says := range map[string]string{
		`[{name: {firstName: Asc}, lifecycle: Asc}]`: "names one field",
		`[{emails: {additionalEmails: Asc}}]`:        `no part "additionalEmails"`,
		`[{consent: Asc}]`:                           `no field "consent"`,
	} {
		if a := f.gql(t, f.ann, `{ people(orderBy: `+by+`) { totalCount } }`, nil); !strings.Contains(a.messages(), says) {
			t.Errorf("%s: errors %q, want %q", by, a.messages(), says)
		}
	}

	// seven activities, paged three at a time
	for i := range 7 {
		f.save(t, "activities", map[string]any{"kind": "note", "subject": fmt.Sprintf("a%d", i), "source_id": fmt.Sprintf("s%d", i)})
	}
	const page = `query($after: String) { activities(first: 3, after: $after, orderBy: [{subject: Asc}]) {
		totalCount edges { cursor node { subject } } pageInfo { hasNextPage hasPreviousPage startCursor endCursor } } }`
	var seen []string
	var after any
	for range 4 {
		a := f.ok(t, f.ann, page, map[string]any{"after": after})
		conn := a["activities"].(map[string]any)
		if conn["totalCount"] != float64(7) {
			t.Fatalf("totalCount %v", conn["totalCount"])
		}
		for _, e := range conn["edges"].([]any) {
			seen = append(seen, e.(map[string]any)["node"].(map[string]any)["subject"].(string))
		}
		info := conn["pageInfo"].(map[string]any)
		if info["hasPreviousPage"] != (after != nil) {
			t.Fatalf("hasPreviousPage %v after %v", info["hasPreviousPage"], after)
		}
		if info["hasNextPage"] != true {
			break
		}
		after = info["endCursor"]
	}
	if strings.Join(seen, ",") != "a0,a1,a2,a3,a4,a5,a6" {
		t.Fatalf("paging read %v", seen)
	}

	a := f.ok(t, f.ann, `{ activities(last: 2, orderBy: [{subject: Asc}]) { edges { cursor node { subject } } pageInfo { hasNextPage hasPreviousPage } } }`, nil)
	conn := a["activities"].(map[string]any)
	edges := conn["edges"].([]any)
	if len(edges) != 2 || edges[0].(map[string]any)["node"].(map[string]any)["subject"] != "a5" {
		t.Fatalf("last 2: %v", edges)
	}
	before := edges[0].(map[string]any)["cursor"]
	b := f.ok(t, f.ann, `query($b: String) { activities(last: 2, before: $b, orderBy: [{subject: Asc}]) { edges { node { subject } } } }`, map[string]any{"b": before})
	got := []string{}
	for _, e := range b["activities"].(map[string]any)["edges"].([]any) {
		got = append(got, e.(map[string]any)["node"].(map[string]any)["subject"].(string))
	}
	if strings.Join(got, ",") != "a3,a4" {
		t.Fatalf("last 2 before a5: %v", got)
	}

	if a := f.gql(t, f.ann, `{ activities(after: "nope") { totalCount } }`, nil); !strings.Contains(a.messages(), "not a cursor") {
		t.Fatalf("a forged cursor: %q", a.messages())
	}
	if a := f.gql(t, f.ann, `{ activities(first: 101) { totalCount } }`, nil); !strings.Contains(a.messages(), "from 0 to 100") {
		t.Fatalf("a page too large: %q", a.messages())
	}
}

// Figures are the aggregate route's, over what the filter selects, and a group
// by is one request for every figure its groups read.
func TestAggregates(t *testing.T) {
	t.Parallel()

	f := setup(t)
	f.people(t)

	a := f.ok(t, f.ann, `{ companies { aggregate {
		count total: sum(field: "employees") avg(field: "employees") min(field: "employees") max(field: "employees")
		countEmpty(field: "address") percentEmpty(field: "address") countTrue(field: "icp")
		revenue: sum(field: "annual_revenue.amountMicros")
	} } }`, nil)
	agg := a["companies"].(map[string]any)["aggregate"].(map[string]any)
	for k, want := range map[string]float64{
		"count": 2, "total": 550, "avg": 275, "min": 50, "max": 500, "countEmpty": 1, "percentEmpty": 50, "countTrue": 1, "revenue": 7_000_000_000_000,
	} {
		if agg[k] != want {
			t.Errorf("%s = %v, want %v", k, agg[k], want)
		}
	}

	b := f.ok(t, f.ann, `{ companies(filter: {icp: {is: true}}, first: 1) { totalCount aggregate { count } } }`, nil)
	if b["companies"].(map[string]any)["aggregate"].(map[string]any)["count"] != float64(1) {
		t.Fatalf("an aggregate ignores the filter: %v", b)
	}

	g := f.ok(t, f.ann, `{ opportunitiesGroupBy(field: "stage") { value aggregate { count sum(field: "amount.amountMicros") } } }`, nil)
	lanes := map[string]any{}
	for _, x := range g["opportunitiesGroupBy"].([]any) {
		m := x.(map[string]any)
		lanes[fmt.Sprint(m["value"])] = m["aggregate"]
	}
	if lanes["proposal"].(map[string]any)["sum"] != float64(120_000_000_000) || lanes["new"].(map[string]any)["count"] != float64(1) {
		t.Fatalf("lanes %v", lanes)
	}

	h := f.ok(t, f.ann, `{ peopleGroupBy(field: "lifecycle", filter: {company: {empty: false}}) { value aggregate { count } } }`, nil)
	if n := len(h["peopleGroupBy"].([]any)); n != 2 {
		t.Fatalf("a group by ignores its filter: %v", h)
	}

	if a := f.gql(t, f.ann, `{ people { aggregate { sum(field: "company.employees") } } }`, nil); !strings.Contains(a.messages(), "reads a field of") {
		t.Fatalf("an aggregate through a relation: %q", a.messages())
	}
}

// Writes are the record routes': what REST would store, as the caller,
// stamped with them, under the collection's rules.
func TestMutations(t *testing.T) {
	t.Parallel()

	f := setup(t)
	ids := f.people(t)

	created := f.ok(t, f.ann, `mutation($c: ID) { createPerson(data: {
		name: {firstName: "Linus", lastName: "Torvalds"}, emails: {primaryEmail: "Linus@Acme.test"}, company: $c, lifecycle: "lead"
	}) { id name { firstName } emails { primaryEmail } company { name } created_by { name source } } }`, map[string]any{"c": ids["acme"]})
	p := created["createPerson"].(map[string]any)
	id := p["id"].(string)
	if p["company"].(map[string]any)["name"] != "Acme" || p["created_by"].(map[string]any)["name"] != "Ann" ||
		p["emails"].(map[string]any)["primaryEmail"] != "linus@acme.test" {
		t.Fatalf("created %v", p)
	}

	// what REST reads back is what was written
	code, rec := f.rest(t, f.ann, http.MethodGet, "/v1/collections/people/records/"+id, "")
	if code != 200 || rec["lifecycle"] != "lead" || rec["name"].(map[string]any)["lastName"] != "Torvalds" {
		t.Fatalf("REST reads %d %v", code, rec)
	}

	updated := f.ok(t, f.ann, `mutation($id: ID!) { updatePerson(id: $id, data: {lifecycle: "customer"}) { lifecycle } }`, map[string]any{"id": id})
	if updated["updatePerson"].(map[string]any)["lifecycle"] != "customer" {
		t.Fatalf("updated %v", updated)
	}

	up := f.ok(t, f.ann, `mutation { upsertPerson(on: "emails.primaryEmail", data: {emails: {primaryEmail: "linus@acme.test"}, job_title: "Kernel"}) { id job_title } }`, nil)
	if u := up["upsertPerson"].(map[string]any); u["id"] != id || u["job_title"] != "Kernel" {
		t.Fatalf("an upsert on a held value made %v", u)
	}
	fresh := f.ok(t, f.ann, `mutation { upsertPerson(on: "emails.primaryEmail", data: {emails: {primaryEmail: "new@acme.test"}}) { id } }`, nil)
	if fresh["upsertPerson"].(map[string]any)["id"] == id {
		t.Fatal("an upsert on a new value updated another record")
	}
	byID := `mutation { upsertPerson(data: {id: "upsertedperson1", job_title: "First"}) { id job_title } }`
	f.ok(t, f.ann, byID, nil)
	again := f.ok(t, f.ann, strings.Replace(byID, "First", "Second", 1), nil)
	if again["upsertPerson"].(map[string]any)["job_title"] != "Second" {
		t.Fatalf("an upsert by id did not update: %v", again)
	}

	hist := f.ok(t, f.ann, `query($id: ID!) { person(id: $id) { history { totalCount edges { node { action actor { name } changes } } } } }`, map[string]any{"id": id})
	h := hist["person"].(map[string]any)["history"].(map[string]any)
	edges := h["edges"].([]any)
	if len(edges) == 0 || edges[0].(map[string]any)["node"].(map[string]any)["actor"].(map[string]any)["name"] != "Ann" {
		t.Fatalf("history %v", h)
	}
	if !strings.Contains(fmt.Sprint(edges[0].(map[string]any)["node"].(map[string]any)["changes"]), "job_title") {
		t.Fatalf("the newest entry is not the last write: %v", edges[0])
	}

	deleted := f.ok(t, f.ann, `mutation($id: ID!) { deletePerson(id: $id) }`, map[string]any{"id": id})
	if deleted["deletePerson"] != id {
		t.Fatalf("deleted %v", deleted)
	}
	gone := f.ok(t, f.ann, `query($id: ID!) { person(id: $id) { id } }`, map[string]any{"id": id})
	if gone["person"] != nil {
		t.Fatalf("a deleted person reads %v", gone)
	}

	bad := f.gql(t, f.ann, `mutation { createPerson(data: {emails: {primaryEmail: "not an email"}}) { id } }`, nil)
	_, restBad := f.rest(t, f.ann, http.MethodPost, "/v1/collections/people/records", `{"emails":{"primaryEmail":"not an email"}}`)
	if !strings.Contains(bad.messages(), strings.TrimSuffix(fmt.Sprint(restBad["message"]), ".")) || !strings.Contains(bad.messages(), "emails") {
		t.Fatalf("a refused write says %q; REST said %v", bad.messages(), restBad)
	}

	for q, says := range map[string]string{
		`mutation { createPerson(data: {nope: "x"}) { id } }`:                    `BasePersonInput has no field "nope"`,
		`mutation { createPerson(data: {name: {middle: "x"}}) { id } }`:          `BaseFullNameInput has no field "middle"`,
		`mutation { createPerson(data: {created_by: {name: "forged"}}) { id } }`: `has no field "created_by"`,
		`{ createPerson(data: {job_title: "x"}) { id } }`:                        "is a mutation field; this operation is a query",
		`mutation { people { totalCount } }`:                                     "is a query field; this operation is a mutation",
		`mutation { upsertPerson(on: "emails.primaryEmail", data: {}) { id } }`:  "needs data to give it a value",
	} {
		if a := f.gql(t, f.ann, q, nil); !strings.Contains(a.messages(), says) {
			t.Errorf("%s: errors %q, want %q", q, a.messages(), says)
		}
	}
}

// The graph refuses what REST refuses, in REST's words: a member a rule keeps
// out, a collection only a superuser may list, and a token of another org.
func TestTheGraphRefusesWhatRESTRefuses(t *testing.T) {
	t.Parallel()

	f := setup(t)
	f.people(t)

	admins := types.Pointer(`@request.auth.admin = true`)
	secrets := core.NewBaseCollection("secrets")
	secrets.ListRule, secrets.ViewRule, secrets.CreateRule = admins, admins, admins
	secrets.Fields.Add(&core.TextField{Name: "title"})
	vault := core.NewBaseCollection("vault")
	vault.Fields.Add(&core.TextField{Name: "title"})
	for _, c := range []*core.Collection{secrets, vault} {
		if err := f.app.Save(c); err != nil {
			t.Fatal(err)
		}
	}
	f.save(t, "secrets", map[string]any{"title": "the plan"})

	count := func(token string) any {
		return f.ok(t, token, `{ secrets { totalCount } }`, nil)["secrets"].(map[string]any)["totalCount"]
	}
	if count(f.ann) != float64(0) || count(f.bob) != float64(1) {
		t.Fatalf("a member reads %v and an admin %v of what only admins list", count(f.ann), count(f.bob))
	}
	if _, rest := f.rest(t, f.ann, http.MethodGet, "/v1/collections/secrets/records", ""); rest["totalItems"] != float64(0) {
		t.Fatalf("REST shows a member %v", rest)
	}

	for _, s := range []struct {
		name, token, query, method, path, body string
	}{
		{"a member's create a rule refuses", f.ann, `mutation { createSecret(data: {title: "x"}) { id } }`, http.MethodPost, "/v1/collections/secrets/records", `{"title":"x"}`},
		{"a collection only a superuser lists", f.bob, `{ vault { edges { node { id } } } }`, http.MethodGet, "/v1/collections/vault/records", ""},
		{"a token of another org", f.zed, `{ people { edges { node { id } } } }`, http.MethodGet, "/v1/collections/people/records", ""},
	} {
		t.Run(s.name, func(t *testing.T) {
			code, rest := f.rest(t, s.token, s.method, s.path, s.body)
			if code < 400 {
				t.Fatalf("REST answered %d", code)
			}
			a := f.gql(t, s.token, s.query, nil)
			msg := strings.TrimSuffix(fmt.Sprint(rest["message"]), ".")
			if len(a.Errors) == 0 || !strings.Contains(a.messages(), msg) {
				t.Fatalf("the graph says %q where REST said %d %q", a.messages(), code, msg)
			}
			for _, v := range a.Data {
				if v != nil {
					t.Fatalf("a refusal answered %v", a.Data)
				}
			}
		})
	}

	t.Run("nobody signed in", func(t *testing.T) {
		a := f.ok(t, "", `{ people { totalCount } }`, nil)
		if a["people"].(map[string]any)["totalCount"] != float64(0) {
			t.Fatalf("an anonymous caller reads %v", a)
		}
	})

	t.Run("a record the caller may not view is not expanded", func(t *testing.T) {
		secret := f.save(t, "secrets", map[string]any{"title": "linked"})
		notes, err := f.app.FindCollectionByNameOrId("notes")
		if err != nil {
			t.Fatal(err)
		}
		secretsColl, _ := f.app.FindCollectionByNameOrId("secrets")
		notes.Fields.Add(&core.RelationField{Name: "secret", CollectionId: secretsColl.Id, MaxSelect: 1})
		if err := f.app.Save(notes); err != nil {
			t.Fatal(err)
		}
		f.save(t, "notes", map[string]any{"title": "has a secret", "secret": secret})
		q := `{ notes(filter: {title: {is: ["has a secret"]}}) { edges { node { secret { id title } } } } }`
		member := f.ok(t, f.ann, q, nil)["notes"].(map[string]any)["edges"].([]any)[0].(map[string]any)["node"].(map[string]any)
		admin := f.ok(t, f.bob, q, nil)["notes"].(map[string]any)["edges"].([]any)[0].(map[string]any)["node"].(map[string]any)
		if member["secret"] != nil || admin["secret"] == nil {
			t.Fatalf("a member reads %v and an admin %v of a related secret", member["secret"], admin["secret"])
		}
	})
}

// A selection that reaches too deep is refused before any request is made, a
// wide one is refused whole by the request's resolution budget, and one field
// may make only so many requests of the Base.
func TestTheBudget(t *testing.T) {
	t.Parallel()

	f := setup(t)
	ids := f.people(t)

	deep := `{ person(id: "` + ids["ada"] + `") { company { people_via_company { edges { node {
		company { people_via_company { edges { node { company { name } } } } } } } } } } }`
	if a := f.gql(t, f.ann, deep, nil); !strings.Contains(a.messages(), "relations deep") {
		t.Fatalf("a deep selection: %q", a.messages())
	}

	for i := range 100 {
		f.save(t, "activities", map[string]any{"kind": "note", "subject": fmt.Sprintf("w%d", i), "source_id": fmt.Sprintf("w%d", i), "person": ids["ada"]})
	}
	var wide strings.Builder
	wide.WriteString("{")
	for i := range 40 {
		fmt.Fprintf(&wide, " a%d: activities(first: 100) { edges { cursor node { id subject kind source_id } } }", i)
	}
	wide.WriteString(" }")
	a := f.gql(t, f.ann, wide.String(), nil)
	if a.status != http.StatusBadRequest || a.Data != nil || !strings.Contains(a.messages(), "resolutions") {
		t.Fatalf("a wide request answered %d with %q", a.status, a.messages())
	}

	calls := f.gql(t, f.ann, `{ activities(first: 100) { edges { node { person { activities_via_person(first: 1) { totalCount } } } } } }`, nil)
	if !strings.Contains(calls.messages(), "at most 50 requests") {
		t.Fatalf("a request per record of a page: %q", calls.messages())
	}
}

// Every object answers its own type name, which is what a client caches by.
func TestTypeNames(t *testing.T) {
	t.Parallel()

	f := setup(t)
	f.people(t)

	a := f.ok(t, f.ann, `{ people(first: 1, orderBy: [{name: {firstName: Asc}}]) { __typename
		edges { __typename node { __typename name { __typename } company { __typename id } phones { __typename } } }
		pageInfo { __typename } aggregate { __typename count } } collections { __typename name } }`, nil)
	conn := a["people"].(map[string]any)
	edge := conn["edges"].([]any)[0].(map[string]any)
	node := edge["node"].(map[string]any)
	for got, want := range map[any]string{
		conn["__typename"]: "BasePersonConnection", edge["__typename"]: "BasePersonEdge",
		node["__typename"]: "BasePerson", node["name"].(map[string]any)["__typename"]: "BaseFullName",
		node["company"].(map[string]any)["__typename"]: "BaseCompany", node["phones"].(map[string]any)["__typename"]: "BasePhones",
		conn["pageInfo"].(map[string]any)["__typename"]:  "BasePageInfo",
		conn["aggregate"].(map[string]any)["__typename"]: "BaseAggregate",
	} {
		if got != want {
			t.Errorf("got %v, want %s", got, want)
		}
	}
	cols := a["collections"].([]any)
	if len(cols) == 0 || cols[0].(map[string]any)["__typename"] != "BaseCollection" {
		t.Fatalf("collections %v", cols)
	}
	for _, c := range cols {
		if n := c.(map[string]any)["name"].(string); strings.HasPrefix(n, "_") || n == "users" {
			t.Fatalf("collections lists %q", n)
		}
	}
}
