package core_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/hanzoai/base/core"
	"github.com/hanzoai/base/tests"
	"github.com/hanzoai/base/tools/types"
	"github.com/hanzoai/dbx"
)

var (
	ann = types.Actor{Source: core.ActorManual, Id: "ann", Name: "Ann"}
	bob = types.Actor{Source: core.ActorManual, Id: "bob", Name: "Bob"}
)

func as(a types.Actor) context.Context { return core.WithActor(context.Background(), a) }

// dealsCollection keeps its history when history is true.
func dealsCollection(t *testing.T, app core.App, name string, history bool) *core.Collection {
	t.Helper()

	c := core.NewBaseCollection(name)
	c.History = history
	c.Fields.Add(
		&core.TextField{Name: "title"},
		&core.NumberField{Name: "amount"},
		&core.TextField{Name: "secret", Hidden: true},
		&core.ActorField{Name: "createdBy", OnCreate: true},
		&core.AutodateField{Name: "created", OnCreate: true},
		&core.AutodateField{Name: "updated", OnCreate: true, OnUpdate: true},
	)
	c.AddIndex("idx_"+name+"_title", true, "title", "title != ''")
	if err := app.Save(c); err != nil {
		t.Fatalf("save collection: %v", err)
	}
	return c
}

type historyRow struct {
	Id      string `db:"id"`
	Action  string `db:"action"`
	Actor   string `db:"actor"`
	Changes string `db:"changes"`
	Created string `db:"created"`
}

// historyOf reads a record's history rows, oldest first.
func historyOf(t *testing.T, app core.App, c *core.Collection, id string) []historyRow {
	t.Helper()

	rows := []historyRow{}
	err := app.DB().Select("id", "action", "actor", "changes", "created").
		From(core.CollectionNameHistory).
		Where(dbx.HashExp{"collection": c.Id, "record": id}).
		OrderBy("created ASC", "_rowid_ ASC").
		All(&rows)
	if err != nil {
		t.Fatal(err)
	}
	return rows
}

func changesOf(t *testing.T, row historyRow) map[string]map[string]any {
	t.Helper()

	m := map[string]map[string]any{}
	if err := json.Unmarshal([]byte(row.Changes), &m); err != nil {
		t.Fatalf("changes %q: %v", row.Changes, err)
	}
	return m
}

func actorOf(t *testing.T, row historyRow) types.Actor {
	t.Helper()

	var a types.Actor
	if err := a.Scan(row.Actor); err != nil {
		t.Fatal(err)
	}
	return a
}

func TestHistoryRecordsCreateUpdateDelete(t *testing.T) {
	app, _ := tests.NewTestApp()
	defer app.Cleanup()

	c := dealsCollection(t, app, "deals", true)

	r := core.NewRecord(c)
	r.Set("title", "Acme")
	r.Set("amount", 100)
	r.Set("secret", "the margin")
	if err := app.SaveWithContext(as(ann), r); err != nil {
		t.Fatal(err)
	}

	rows := historyOf(t, app, c, r.Id)
	if len(rows) != 1 || rows[0].Action != core.HistoryCreate {
		t.Fatalf("expected one create row, got %+v", rows)
	}
	if a := actorOf(t, rows[0]); a != ann {
		t.Fatalf("the create row names %+v, want %+v", a, ann)
	}
	created := changesOf(t, rows[0])
	if created["title"]["to"] != "Acme" || created["amount"]["to"] != float64(100) {
		t.Fatalf("create changes: %v", created)
	}
	for _, skipped := range []string{"secret", "createdBy", "created", "updated", "id"} {
		if _, ok := created[skipped]; ok {
			t.Fatalf("%s must not be in the history: %v", skipped, created)
		}
	}

	// bob changes the amount: one update row, from and to
	r, _ = app.FindRecordById(c, r.Id)
	r.Set("amount", 250)
	if err := app.SaveWithContext(as(bob), r); err != nil {
		t.Fatal(err)
	}
	rows = historyOf(t, app, c, r.Id)
	if len(rows) != 2 || rows[1].Action != core.HistoryUpdate {
		t.Fatalf("expected an update row, got %+v", rows)
	}
	if ch := changesOf(t, rows[1]); ch["amount"]["from"] != float64(100) || ch["amount"]["to"] != float64(250) || len(ch) != 1 {
		t.Fatalf("update changes: %v", ch)
	}

	// a save that changes nothing a reader sees writes nothing
	r, _ = app.FindRecordById(c, r.Id)
	if err := app.SaveWithContext(as(bob), r); err != nil {
		t.Fatal(err)
	}
	r, _ = app.FindRecordById(c, r.Id)
	r.Set("secret", "a new margin") // hidden: not the reader's to see
	if err := app.SaveWithContext(as(bob), r); err != nil {
		t.Fatal(err)
	}
	if rows = historyOf(t, app, c, r.Id); len(rows) != 2 {
		t.Fatalf("a save with no visible change must not write history, got %d rows", len(rows))
	}

	// ann deletes it: the row keeps what it held
	if err := app.DeleteWithContext(as(ann), r); err != nil {
		t.Fatal(err)
	}
	rows = historyOf(t, app, c, r.Id)
	if len(rows) != 3 || rows[2].Action != core.HistoryDelete {
		t.Fatalf("expected a delete row, got %+v", rows)
	}
	if a := actorOf(t, rows[2]); a != ann {
		t.Fatalf("the delete row names %+v", a)
	}
	if ch := changesOf(t, rows[2]); ch["title"]["from"] != "Acme" || ch["amount"]["from"] != float64(250) {
		t.Fatalf("delete changes: %v", ch)
	}
}

