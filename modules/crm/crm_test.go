package crm_test

import (
	"strings"
	"testing"

	"github.com/hanzoai/base/core"
	"github.com/hanzoai/base/modules"
	"github.com/hanzoai/base/modules/crm"
	"github.com/hanzoai/base/tests"
	"github.com/hanzoai/base/tools/types"
)

var collections = []string{
	"companies", "people", "opportunities", "candidates", "applications", "segments",
	"lists", "list_members", "activities", "notes", "tasks", "attachments", "views",
}

func install(t *testing.T) *tests.TestApp {
	t.Helper()
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(app.Cleanup)

	g, err := modules.Install(app, crm.Name)
	if err != nil {
		t.Fatalf("install: %v", err)
	}
	if strings.Join(g.Created, ",") != strings.Join(collections, ",") {
		t.Fatalf("created %v", g.Created)
	}
	return app
}

func TestTheModuleInstallsAndReinstallsAsNothing(t *testing.T) {
	app := install(t)

	g, err := modules.Install(app, crm.Name)
	if err != nil {
		t.Fatal(err)
	}
	if len(g.Created) != 0 || len(g.Grown) != 0 || len(g.Kept) != 0 {
		t.Fatalf("a second install must change nothing: %+v", g)
	}
}

func TestEveryFieldIsLabelled(t *testing.T) {
	app := install(t)

	history := map[string]bool{
		"people": true, "companies": true, "opportunities": true, "candidates": true,
		"applications": true, "notes": true, "tasks": true,
	}

	for _, name := range collections {
		c, err := app.FindCollectionByNameOrId(name)
		if err != nil {
			t.Fatal(err)
		}
		if c.Label == "" || c.Plural == "" || c.Icon == "" {
			t.Errorf("%s is not labelled: %q %q %q", name, c.Label, c.Plural, c.Icon)
		}
		if c.History != history[name] {
			t.Errorf("%s history = %v", name, c.History)
		}
		for _, f := range c.Fields {
			if f.GetName() == core.FieldNameId {
				continue
			}
			m := f.(core.FieldMetaHolder).Meta()
			if m.Label == "" || m.Icon == "" || m.Position == 0 {
				t.Errorf("%s.%s is not labelled: %+v", name, f.GetName(), *m)
			}
		}
		if c.Fields.GetByName("created_by") == nil || c.Fields.GetByName("updated_by") == nil {
			t.Errorf("%s does not record its writers", name)
		}
	}

	stage := mustCollection(t, app, "opportunities").Fields.GetByName("stage").(*core.SelectField)
	if len(stage.Options) != 10 || stage.Options[0].Color != "red" || stage.Options[4].Color != "yellow" {
		t.Errorf("the pipeline stages are not coloured: %+v", stage.Options)
	}
}

func TestFundraisingIsAPipeline(t *testing.T) {
	app := install(t)

	fund := save(t, app, "companies", map[string]any{"name": "First Fund", "type": "investor"})
	gp := save(t, app, "people", map[string]any{
		"name":      map[string]any{"firstName": "Grace", "lastName": "Partner"},
		"emails":    map[string]any{"primaryEmail": "grace@first.test"},
		"company":   fund.Id,
		"type":      "investor",
		"outreach":  "replied",
		"contacted": "2026-10-20 00:00:00.000Z",
	})
	round := save(t, app, "opportunities", map[string]any{
		"name":      "First Fund, seed",
		"pipeline":  "fundraising",
		"stage":     "term_sheet",
		"amount":    map[string]any{"amountMicros": 1_500_000_000_000, "currencyCode": "USD"},
		"company":   fund.Id,
		"contact":   gp.Id,
		"next_step": "Send the data room",
		"contacted": "2026-10-28 00:00:00.000Z",
	})
	save(t, app, "opportunities", map[string]any{"name": "Acme pilot", "pipeline": "sales", "stage": "meeting"})

	got, err := app.FindRecordsByFilter("opportunities",
		`pipeline = "fundraising" && stage = "term_sheet" && amount.amountMicros >= 1000000000000 && contacted >= "2026-10-01"`, "", 0, 0)
	if err != nil || len(got) != 1 || got[0].Id != round.Id {
		t.Fatalf("the round is not found in its pipeline: %v %v", got, err)
	}
	if got[0].GetString("next_step") != "Send the data room" {
		t.Fatalf("next step: %q", got[0].GetString("next_step"))
	}

	investors, err := app.FindRecordsByFilter("people", `type = "investor" && outreach = "replied" && company.type = "investor"`, "", 0, 0)
	if err != nil || len(investors) != 1 || investors[0].Id != gp.Id {
		t.Fatalf("investors: %v %v", investors, err)
	}

	bad := core.NewRecord(mustCollection(t, app, "opportunities"))
	bad.Load(map[string]any{"name": "x", "pipeline": "lottery"})
	if err := app.Save(bad); err == nil {
		t.Fatal("a pipeline the module does not name must be refused")
	}
}

