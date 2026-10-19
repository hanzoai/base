package core_test

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/hanzoai/base/core"
	"github.com/hanzoai/base/tests"
	"github.com/hanzoai/base/tools/types"
)

// whereNow is the instant every relative date in these tests is taken from:
// Sunday 18 October 2026, noon UTC.
var whereNow = time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)

type whereFixture struct {
	app      *tests.TestApp
	deals    *core.Collection
	ids      map[string]string // title -> id, and company names -> id
	member   *core.RequestInfo // auth record "u1", not a superuser
	all      []string
	segments *core.Collection
}

// newWhereFixture builds companies, deals over every field type, members of a
// deal (for back-relations) and segments.
func newWhereFixture(t *testing.T) *whereFixture {
	t.Helper()

	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(app.Cleanup)

	fx := &whereFixture{app: app, ids: map[string]string{}}

	companies := core.NewBaseCollection("companies")
	companies.Fields.Add(&core.TextField{Name: "name"}, &core.NumberField{Name: "employees"})
	must(t, app.Save(companies))

	deals := core.NewBaseCollection("deals")
	deals.Fields.Add(
		&core.TextField{Name: "title"},
		&core.EditorField{Name: "notes"},
		&core.NumberField{Name: "amount"},
		&core.RatingField{Name: "rating"},
		&core.BoolField{Name: "won"},
		&core.SelectField{Name: "stage", MaxSelect: 1, Values: []string{"new", "won", "lost"}},
		&core.SelectField{Name: "tags", MaxSelect: 3, Values: []string{"a", "b", "c"}},
		&core.RelationField{Name: "company", CollectionId: companies.Id, MaxSelect: 1},
		&core.RelationField{Name: "contacts", CollectionId: companies.Id, MaxSelect: 5},
		&core.DateField{Name: "closed"},
		&core.JSONField{Name: "meta"},
		&core.TextField{Name: "owner"},
		&core.TextField{Name: "secret", Hidden: true},
		&core.EmailsField{Name: "emails"},
		&core.PhonesField{Name: "phones"},
		&core.CurrencyField{Name: "money"},
		&core.FullNameField{Name: "person"},
		&core.AddressField{Name: "place"},
		&core.LinksField{Name: "links"},
		&core.ActorField{Name: "createdBy", OnCreate: true},
	)
	must(t, app.Save(deals))
	fx.deals = deals

	members := core.NewBaseCollection("members")
	members.Fields.Add(
		&core.RelationField{Name: "deal", CollectionId: deals.Id, MaxSelect: 1},
		&core.TextField{Name: "label"},
	)
	must(t, app.Save(members))

	segments := core.NewBaseCollection(core.CollectionNameSegments)
	segments.ViewRule = types.Pointer("")
	segments.Fields.Add(&core.TextField{Name: "name"}, &core.TextField{Name: "object"}, &core.JSONField{Name: "where"})
	must(t, app.Save(segments))
	fx.segments = segments

	for _, c := range []struct {
		name string
		n    int
	}{{"Acme", 50}, {"Globex", 500}} {
		r := core.NewRecord(companies)
		r.Set("name", c.name)
		r.Set("employees", c.n)
		must(t, app.Save(r))
		fx.ids[c.name] = r.Id
	}
	acme, globex := fx.ids["Acme"], fx.ids["Globex"]

	ann := types.Actor{Source: core.ActorManual, Id: "u1", Name: "Ann"}
	bob := types.Actor{Source: core.ActorManual, Id: "u2", Name: "Bob"}
	imp := types.Actor{Source: core.ActorImport, Id: "u2", Name: "Bob"}

	rows := []struct {
		actor  *types.Actor
		values map[string]any
	}{
		{&ann, map[string]any{
			"title": "Alpha deal", "notes": "<p>first</p>", "amount": 100, "rating": 5, "won": true,
			"stage": "won", "tags": []string{"a", "b"}, "company": acme, "contacts": []string{acme, globex},
			"closed": "2026-10-18 03:00:00.000Z", "meta": map[string]any{"tier": "gold", "score": 7}, "owner": "u1",
			"emails": types.Emails{PrimaryEmail: "alpha@acme.test"},
			"phones": types.Phones{PrimaryPhoneNumber: "4155550100", PrimaryPhoneCallingCode: "+1", PrimaryPhoneCountryCode: "US"},
			"money":  types.Currency{AmountMicros: 1_500_000, CurrencyCode: "USD"},
			"person": types.FullName{FirstName: "Ada", LastName: "Lovelace"},
			"place":  map[string]any{"addressCity": "London", "addressCountry": "GB"},
			"links":  types.Links{PrimaryLinkUrl: "https://acme.test"},
			"secret": "s1",
		}},
		{&imp, map[string]any{
			"title": "Beta", "amount": 50, "rating": 3, "stage": "lost", "tags": []string{"b"}, "company": globex,
			"closed": "2026-09-10 10:00:00.000Z", "meta": map[string]any{"tier": "silver"}, "owner": "u2",
			"emails": types.Emails{PrimaryEmail: "beta@globex.test", AdditionalEmails: []string{"b2@x.test"}},
			"money":  types.Currency{AmountMicros: 500_000, CurrencyCode: "EUR"},
			"person": types.FullName{FirstName: "Grace", LastName: "Hopper"},
		}},
		{&ann, map[string]any{
			"title": "Gamma", "stage": "new", "contacts": []string{globex}, "closed": "2026-11-02 08:00:00.000Z",
			"phones": types.Phones{PrimaryPhoneNumber: "2025550199", PrimaryPhoneCallingCode: "+1"},
			"person": types.FullName{FirstName: "Alan", LastName: "Turing"},
			"place":  map[string]any{"addressCity": "Paris", "addressCountry": "FR"},
		}},
		{nil, map[string]any{
			"title": "Delta 50%", "amount": 7, "rating": 1, "won": true, "tags": []string{"c"}, "company": acme,
			"contacts": []string{acme}, "meta": map[string]any{"score": 2}, "owner": "u1",
			"emails": types.Emails{PrimaryEmail: "delta@acme.test"},
			"money":  types.Currency{AmountMicros: 9_000_000, CurrencyCode: "USD"},
		}},
		{&bob, map[string]any{
			"title": "Epsilon_x", "amount": 30, "rating": 4, "stage": "new", "tags": []string{"a"}, "company": globex,
			"closed": "2026-10-12 10:00:00.000Z", "owner": "u2",
		}},
	}

	for _, row := range rows {
		r := core.NewRecord(deals)
		for k, v := range row.values {
			r.Set(k, v)
		}
		ctx := context.Background()
		if row.actor != nil {
			ctx = core.WithActor(ctx, *row.actor)
		}
		must(t, app.SaveWithContext(ctx, r))
		title := row.values["title"].(string)
		fx.ids[title] = r.Id
		fx.all = append(fx.all, title)
	}

	for _, m := range []struct{ deal, label string }{{"Alpha deal", "vip"}, {"Alpha deal", "beta"}, {"Beta", "beta"}} {
		r := core.NewRecord(members)
		r.Set("deal", fx.ids[m.deal])
		r.Set("label", m.label)
		must(t, app.Save(r))
	}

	users, err := app.FindCollectionByNameOrId("users")
	if err != nil {
		t.Fatal(err)
	}
	auth := core.NewRecord(users)
	auth.Id = "u1"
	fx.member = &core.RequestInfo{Auth: auth}

	slices.Sort(fx.all)

	return fx
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

// match returns the titles of the deals where selects, sorted.
func (fx *whereFixture) match(info *core.RequestInfo, where string) ([]string, error) {
	filter, params, err := core.CompileWhere(fx.app, fx.deals, info, []byte(where), whereNow)
	if err != nil {
		return nil, err
	}

	resolver := core.NewRecordFieldResolver(fx.app, fx.deals, info, true)
	expr, err := filter.BuildExpr(resolver, params)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", filter, err)
	}

	q := fx.app.RecordQuery(fx.deals).AndWhere(expr)
	if err := resolver.UpdateQuery(q); err != nil {
		return nil, err
	}

	records := []*core.Record{}
	if err := q.All(&records); err != nil {
		return nil, fmt.Errorf("%s: %w", filter, err)
	}

	titles := make([]string, len(records))
	for i, r := range records {
		titles[i] = r.GetString("title")
	}
	slices.Sort(titles)

	return titles, nil
}

