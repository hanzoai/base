package core_test

import (
	"encoding/json"
	"slices"
	"testing"

	"github.com/hanzoai/base/core"
	"github.com/hanzoai/base/tests"
	"github.com/hanzoai/base/tools/types"
)

// docs decodes collection documents written as JSON, the way a module or an
// import states them.
func docs(t *testing.T, raw string) []map[string]any {
	t.Helper()
	var out []map[string]any
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func mustGrow(t *testing.T, app core.App, raw string) *core.Growth {
	t.Helper()
	g, err := core.GrowCollections(app, docs(t, raw))
	if err != nil {
		t.Fatalf("grow: %v", err)
	}
	return g
}

func asJSON(v any) string {
	raw, _ := json.Marshal(v)
	return string(raw)
}

const shopDocs = `[
	{"name": "shops", "type": "base", "label": "Shop", "listRule": "@request.auth.id != \"\"",
	 "fields": [
		{"name": "title", "type": "text", "label": "Title", "required": true},
		{"name": "parent", "type": "relation", "collectionId": "shops", "maxSelect": 1},
		{"name": "kind", "type": "select", "maxSelect": 1, "values": ["a", "b"],
		 "options": [{"value": "a", "label": "A", "color": "red"}]}
	 ],
	 "indexes": ["CREATE INDEX ` + "`idx_shops_title`" + ` ON ` + "`shops`" + ` (` + "`title`" + `)"]},
	{"name": "items", "type": "base",
	 "fields": [
		{"name": "name", "type": "text"},
		{"name": "shop", "type": "relation", "collectionId": "shops", "maxSelect": 1, "cascadeDelete": true}
	 ]}
]`

func TestGrowCreatesWhatIsMissing(t *testing.T) {
	app, _ := tests.NewTestApp()
	defer app.Cleanup()

	g := mustGrow(t, app, shopDocs)
	if asJSON(g) != `{"created":["shops","items"],"grown":{},"kept":{}}` {
		t.Fatalf("growth: %s", asJSON(g))
	}

	shops, err := app.FindCollectionByNameOrId("shops")
	if err != nil {
		t.Fatal(err)
	}
	items, _ := app.FindCollectionByNameOrId("items")

	if parent := shops.Fields.GetByName("parent").(*core.RelationField); parent.CollectionId != shops.Id {
		t.Fatalf("a self relation named by name must resolve to the id, got %q", parent.CollectionId)
	}
	if shop := items.Fields.GetByName("shop").(*core.RelationField); shop.CollectionId != shops.Id || !shop.CascadeDelete {
		t.Fatalf("a relation named by name must resolve to the id, got %+v", shop)
	}
	if shops.ListRule == nil || *shops.ListRule != `@request.auth.id != ""` || shops.Label != "Shop" {
		t.Fatalf("the document's rules and options must be applied on create: %s", shops)
	}
	if shops.GetIndex("idx_shops_title") == "" {
		t.Fatalf("the document's index must be created: %v", shops.Indexes)
	}
	if f := shops.Fields.GetByName("title").(*core.TextField); f.Label != "Title" || !f.Required {
		t.Fatalf("field settings and meta must be applied: %+v", f)
	}
}

func TestGrowTwiceChangesNothing(t *testing.T) {
	app, _ := tests.NewTestApp()
	defer app.Cleanup()

	mustGrow(t, app, shopDocs)
	before, _ := app.FindCollectionByNameOrId("shops")

	g := mustGrow(t, app, shopDocs)
	if asJSON(g) != `{"created":[],"grown":{},"kept":{}}` {
		t.Fatalf("a second grow must change nothing, got %s", asJSON(g))
	}

	after, _ := app.FindCollectionByNameOrId("shops")
	if !after.Updated.Equal(before.Updated) {
		t.Fatal("a grow that changes nothing must not save the collection")
	}
}

func TestGrowKeepsWhatTheOrgChanged(t *testing.T) {
	app, _ := tests.NewTestApp()
	defer app.Cleanup()

	mustGrow(t, app, shopDocs)

	// the org adds a field of its own, relabels one, tightens a rule and drops
	// an index — none of which a later grow may undo
	shops, _ := app.FindCollectionByNameOrId("shops")
	shops.Fields.Add(&core.TextField{Name: "custom"})
	shops.Fields.GetByName("title").(*core.TextField).Label = "Shop name"
	shops.ListRule = types.Pointer(`@request.auth.admin = true`)
	if err := app.Save(shops); err != nil {
		t.Fatal(err)
	}

	g := mustGrow(t, app, shopDocs)
	if asJSON(g) != `{"created":[],"grown":{},"kept":{}}` {
		t.Fatalf("growth: %s", asJSON(g))
	}

	shops, _ = app.FindCollectionByNameOrId("shops")
	if shops.Fields.GetByName("custom") == nil {
		t.Fatal("the org's own field was dropped")
	}
	if l := shops.Fields.GetByName("title").(*core.TextField).Label; l != "Shop name" {
		t.Fatalf("the org's label was overwritten: %q", l)
	}
	if *shops.ListRule != `@request.auth.admin = true` {
		t.Fatalf("the org's rule was overwritten: %q", *shops.ListRule)
	}
}

func TestGrowAddsFieldsValuesAndIndexes(t *testing.T) {
	app, _ := tests.NewTestApp()
	defer app.Cleanup()

	mustGrow(t, app, shopDocs)

	// a later version of the documents: a new field, a new select value with
	// its option, a field the org already has under another type, a new index
	g := mustGrow(t, app, `[
		{"name": "shops", "type": "base",
		 "fields": [
			{"name": "title", "type": "number"},
			{"name": "phone", "type": "phones", "label": "Phone"},
			{"name": "kind", "type": "select", "maxSelect": 1, "values": ["a", "b", "c"],
			 "options": [{"value": "c", "label": "C", "color": "green"}]}
		 ],
		 "indexes": [
			"CREATE INDEX `+"`idx_shops_title`"+` ON `+"`shops`"+` (`+"`title`"+`)",
			"CREATE INDEX `+"`idx_shops_kind`"+` ON `+"`shops`"+` (`+"`kind`"+`)"
		 ]}
	]`)

	want := `{"created":[],"grown":{"shops":["phone","kind:c","index:idx_shops_kind"]},"kept":{"shops":["title"]}}`
	if asJSON(g) != want {
		t.Fatalf("growth:\nwant %s\n got %s", want, asJSON(g))
	}

	shops, _ := app.FindCollectionByNameOrId("shops")
	if shops.Fields.GetByName("title").Type() != core.FieldTypeText {
		t.Fatal("a field kept under its own type must not change")
	}
	kind := shops.Fields.GetByName("kind").(*core.SelectField)
	if !slices.Equal(kind.Values, []string{"a", "b", "c"}) || len(kind.Options) != 2 || kind.Options[1].Color != "green" {
		t.Fatalf("select values must be unioned with their options: %+v", kind)
	}
	if shops.Fields.GetByName("phone") == nil || shops.GetIndex("idx_shops_kind") == "" {
		t.Fatalf("the new field and index must be added: %s", shops)
	}
}

func TestGrowIsOneTransaction(t *testing.T) {
	app, _ := tests.NewTestApp()
	defer app.Cleanup()

	_, err := core.GrowCollections(app, docs(t, `[
		{"name": "first", "type": "base", "fields": [{"name": "a", "type": "text"}]},
		{"name": "second", "type": "base", "fields": [{"name": "b", "type": "relation", "collectionId": "nowhere"}]}
	]`))
	if err == nil {
		t.Fatal("a relation to no collection must fail the grow")
	}

	if _, err := app.FindCollectionByNameOrId("first"); err == nil {
		t.Fatal("a failed grow must leave nothing behind")
	}
}

func TestGrowKeepsACollectionOfAnotherType(t *testing.T) {
	app, _ := tests.NewTestApp()
	defer app.Cleanup()

	// "users" is an auth collection in every Base
	g := mustGrow(t, app, `[{"name": "users", "type": "base", "fields": [{"name": "extra", "type": "text"}]}]`)
	if asJSON(g) != `{"created":[],"grown":{},"kept":{"users":["type"]}}` {
		t.Fatalf("growth: %s", asJSON(g))
	}

	users, _ := app.FindCollectionByNameOrId("users")
	if users.Fields.GetByName("extra") != nil {
		t.Fatal("a collection of another type must be left alone")
	}
}

// An index the collection already holds under another name is already there: an
// org that indexed a column itself, then installs a module that indexes the same
// column, keeps its own index and the install still succeeds.
func TestGrowKeepsAnIndexHeldUnderAnotherName(t *testing.T) {
	app, _ := tests.NewTestApp()
	defer app.Cleanup()

	mustGrow(t, app, `[{"name": "events", "type": "base",
		"fields": [{"name": "source_id", "type": "text"}],
		"indexes": ["CREATE UNIQUE INDEX `+"`idx_mine`"+` ON `+"`events`"+` (`+"`source_id`"+`) WHERE source_id != ''"]}]`)

	g := mustGrow(t, app, `[{"name": "events", "type": "base",
		"fields": [{"name": "source_id", "type": "text"}],
		"indexes": ["CREATE UNIQUE INDEX `+"`idx_events_source_id`"+` ON `+"`events`"+` (`+"`source_id`"+`) WHERE source_id != ''"]}]`)
	if asJSON(g) != `{"created":[],"grown":{},"kept":{}}` {
		t.Fatalf("growth: %s", asJSON(g))
	}

	c, err := app.FindCollectionByNameOrId("events")
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Indexes) != 1 || c.GetIndex("idx_mine") == "" {
		t.Fatalf("indexes: %v", c.Indexes)
	}
}