func TestACrmInstalledBeforeThePipelinesGrowsThem(t *testing.T) {
	app := install(t)

	// The CRM as it was installed before pipelines, types and outreach: drop what
	// this version adds, then install again.
	opp := mustCollection(t, app, "opportunities")
	opp.Fields.RemoveByName("pipeline")
	opp.Fields.RemoveByName("next_step")
	stage := opp.Fields.GetByName("stage").(*core.SelectField)
	stage.Values = stage.Values[:5]
	stage.Options = stage.Options[:5]
	opp.RemoveIndex("idx_opportunities_pipeline_stage")
	if err := app.Save(opp); err != nil {
		t.Fatal(err)
	}
	people := mustCollection(t, app, "people")
	people.Fields.RemoveByName("outreach")
	people.RemoveIndex("idx_people_outreach")
	if err := app.Save(people); err != nil {
		t.Fatal(err)
	}

	g, err := modules.Install(app, crm.Name)
	if err != nil {
		t.Fatal(err)
	}
	grown := strings.Join(g.Grown["opportunities"], ",")
	for _, want := range []string{"pipeline", "next_step", "stage:intro", "stage:term_sheet", "stage:lost", "index:idx_opportunities_pipeline_stage"} {
		if !strings.Contains(","+grown+",", ","+want+",") {
			t.Errorf("opportunities did not grow %s: %s", want, grown)
		}
	}
	if !strings.Contains(strings.Join(g.Grown["people"], ","), "outreach") {
		t.Errorf("people did not grow outreach: %v", g.Grown["people"])
	}
}