func (fx *whereFixture) complement(set []string) []string {
	out := []string{}
	for _, t := range fx.all {
		if !slices.Contains(set, t) {
			out = append(out, t)
		}
	}
	return out
}

// twins pairs every operator that has a negative twin.
var twins = map[string]string{
	core.WhereIs:        core.WhereIsNot,
	core.WhereContains:  core.WhereNotContains,
	core.WhereEmpty:     core.WhereNotEmpty,
	core.WhereInSegment: core.WhereNotInSegment,
}

func TestWhereOperators(t *testing.T) {
	fx := newWhereFixture(t)

	id := func(name string) string { return fx.ids[name] }

	scenarios := []struct {
		name  string
		where string
		want  []string
	}{
		// text
		{"text is", `{"field":"title","op":"is","value":"Beta"}`, []string{"Beta"}},
		{"text is any of", `{"field":"title","op":"is","value":["Beta","Gamma"]}`, []string{"Beta", "Gamma"}},
		{"text contains", `{"field":"title","op":"contains","value":"ta"}`, []string{"Beta", "Delta 50%"}},
		{"text contains is case-insensitive", `{"field":"title","op":"contains","value":"ALPHA"}`, []string{"Alpha deal"}},
		{"text contains a percent literally", `{"field":"title","op":"contains","value":"%"}`, []string{"Delta 50%"}},
		{"text contains an underscore literally", `{"field":"title","op":"contains","value":"_"}`, []string{"Epsilon_x"}},
		{"text empty", `{"field":"title","op":"empty"}`, []string{}},
		{"editor contains", `{"field":"notes","op":"contains","value":"first"}`, []string{"Alpha deal"}},
		{"editor empty", `{"field":"notes","op":"empty"}`, []string{"Beta", "Delta 50%", "Epsilon_x", "Gamma"}},
		{"text is @me", `{"field":"owner","op":"is","value":"@me"}`, []string{"Alpha deal", "Delta 50%"}},
		{"id is", `{"field":"id","op":"is","value":"` + id("Beta") + `"}`, []string{"Beta"}},

		// number and rating
		{"number is", `{"field":"amount","op":"is","value":50}`, []string{"Beta"}},
		{"number is any of", `{"field":"amount","op":"is","value":[50,7]}`, []string{"Beta", "Delta 50%"}},
		{"number gt", `{"field":"amount","op":"gt","value":30}`, []string{"Alpha deal", "Beta"}},
		{"number gte", `{"field":"amount","op":"gte","value":30}`, []string{"Alpha deal", "Beta", "Epsilon_x"}},
		{"number lt", `{"field":"amount","op":"lt","value":7}`, []string{"Gamma"}},
		{"number lte", `{"field":"amount","op":"lte","value":"7"}`, []string{"Delta 50%", "Gamma"}},
		{"number empty is zero", `{"field":"amount","op":"empty"}`, []string{"Gamma"}},
		{"rating gte", `{"field":"rating","op":"gte","value":4}`, []string{"Alpha deal", "Epsilon_x"}},
		{"rating is", `{"field":"rating","op":"is","value":"3"}`, []string{"Beta"}},
		{"rating empty", `{"field":"rating","op":"empty"}`, []string{"Gamma"}},

		// bool
		{"bool is true", `{"field":"won","op":"is","value":true}`, []string{"Alpha deal", "Delta 50%"}},
		{"bool is false", `{"field":"won","op":"is","value":"false"}`, []string{"Beta", "Epsilon_x", "Gamma"}},

		// select
		{"select is", `{"field":"stage","op":"is","value":"new"}`, []string{"Epsilon_x", "Gamma"}},
		{"select is any of", `{"field":"stage","op":"is","value":["won","lost"]}`, []string{"Alpha deal", "Beta"}},
		{"select empty", `{"field":"stage","op":"empty"}`, []string{"Delta 50%"}},
		{"multi-select is any of", `{"field":"tags","op":"is","value":["b","c"]}`, []string{"Alpha deal", "Beta", "Delta 50%"}},
		{"multi-select contains", `{"field":"tags","op":"contains","value":"a"}`, []string{"Alpha deal", "Epsilon_x"}},
		{"multi-select empty", `{"field":"tags","op":"empty"}`, []string{"Gamma"}},

		// relations
		{"relation is", `{"field":"company","op":"is","value":"` + id("Acme") + `"}`, []string{"Alpha deal", "Delta 50%"}},
		{"relation empty", `{"field":"company","op":"empty"}`, []string{"Gamma"}},
		{"through a relation", `{"field":"company.name","op":"contains","value":"acme"}`, []string{"Alpha deal", "Delta 50%"}},
		{"a number through a relation", `{"field":"company.employees","op":"gte","value":100}`, []string{"Beta", "Epsilon_x"}},
		{"multi-relation is", `{"field":"contacts","op":"is","value":"` + id("Globex") + `"}`, []string{"Alpha deal", "Gamma"}},
		{"multi-relation empty", `{"field":"contacts","op":"empty"}`, []string{"Beta", "Epsilon_x"}},
		{"through a multi-relation", `{"field":"contacts.name","op":"contains","value":"glob"}`, []string{"Alpha deal", "Gamma"}},
		{"in list (a back-relation)", `{"field":"members_via_deal.label","op":"is","value":"beta"}`, []string{"Alpha deal", "Beta"}},
		{"in a list nobody else is in", `{"field":"members_via_deal.label","op":"is","value":"vip"}`, []string{"Alpha deal"}},

		// dates
		{"date is a day", `{"field":"closed","op":"is","value":"2026-10-18"}`, []string{"Alpha deal"}},
		{"date is a day elsewhere", `{"field":"closed","op":"is","value":"2026-10-17","tz":"America/Los_Angeles"}`, []string{"Alpha deal"}},
		{"date is an instant", `{"field":"closed","op":"is","value":"2026-09-10 10:00:00.000Z"}`, []string{"Beta"}},
		{"date after a day", `{"field":"closed","op":"gt","value":"2026-10-18"}`, []string{"Gamma"}},
		{"date on or after a day", `{"field":"closed","op":"gte","value":"2026-10-18"}`, []string{"Alpha deal", "Gamma"}},
		{"date before a day", `{"field":"closed","op":"lt","value":"2026-10-18"}`, []string{"Beta", "Epsilon_x"}},
		{"date on or before a day", `{"field":"closed","op":"lte","value":"2026-10-18"}`, []string{"Alpha deal", "Beta", "Epsilon_x"}},
		{"date today", `{"field":"closed","op":"today"}`, []string{"Alpha deal"}},
		{"date today elsewhere", `{"field":"closed","op":"today","tz":"America/Los_Angeles"}`, []string{}},
		{"date past", `{"field":"closed","op":"past"}`, []string{"Alpha deal", "Beta", "Epsilon_x"}},
		{"date future", `{"field":"closed","op":"future"}`, []string{"Gamma"}},
		{"date this week from Monday", `{"field":"closed","op":"relative","value":{"direction":"this","unit":"week"}}`, []string{"Alpha deal", "Epsilon_x"}},
		{"date this week from Sunday", `{"field":"closed","op":"relative","value":{"direction":"this","unit":"week"},"weekStart":0}`, []string{"Alpha deal"}},
		{"date past month", `{"field":"closed","op":"relative","value":{"direction":"past","amount":1,"unit":"month"}}`, []string{"Beta"}},
		{"date next month", `{"field":"closed","op":"relative","value":{"direction":"next","amount":1,"unit":"month"}}`, []string{"Gamma"}},
		{"date this month", `{"field":"closed","op":"relative","value":{"direction":"this","unit":"month"}}`, []string{"Alpha deal", "Epsilon_x"}},
		{"date this quarter", `{"field":"closed","op":"relative","value":{"direction":"this","unit":"quarter"}}`, []string{"Alpha deal", "Epsilon_x", "Gamma"}},
		{"date past 12 hours", `{"field":"closed","op":"relative","value":{"direction":"past","amount":12,"unit":"hour"}}`, []string{"Alpha deal"}},
		{"date empty", `{"field":"closed","op":"empty"}`, []string{"Delta 50%"}},

		// json
		{"json contains", `{"field":"meta","op":"contains","value":"gold"}`, []string{"Alpha deal"}},
		{"json empty", `{"field":"meta","op":"empty"}`, []string{"Epsilon_x", "Gamma"}},
		{"json part is", `{"field":"meta.tier","op":"is","value":"silver"}`, []string{"Beta"}},
		{"json part gt", `{"field":"meta.score","op":"gt","value":5}`, []string{"Alpha deal"}},
		{"json part lte", `{"field":"meta.score","op":"lte","value":5}`, []string{"Delta 50%"}},

		// composites
		{"emails contains", `{"field":"emails","op":"contains","value":"acme"}`, []string{"Alpha deal", "Delta 50%"}},
		{"emails contains another address", `{"field":"emails","op":"contains","value":"b2@"}`, []string{"Beta"}},
		{"emails empty", `{"field":"emails","op":"empty"}`, []string{"Epsilon_x", "Gamma"}},
		{"emails primary is", `{"field":"emails.primaryEmail","op":"is","value":"beta@globex.test"}`, []string{"Beta"}},
		{"phones contains digits", `{"field":"phones","op":"contains","value":"(415) 555"}`, []string{"Alpha deal"}},
		{"phones contains no digit", `{"field":"phones","op":"contains","value":"abc"}`, []string{}},
		{"phones empty", `{"field":"phones","op":"empty"}`, []string{"Beta", "Delta 50%", "Epsilon_x"}},
		{"phones calling code", `{"field":"phones.primaryPhoneCallingCode","op":"is","value":"+1"}`, []string{"Alpha deal", "Gamma"}},
		{"currency amount gte", `{"field":"money.amountMicros","op":"gte","value":1000000}`, []string{"Alpha deal", "Delta 50%"}},
		{"currency code is", `{"field":"money.currencyCode","op":"is","value":"EUR"}`, []string{"Beta"}},
		{"currency empty", `{"field":"money","op":"empty"}`, []string{"Epsilon_x", "Gamma"}},
		{"full name contains", `{"field":"person","op":"contains","value":"hop"}`, []string{"Beta"}},
		{"full name part is", `{"field":"person.lastName","op":"is","value":"Turing"}`, []string{"Gamma"}},
		{"full name empty", `{"field":"person","op":"empty"}`, []string{"Delta 50%", "Epsilon_x"}},
		{"address contains", `{"field":"place","op":"contains","value":"paris"}`, []string{"Gamma"}},
		{"address part is", `{"field":"place.addressCountry","op":"is","value":"GB"}`, []string{"Alpha deal"}},
		{"links contains", `{"field":"links","op":"contains","value":"acme.test"}`, []string{"Alpha deal"}},
		{"actor id is @me", `{"field":"createdBy.id","op":"is","value":"@me"}`, []string{"Alpha deal", "Gamma"}},
		{"actor source is", `{"field":"createdBy.source","op":"is","value":"import"}`, []string{"Beta"}},
		{"actor contains a name", `{"field":"createdBy","op":"contains","value":"bob"}`, []string{"Beta", "Epsilon_x"}},
		{"actor contains a source", `{"field":"createdBy","op":"contains","value":"system"}`, []string{"Delta 50%"}},

		// groups
		{"and", `{"and":[{"field":"stage","op":"is","value":"new"},{"field":"amount","op":"gt","value":10}]}`, []string{"Epsilon_x"}},
		{"or", `{"or":[{"field":"stage","op":"is","value":"lost"},{"field":"won","op":"is","value":true}]}`, []string{"Alpha deal", "Beta", "Delta 50%"}},
		{"nested", `{"and":[{"field":"won","op":"is","value":false},{"or":[{"field":"amount","op":"gt","value":40},{"field":"title","op":"contains","value":"gam"}]}]}`, []string{"Beta", "Gamma"}},
		{"double negation", `{"not":{"not":{"field":"title","op":"is","value":"Beta"}}}`, []string{"Beta"}},
		{"an empty clause", `{}`, fx.all},
		{"an empty group", `{"and":[]}`, fx.all},
	}

	for _, s := range scenarios {
		t.Run(s.name, func(t *testing.T) {
			got, err := fx.match(fx.member, s.where)
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(got, s.want) {
				t.Fatalf("want %v, got %v", s.want, got)
			}

			// the negation is exactly everything else
			neg, err := fx.match(fx.member, `{"not":`+s.where+`}`)
			if err != nil {
				t.Fatal(err)
			}
			if want := fx.complement(s.want); !slices.Equal(neg, want) {
				t.Fatalf("negated: want %v, got %v", want, neg)
			}

			// and so is the negative twin of the operator
			for op, twin := range twins {
				needle := `"op":"` + op + `"`
				if strings.Count(s.where, `"op"`) == 1 && strings.Contains(s.where, needle) {
					got, err := fx.match(fx.member, strings.Replace(s.where, needle, `"op":"`+twin+`"`, 1))
					if err != nil {
						t.Fatal(err)
					}
					if want := fx.complement(s.want); !slices.Equal(got, want) {
						t.Fatalf("%s: want %v, got %v", twin, want, got)
					}
				}
			}
		})
	}
}