// A document that says which rules it replaces tightens a collection still
// holding one of them, keeps a rule an org wrote, and is refused naming a rule
// that is not one.
func TestGrowReplacesOnlyTheRulesItShipped(t *testing.T) {
	app, _ := tests.NewTestApp()
	defer app.Cleanup()

	mustGrow(t, app, `[{"name": "notes", "type": "base", "listRule": "@request.auth.id != \"\"", "viewRule": "@request.auth.id != \"\"",
		"fields": [{"name": "title", "type": "text"}]}]`)

	notes, _ := app.FindCollectionByNameOrId("notes")
	notes.ViewRule = types.Pointer(`@request.auth.admin = true`) // the org's own
	if err := app.Save(notes); err != nil {
		t.Fatal(err)
	}

	tighter := `[{"name": "notes", "type": "base",
		"listRule": "@request.auth.id != \"\" && title != \"\"", "viewRule": "@request.auth.id != \"\" && title != \"\"",
		"replaces": {"listRule": ["@request.auth.id != \"\""], "viewRule": ["@request.auth.id != \"\""]},
		"fields": [{"name": "title", "type": "text"}]}]`
	g := mustGrow(t, app, tighter)
	if asJSON(g.Grown) != `{"notes":["rule:listRule"]}` {
		t.Fatalf("growth: %s", asJSON(g))
	}
	notes, _ = app.FindCollectionByNameOrId("notes")
	if *notes.ListRule != `@request.auth.id != "" && title != ""` || *notes.ViewRule != `@request.auth.admin = true` {
		t.Fatalf("rules %q %q", *notes.ListRule, *notes.ViewRule)
	}
	if g := mustGrow(t, app, tighter); len(g.Grown) != 0 {
		t.Fatalf("a second grow changed %s", asJSON(g))
	}

	if _, err := core.GrowCollections(app, docs(t, `[{"name": "notes", "type": "base", "replaces": {"readRule": ["x"]}}]`)); err == nil {
		t.Fatal("a replaces naming no rule was accepted")
	}
}
