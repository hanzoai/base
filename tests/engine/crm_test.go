package engine

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/hanzoai/base/core"
	"github.com/hanzoai/base/modules"
	_ "github.com/hanzoai/base/modules/crm"
	"github.com/hanzoai/base/tools/types"
)

// The CRM module on either engine: it installs, its composite values store and
// read back, a primary email stays unique whatever its case, a where clause and
// a part path filter alike, and a stage change is history.
func TestCRM(t *testing.T) {
	app := newApp(t)

	growth, err := modules.Install(app, "crm")
	if err != nil {
		t.Fatalf("install: %v", err)
	}
	if len(growth.Created) != 13 {
		t.Fatalf("created %v", growth.Created)
	}
	if again, err := modules.Install(app, "crm"); err != nil || len(again.Created)+len(again.Grown) != 0 {
		t.Fatalf("a second install changed something: %+v %v", again, err)
	}

	ctx := core.WithActor(context.Background(), types.Actor{Source: core.ActorManual, Id: "ann", Name: "Ann"})
	save := func(coll string, data map[string]any) *core.Record {
		t.Helper()
		c, err := app.FindCollectionByNameOrId(coll)
		if err != nil {
			t.Fatal(err)
		}
		r := core.NewRecord(c)
		for k, v := range data {
			r.Set(k, v)
		}
		if err := app.SaveWithContext(ctx, r); err != nil {
			t.Fatalf("save %s: %v", coll, err)
		}
		return r
	}

	acme := save("companies", map[string]any{"name": "Acme", "annual_revenue": types.Currency{AmountMicros: 9_000_000, CurrencyCode: "USD"}})
	ada := save("people", map[string]any{
		"name": types.FullName{FirstName: "Ada", LastName: "Lovelace"}, "company": acme.Id, "lifecycle": "lead",
		"emails": types.Emails{PrimaryEmail: "Ada@Acme.test"}, "phones": types.Phones{PrimaryPhoneNumber: "+1 (415) 555-0100"},
	})
	save("people", map[string]any{"name": types.FullName{FirstName: "Grace"}, "lifecycle": "customer", "emails": types.Emails{PrimaryEmail: "grace@globex.test"}})

	people, _ := app.FindCollectionByNameOrId("people")
	dup := core.NewRecord(people)
	dup.Set("emails", types.Emails{PrimaryEmail: "ADA@acme.TEST"})
	if err := app.Validate(dup); err == nil || !strings.Contains(err.Error(), "unique") {
		t.Fatalf("a primary email another record holds was accepted: %v", err)
	}

	got, err := app.FindRecordById(people, ada.Id)
	if err != nil {
		t.Fatal(err)
	}
	if got.Get("emails").(types.Emails).PrimaryEmail != "ada@acme.test" || got.Get("phones").(types.Phones).PrimaryPhoneNumber != "+14155550100" {
		t.Fatalf("values: %v %v", got.Get("emails"), got.Get("phones"))
	}
	if a := got.Get("created_by").(types.Actor); a.Id != "ann" {
		t.Fatalf("created_by: %+v", a)
	}

	for name, s := range map[string]struct {
		where string
		want  int
	}{
		"select any of":       {`{"field":"lifecycle","op":"is","value":["lead"]}`, 1},
		"composite contains":  {`{"field":"emails","op":"contains","value":"globex"}`, 1},
		"phone digits":        {`{"field":"phones","op":"contains","value":"415 555"}`, 1},
		"a relation's field":  {`{"field":"company.name","op":"is","value":"Acme"}`, 1},
		"not":                 {`{"not":{"field":"lifecycle","op":"is","value":"lead"}}`, 1},
		"empty composite":     {`{"field":"address","op":"empty"}`, 2},
		"or":                  {`{"or":[{"field":"name.firstName","op":"is","value":"Ada"},{"field":"name.firstName","op":"is","value":"Grace"}]}`, 2},
		"created today":       {`{"field":"created","op":"today"}`, 2},
	} {
		filter, params, err := core.CompileWhere(app, people, nil, []byte(s.where), time.Now())
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		found, err := app.FindRecordsByFilter(people, string(filter), "", 0, 0, params)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if len(found) != s.want {
			t.Errorf("%s: %d records, want %d", name, len(found), s.want)
		}
	}

	found, err := app.FindRecordsByFilter("companies", "annual_revenue.amountMicros >= 1000000", "-annual_revenue.amountMicros", 0, 0)
	if err != nil || len(found) != 1 {
		t.Fatalf("a part filter: %d, %v", len(found), err)
	}

	opp := save("opportunities", map[string]any{"name": "deal", "stage": "new", "company": acme.Id})
	opp.Set("stage", "meeting")
	if err := app.SaveWithContext(ctx, opp); err != nil {
		t.Fatal(err)
	}
	rows, err := app.FindRecordsByFilter(core.CollectionNameHistory, "record = {:id}", "created", 0, 0, map[string]any{"id": opp.Id})
	if err != nil || len(rows) != 2 || rows[1].GetString("action") != "update" || !strings.Contains(rows[1].GetString("changes"), `"meeting"`) {
		t.Fatalf("history: %d rows, %v", len(rows), err)
	}
}