func TestHistoryMergesOneWritersEdits(t *testing.T) {
	app, _ := tests.NewTestApp()
	defer app.Cleanup()

	c := dealsCollection(t, app, "deals", true)

	r := core.NewRecord(c)
	r.Set("title", "A")
	r.Set("amount", 1)
	if err := app.SaveWithContext(as(ann), r); err != nil {
		t.Fatal(err)
	}

	edit := func(a types.Actor, field string, v any) {
		t.Helper()
		rec, err := app.FindRecordById(c, r.Id)
		if err != nil {
			t.Fatal(err)
		}
		rec.Set(field, v)
		if err := app.SaveWithContext(as(a), rec); err != nil {
			t.Fatal(err)
		}
	}

	edit(ann, "amount", 2)  // update row
	edit(ann, "title", "B") // folds into it
	edit(ann, "amount", 1)  // amount back where it started: drops out

	rows := historyOf(t, app, c, r.Id)
	if len(rows) != 2 {
		t.Fatalf("one writer's edits within the window are one entry, got %d rows", len(rows))
	}
	ch := changesOf(t, rows[1])
	if len(ch) != 1 || ch["title"]["from"] != "A" || ch["title"]["to"] != "B" {
		t.Fatalf("merged changes: %v", ch)
	}

	// another writer starts an entry of its own
	edit(bob, "amount", 5)
	if rows = historyOf(t, app, c, r.Id); len(rows) != 3 {
		t.Fatalf("another writer's edit is its own entry, got %d rows", len(rows))
	}

	// past the window, the same writer starts a new entry too
	old := types.NowDateTime().Add(-11 * time.Minute)
	if _, err := app.DB().Update(core.CollectionNameHistory, dbx.Params{"created": old.String()}, dbx.HashExp{"id": rows[2].Id}).Execute(); err != nil {
		t.Fatal(err)
	}
	edit(bob, "amount", 6)
	if rows = historyOf(t, app, c, r.Id); len(rows) != 4 {
		t.Fatalf("an edit after the window is a new entry, got %d rows", len(rows))
	}

	// edits that put everything back leave no entry at all
	edit(ann, "title", "C")
	edit(ann, "title", "B")
	if rows = historyOf(t, app, c, r.Id); len(rows) != 4 {
		t.Fatalf("an entry left with no change is removed, got %d rows", len(rows))
	}
}

func TestHistoryIsOptIn(t *testing.T) {
	app, _ := tests.NewTestApp()
	defer app.Cleanup()

	c := dealsCollection(t, app, "quiet", false)

	r := core.NewRecord(c)
	r.Set("title", "nobody asked")
	if err := app.SaveWithContext(as(ann), r); err != nil {
		t.Fatal(err)
	}
	if rows := historyOf(t, app, c, r.Id); len(rows) != 0 {
		t.Fatalf("a collection without history keeps none, got %+v", rows)
	}
}

func TestHistoryCommitsWithTheRecord(t *testing.T) {
	app, _ := tests.NewTestApp()
	defer app.Cleanup()

	c := dealsCollection(t, app, "deals", true)

	first := core.NewRecord(c)
	first.Set("title", "taken")
	if err := app.SaveWithContext(as(ann), first); err != nil {
		t.Fatal(err)
	}

	// the record's write fails (unique title): no history either
	dup := core.NewRecord(c)
	dup.Set("title", "taken")
	if err := app.SaveNoValidateWithContext(as(ann), dup); err == nil {
		t.Fatal("expected the duplicate to be refused")
	}
	var n int
	if err := app.DB().Select("COUNT(*)").From(core.CollectionNameHistory).Where(dbx.HashExp{"collection": c.Id}).Row(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("a refused write must leave no history, got %d rows", n)
	}

	// the history's write fails: the record is not saved either
	if _, err := app.DB().NewQuery("DROP TABLE " + core.CollectionNameHistory).Execute(); err != nil {
		t.Fatal(err)
	}
	lost := core.NewRecord(c)
	lost.Set("title", "unrecorded")
	if err := app.SaveWithContext(as(ann), lost); err == nil {
		t.Fatal("expected the save to fail with its history")
	}
	if _, err := app.FindRecordById(c, lost.Id); err == nil {
		t.Fatal("a record whose history could not be written must not be saved")
	}
}