func TestWhereSegments(t *testing.T) {
	fx := newWhereFixture(t)

	segment := func(object, where string) string {
		r := core.NewRecord(fx.segments)
		r.Set("object", object)
		r.Set("where", where)
		must(t, fx.app.Save(r))
		return r.Id
	}

	newDeals := segment("deals", `{"field":"stage","op":"is","value":"new"}`)
	bigNew := segment("deals", `{"and":[{"op":"inSegment","value":"`+newDeals+`"},{"field":"amount","op":"gt","value":10}]}`)
	companiesOnly := segment("companies", `{"field":"name","op":"is","value":"Acme"}`)

	got, err := fx.match(fx.member, `{"op":"inSegment","value":"`+newDeals+`"}`)
	must(t, err)
	if want := []string{"Epsilon_x", "Gamma"}; !slices.Equal(got, want) {
		t.Fatalf("inSegment: want %v, got %v", want, got)
	}

	got, err = fx.match(fx.member, `{"op":"notInSegment","value":"`+newDeals+`"}`)
	must(t, err)
	if want := fx.complement([]string{"Epsilon_x", "Gamma"}); !slices.Equal(got, want) {
		t.Fatalf("notInSegment: want %v, got %v", want, got)
	}

	got, err = fx.match(fx.member, `{"op":"inSegment","value":"`+bigNew+`"}`)
	must(t, err)
	if want := []string{"Epsilon_x"}; !slices.Equal(got, want) {
		t.Fatalf("a segment of a segment: want %v, got %v", want, got)
	}

	if _, err := fx.match(fx.member, `{"op":"inSegment","value":"`+companiesOnly+`"}`); err == nil {
		t.Fatal("a segment of another collection must be refused")
	}

	// two segments that name each other
	a := core.NewRecord(fx.segments)
	a.Set("object", "deals")
	a.Set("where", `{}`)
	must(t, fx.app.Save(a))
	b := segment("deals", `{"op":"inSegment","value":"`+a.Id+`"}`)
	a.Set("where", `{"op":"inSegment","value":"`+b+`"}`)
	must(t, fx.app.Save(a))
	if _, err := fx.match(fx.member, `{"op":"inSegment","value":"`+a.Id+`"}`); err == nil || !strings.Contains(err.Error(), "names itself") {
		t.Fatalf("a cycle must be refused, got %v", err)
	}

	// a segment the caller may not read is one it cannot name
	fx.segments.ViewRule = nil
	must(t, fx.app.Save(fx.segments))
	if _, err := fx.match(fx.member, `{"op":"inSegment","value":"`+newDeals+`"}`); err == nil {
		t.Fatal("a segment the caller cannot view must be refused")
	}
}