func mustCollection(t *testing.T, app core.App, name string) *core.Collection {
	t.Helper()
	c, err := app.FindCollectionByNameOrId(name)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func save(t *testing.T, app core.App, collection string, data map[string]any) *core.Record {
	t.Helper()
	r := core.NewRecord(mustCollection(t, app, collection))
	r.Load(data)
	if err := app.Save(r); err != nil {
		t.Fatalf("save %s: %v", collection, err)
	}
	return r
}

func TestThePipeline(t *testing.T) {
	app := install(t)

	acme := save(t, app, "companies", map[string]any{
		"name":           "Acme",
		"domain":         map[string]any{"primaryLinkUrl": "acme.test"},
		"annual_revenue": map[string]any{"amountMicros": 12_000_000_000_000, "currencyCode": "USD"},
	})
	ada := save(t, app, "people", map[string]any{
		"name":      map[string]any{"firstName": "Ada", "lastName": "Lovelace"},
		"emails":    map[string]any{"primaryEmail": "ada@acme.test"},
		"company":   acme.Id,
		"lifecycle": "lead",
	})
	deal := save(t, app, "opportunities", map[string]any{
		"name":    "Acme pilot",
		"amount":  map[string]any{"amountMicros": 50_000_000_000, "currencyCode": "USD"},
		"stage":   "meeting",
		"company": acme.Id,
		"contact": ada.Id,
	})

	got, err := app.FindRecordsByFilter("opportunities", `company.name = "Acme" && contact.emails.primaryEmail = "ada@acme.test"`, "", 0, 0)
	if err != nil || len(got) != 1 || got[0].Id != deal.Id {
		t.Fatalf("the deal is not reached through its company and contact: %v %v", got, err)
	}

	// a person's primary address is theirs alone
	dup := core.NewRecord(mustCollection(t, app, "people"))
	dup.Set("emails", types.Emails{PrimaryEmail: "ADA@acme.test"})
	if err := app.Save(dup); err == nil {
		t.Fatal("a second person with the same primary email must be refused")
	}
}

func TestListsAndTheInListFilter(t *testing.T) {
	app := install(t)

	ada := save(t, app, "people", map[string]any{"name": map[string]any{"firstName": "Ada"}})
	bob := save(t, app, "people", map[string]any{"name": map[string]any{"firstName": "Bob"}})
	invitees := save(t, app, "lists", map[string]any{"name": "Invitees", "object": "people"})
	save(t, app, "list_members", map[string]any{"list": invitees.Id, "person": ada.Id, "source": "manual"})

	dup := core.NewRecord(mustCollection(t, app, "list_members"))
	dup.Load(map[string]any{"list": invitees.Id, "person": ada.Id})
	if err := app.Save(dup); err == nil {
		t.Fatal("a person may be on a list once")
	}

	in, err := app.FindRecordsByFilter("people", `list_members_via_person.list ?= {:list}`, "", 0, 0, map[string]any{"list": invitees.Id})
	if err != nil || len(in) != 1 || in[0].Id != ada.Id {
		t.Fatalf("in list: %v %v", in, err)
	}

	out, err := app.FindRecordsByFilter("people", `list_members_via_person.list != {:list}`, "", 0, 0, map[string]any{"list": invitees.Id})
	if err != nil || len(out) != 1 || out[0].Id != bob.Id {
		t.Fatalf("not in list: %v %v", out, err)
	}

	// deleting the list takes its members with it
	if err := app.Delete(invitees); err != nil {
		t.Fatal(err)
	}
	if left, _ := app.FindRecordsByFilter("list_members", "", "", 0, 0); len(left) != 0 {
		t.Fatalf("members outlived their list: %d", len(left))
	}
}

func TestAnActivityIsWrittenOncePerSource(t *testing.T) {
	app := install(t)

	ada := save(t, app, "people", map[string]any{"name": map[string]any{"firstName": "Ada"}})
	save(t, app, "activities", map[string]any{"kind": "email", "direction": "in", "source_id": "msg-1", "person": ada.Id})

	again := core.NewRecord(mustCollection(t, app, "activities"))
	again.Load(map[string]any{"kind": "email", "source_id": "msg-1", "person": ada.Id})
	if err := app.Save(again); err == nil {
		t.Fatal("a second activity from one source must be refused")
	}

	// activities with no source are not one another's duplicates
	save(t, app, "activities", map[string]any{"kind": "note", "person": ada.Id})
	save(t, app, "activities", map[string]any{"kind": "note", "person": ada.Id})
}

func TestSegmentsAndViewsHoldTheirFilters(t *testing.T) {
	app := install(t)

	where := map[string]any{"field": "lifecycle", "op": "is", "value": []any{"lead"}}
	seg := save(t, app, "segments", map[string]any{"name": "Leads", "object": "people", "where": where})
	view := save(t, app, "views", map[string]any{
		"name": "By stage", "object": "opportunities", "type": "kanban", "group_by": "stage",
		"where": where, "sorts": []any{map[string]any{"field": "close_date", "direction": "asc"}},
	})

	for _, r := range []*core.Record{seg, view} {
		got, err := app.FindRecordById(r.Collection(), r.Id)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(got.GetString("where"), `"lifecycle"`) {
			t.Fatalf("%s lost its filter: %s", r.Collection().Name, got.GetString("where"))
		}
	}
}