func TestWhereRefusals(t *testing.T) {
	fx := newWhereFixture(t)

	deep := `{"field":"title","op":"is","value":"x"}`
	for range 9 {
		deep = `{"not":` + deep + `}`
	}
	many := make([]string, 101)
	for i := range many {
		many[i] = `{"field":"title","op":"is","value":"x"}`
	}
	values := make([]string, 101)
	for i := range values {
		values[i] = `"x"`
	}

	scenarios := []struct{ name, where string }{
		{"an unknown field", `{"field":"nope","op":"is","value":"x"}`},
		{"an unknown operator", `{"field":"title","op":"near","value":"x"}`},
		{"an ordering of text", `{"field":"title","op":"gt","value":"x"}`},
		{"a bool asked if empty", `{"field":"won","op":"empty"}`},
		{"today asked of a number", `{"field":"amount","op":"today"}`},
		{"a part a value does not have", `{"field":"emails.secondaryEmail","op":"is","value":"x"}`},
		{"a path through a text field", `{"field":"title.length","op":"is","value":"x"}`},
		{"a hidden field", `{"field":"secret","op":"is","value":"s1"}`},
		{"no value", `{"field":"title","op":"is"}`},
		{"an empty list", `{"field":"title","op":"is","value":[]}`},
		{"contains nothing", `{"field":"title","op":"contains","value":""}`},
		{"a number that is not one", `{"field":"amount","op":"gt","value":"many"}`},
		{"an unknown unit", `{"field":"closed","op":"relative","value":{"direction":"past","amount":1,"unit":"fortnight"}}`},
		{"an unknown time zone", `{"field":"closed","op":"today","tz":"Mars/Olympus"}`},
		{"two kinds in one node", `{"and":[],"field":"title","op":"is","value":"x"}`},
		{"an unknown key", `{"field":"title","op":"is","value":"x","flavour":1}`},
		{"nested too deep", deep},
		{"too many conditions", `{"and":[` + strings.Join(many, ",") + `]}`},
		{"too many values", `{"field":"title","op":"is","value":[` + strings.Join(values, ",") + `]}`},
		{"a request field", `{"field":"@request.auth.id","op":"is","value":"x"}`},
	}

	for _, s := range scenarios {
		t.Run(s.name, func(t *testing.T) {
			if _, err := fx.match(fx.member, s.where); err == nil {
				t.Fatalf("expected %s to be refused", s.where)
			}
		})
	}

	// a superuser may name a hidden field
	supers, err := fx.app.FindCollectionByNameOrId(core.CollectionNameSuperusers)
	must(t, err)
	got, err := fx.match(&core.RequestInfo{Auth: core.NewRecord(supers)}, `{"field":"secret","op":"is","value":"s1"}`)
	must(t, err)
	if !slices.Equal(got, []string{"Alpha deal"}) {
		t.Fatalf("superuser on a hidden field: got %v", got)
	}
}

// Every value is bound: the text of a clause never reaches the filter.
func TestWhereBindsValues(t *testing.T) {
	fx := newWhereFixture(t)

	filter, params, err := core.CompileWhere(fx.app, fx.deals, fx.member,
		[]byte(`{"field":"title","op":"is","value":"x\" || id != \"y"}`), whereNow)
	must(t, err)
	if strings.Contains(string(filter), "x") || len(params) != 1 {
		t.Fatalf("the value must be a parameter: %s %v", filter, params)
	}

	got, err := fx.match(fx.member, `{"field":"title","op":"is","value":"x\" || id != \"y"}`)
	must(t, err)
	if len(got) != 0 {
		t.Fatalf("a value is a value, got %v", got)
	}
}
