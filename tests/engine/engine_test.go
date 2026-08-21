// Package engine runs Base's data plane against the SQL engine underneath it
// and asserts the same answers whichever engine that is.
//
// Every fixture here is built through Base's own API instead of being read from
// tests/data. That directory is a SQLite file — it is the reason the rest of the
// suite cannot be pointed at a server, and a corpus that inherited it could only
// ever test one engine.
//
// So these tests carry no engine in them. They run on the embedded file under a
// plain `go test ./...`, and on PostgreSQL when BASE_TEST_POSTGRES names a
// server; the assertions do not change between the two, which is the point. The
// one place an engine is legitimately visible — whether a backup can hold the
// database it is backing up — says so out loud rather than averaging over it.
//
// Ordering by a TEXT column is deliberately absent. Which of "Alpha" and "beta"
// sorts first is the collation's answer, not Base's: SQLite compares bytes,
// while a PostgreSQL database compares by the locale it was created with. That
// is a property of the database an operator provisions, so a corpus that pinned
// one of them would be asserting a deployment choice. Ordering by a number,
// which both engines agree on, is what the paging tests use.
package engine

import (
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/hanzoai/base/core"
	"github.com/hanzoai/base/tests"
)

func newApp(t *testing.T) *tests.TestApp {
	t.Helper()

	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(app.Cleanup)

	return app
}

// items is the corpus every case below is asked about: one collection covering
// the field kinds whose comparisons the two engines spell differently, and three
// records whose ranks order them unambiguously.
func items(t *testing.T, app *tests.TestApp) *core.Collection {
	t.Helper()

	c := core.NewBaseCollection("items")
	c.Fields.Add(
		&core.TextField{Name: "title"},
		&core.BoolField{Name: "live"},
		&core.NumberField{Name: "rank"},
		&core.JSONField{Name: "meta", MaxSize: 1000},
	)
	c.AddIndex("idx_items_title", false, "title", "")

	if err := app.Save(c); err != nil {
		t.Fatalf("save collection: %v", err)
	}

	for _, r := range []struct {
		title string
		live  bool
		rank  float64
		meta  string
	}{
		{"Alpha", true, 1, `{"tier":"gold","n":3}`},
		{"beta", false, 2, `{"tier":"silver","n":10}`},
		{"GAMMA", true, 30, `{"tier":"gold","n":2}`},
	} {
		rec := core.NewRecord(c)
		rec.Set("title", r.title)
		rec.Set("live", r.live)
		rec.Set("rank", r.rank)
		rec.Set("meta", r.meta)
		if err := app.Save(rec); err != nil {
			t.Fatalf("save %s: %v", r.title, err)
		}
	}

	return c
}

func titles(records []*core.Record) []string {
	out := make([]string, len(records))
	for i, r := range records {
		out[i] = r.GetString("title")
	}
	return out
}

// A filter is a question about the data, so its answer belongs to the data and
// not to the engine holding it. These are the comparisons the two engines spell
// differently — a case-insensitive match, a truth value, a number, and a path
// into a JSON document — asked once and expected to answer the same twice.
//
// Ranked rather than sorted: every case names the rows it wants, and rank orders
// them, so a case tests the filter and nothing else.
func TestFilter(t *testing.T) {
	app := newApp(t)
	items(t, app)

	for _, s := range []struct {
		filter string
		want   []string
	}{
		{`title ~ "a"`, []string{"Alpha", "beta", "GAMMA"}},
		{`title ~ "AL"`, []string{"Alpha"}},
		{`title ~ "mm"`, []string{"GAMMA"}},
		{`title = "Alpha"`, []string{"Alpha"}},
		{`title != "Alpha"`, []string{"beta", "GAMMA"}},
		{`live = true`, []string{"Alpha", "GAMMA"}},
		{`live = false`, []string{"beta"}},
		{`live != true`, []string{"beta"}},
		{`rank > 1`, []string{"beta", "GAMMA"}},
		{`rank >= 2 && rank < 30`, []string{"beta"}},
		{`rank <= 2`, []string{"Alpha", "beta"}},
		{`meta.tier = "gold"`, []string{"Alpha", "GAMMA"}},
		{`meta.tier != "gold"`, []string{"beta"}},
		{`meta.tier ~ "GOLD"`, []string{"Alpha", "GAMMA"}},
		// A number reached through a JSON path: the document stores text and the
		// literal is a number, so this is the comparison that needs the engine
		// asked how it spells one.
		{`meta.n = 10`, []string{"beta"}},
		{`meta.n > 2`, []string{"Alpha", "beta"}},
		{`title ~ "a" && live = true`, []string{"Alpha", "GAMMA"}},
		{`rank = 1 || rank = 30`, []string{"Alpha", "GAMMA"}},
		{`title = "nobody"`, nil},
	} {
		t.Run(s.filter, func(t *testing.T) {
			got, err := app.FindRecordsByFilter("items", s.filter, "rank", 0, 0)
			if err != nil {
				t.Fatalf("%s: %v", s.filter, err)
			}

			if have := titles(got); !slices.Equal(have, s.want) {
				t.Fatalf("got %v, want %v", have, s.want)
			}
		})
	}
}

// Paging and sorting are the other half of a list, and an engine that ordered
// differently would return a different page for the same request.
func TestPage(t *testing.T) {
	app := newApp(t)
	items(t, app)

	for _, s := range []struct {
		sort   string
		limit  int
		offset int
		want   []string
	}{
		{"rank", 2, 0, []string{"Alpha", "beta"}},
		{"rank", 2, 1, []string{"beta", "GAMMA"}},
		{"rank", 2, 2, []string{"GAMMA"}},
		{"-rank", 1, 0, []string{"GAMMA"}},
		{"-rank", 3, 0, []string{"GAMMA", "beta", "Alpha"}},
	} {
		t.Run(fmt.Sprintf("%s/%d/%d", s.sort, s.limit, s.offset), func(t *testing.T) {
			got, err := app.FindRecordsByFilter("items", "", s.sort, s.limit, s.offset)
			if err != nil {
				t.Fatal(err)
			}
			if have := titles(got); !slices.Equal(have, s.want) {
				t.Fatalf("got %v, want %v", have, s.want)
			}
		})
	}
}

// A collection is a real table with real columns and a real index, whichever
// engine holds it, and both are read back through the same catalog the schema is
// managed with.
func TestSchema(t *testing.T) {
	app := newApp(t)
	items(t, app)

	cols, err := app.TableColumns("items")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"id", "title", "live", "rank", "meta"} {
		if !slices.Contains(cols, name) {
			t.Fatalf("column %q missing from %v", name, cols)
		}
	}

	idx, err := app.TableIndexes("items")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := idx["idx_items_title"]; !ok {
		t.Fatalf("declared index missing from %v", idx)
	}
}

// Updates and deletes reach the row they name.
func TestWrite(t *testing.T) {
	app := newApp(t)
	items(t, app)

	got, err := app.FindRecordsByFilter("items", `title = "beta"`, "", 0, 0)
	if err != nil || len(got) != 1 {
		t.Fatalf("find beta: %v %v", titles(got), err)
	}

	got[0].Set("rank", float64(99))
	if err := app.Save(got[0]); err != nil {
		t.Fatalf("update: %v", err)
	}

	after, err := app.FindRecordsByFilter("items", `rank = 99`, "", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if have := titles(after); !slices.Equal(have, []string{"beta"}) {
		t.Fatalf("after update got %v, want [beta]", have)
	}

	if err := app.Delete(after[0]); err != nil {
		t.Fatalf("delete: %v", err)
	}

	rest, err := app.FindRecordsByFilter("items", "", "rank", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if have := titles(rest); !slices.Equal(have, []string{"Alpha", "GAMMA"}) {
		t.Fatalf("after delete got %v, want [Alpha GAMMA]", have)
	}
}

// A field added to a live collection is queryable at once. An engine that
// prepared a statement against the old shape of the table, and kept it, would
// answer this with a column that does not exist.
func TestFieldAdded(t *testing.T) {
	app := newApp(t)
	c := items(t, app)

	// read first, so a statement for this table is already prepared
	if _, err := app.FindRecordsByFilter("items", `title != ""`, "", 0, 0); err != nil {
		t.Fatal(err)
	}

	c.Fields.Add(&core.TextField{Name: "note"})
	if err := app.Save(c); err != nil {
		t.Fatalf("add field: %v", err)
	}

	rec := core.NewRecord(c)
	rec.Set("title", "Delta")
	rec.Set("note", "hello")
	if err := app.Save(rec); err != nil {
		t.Fatalf("save with new field: %v", err)
	}

	got, err := app.FindRecordsByFilter("items", `note = "hello"`, "", 0, 0)
	if err != nil {
		t.Fatalf("filter on new field: %v", err)
	}
	if have := titles(got); !slices.Equal(have, []string{"Delta"}) {
		t.Fatalf("got %v, want [Delta]", have)
	}
}

// Writers arriving together all land. One write connection serialises them and a
// pool does not, so on an engine whose pool is free this is the path where two
// transactions can take rows in opposite orders — a failure the writer is
// expected to retry rather than return.
func TestConcurrentWrites(t *testing.T) {
	app := newApp(t)
	c := items(t, app)

	const writers = 8

	var wg sync.WaitGroup
	errs := make(chan error, writers)

	for i := range writers {
		wg.Add(1)
		go func() {
			defer wg.Done()

			rec := core.NewRecord(c)
			rec.Set("title", fmt.Sprintf("w%d", i))
			rec.Set("rank", float64(100+i))
			if err := app.Save(rec); err != nil {
				errs <- fmt.Errorf("writer %d: %w", i, err)
			}
		}()
	}

	wg.Wait()
	close(errs)

	for err := range errs {
		t.Error(err)
	}

	got, err := app.FindRecordsByFilter("items", `rank >= 100`, "rank", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != writers {
		t.Fatalf("got %d of %d writes: %v", len(got), writers, titles(got))
	}
}

// Settings hold secrets, so they are written encrypted and have to survive the
// round trip through whatever column the engine gave them.
func TestSettings(t *testing.T) {
	t.Setenv("hz_test_env", strings.Repeat("a", 32))

	app := newApp(t)

	app.Settings().Meta.AppName = "engine"
	app.Settings().SMTP.Password = "s3cret"
	if err := app.Save(app.Settings()); err != nil {
		t.Fatalf("save settings: %v", err)
	}

	if err := app.ReloadSettings(); err != nil {
		t.Fatalf("reload settings: %v", err)
	}

	if got := app.Settings().Meta.AppName; got != "engine" {
		t.Fatalf("app name did not survive: %q", got)
	}
	if got := app.Settings().SMTP.Password; got != "s3cret" {
		t.Fatalf("secret did not survive: %q", got)
	}

	// stored encrypted, not as the value that was handed over
	var stored string
	if err := app.DB().NewQuery("SELECT value FROM _params WHERE id = 'settings'").Row(&stored); err != nil {
		t.Fatalf("read stored settings: %v", err)
	}
	if strings.Contains(stored, "s3cret") {
		t.Fatal("the stored settings carry the secret in the clear")
	}
}

// A backup is an archive of the data directory, so it is a backup of the
// database only while the engine keeps the database there. An engine that keeps
// it in a server refuses, naming itself, rather than writing an archive that
// would restore nothing.
func TestBackup(t *testing.T) {
	app := newApp(t)

	err := app.CreateBackup(t.Context(), "engine.zip")

	if tests.Postgres() {
		if err == nil {
			t.Fatal("expected a server-held database to refuse a backup")
		}
		if !strings.Contains(err.Error(), "postgres") {
			t.Fatalf("expected the refusal to name the engine, got %q", err)
		}
		return
	}

	if err != nil {
		t.Fatalf("expected a file-held database to archive itself, got %v", err)
	}
}

// What a Base can say about where it keeps records is one report whichever
// engine is under it: the same collections, counted the same way. The engine is
// visible in exactly one place, and it is the place a host has to act on — an
// embedded database is files the app can size, back up and rewrite, while a
// server's are the server's — so that is stated rather than averaged over.
func TestDescribe(t *testing.T) {
	app := newApp(t)
	items(t, app)

	db, err := core.DescribeDatabase(app)
	if err != nil {
		t.Fatalf("describe: %v", err)
	}

	if db.Engine != app.Dialect().Name() {
		t.Fatalf("expected the report to name the connected engine %q, got %q", app.Dialect().Name(), db.Engine)
	}

	var counted *int64
	for _, c := range db.Collections {
		if c.Name == "items" {
			counted = c.Records
		}
	}
	if counted == nil {
		t.Fatal("expected the report to count the collection that was just written")
	}
	if *counted != 3 {
		t.Fatalf("expected the 3 records written, got %d", *counted)
	}

	if tests.Postgres() {
		if db.Local {
			t.Fatal("expected a server-held database to report that the app does not hold it")
		}
		if db.Data.Path != "" || db.Data.Size != 0 {
			t.Fatalf("expected no file to be named for a server-held database, got %q at %d bytes", db.Data.Path, db.Data.Size)
		}
		return
	}

	if !db.Local {
		t.Fatal("expected a file-held database to report that the app holds it")
	}
	if !strings.HasPrefix(db.Data.Path, app.DataDir()) {
		t.Fatalf("expected the database to be named in the data directory %q, got %q", app.DataDir(), db.Data.Path)
	}
	if db.Data.Size == 0 || db.Aux.Size == 0 {
		t.Fatalf("expected both files to be sized, got data=%d aux=%d", db.Data.Size, db.Aux.Size)
	}
}

// A reclaim is the app rewriting what it holds, so it runs on whichever engine
// holds it and reports what it freed. On a server it holds nothing, and says so
// with a zero rather than by refusing: the rewrite still happens, at the server,
// which is also where its size is read.
func TestReclaim(t *testing.T) {
	app := newApp(t)
	items(t, app)

	before, after, err := core.ReclaimDatabase(app)
	if err != nil {
		t.Fatalf("reclaim: %v", err)
	}

	if tests.Postgres() {
		if before != 0 || after != 0 {
			t.Fatalf("expected a server-held database to be no size of the app's, got %d then %d", before, after)
		}
		return
	}

	if before == 0 || after == 0 {
		t.Fatalf("expected a file-held database to be sized either side of the rewrite, got %d then %d", before, after)
	}
	if after > before {
		t.Fatalf("expected the rewrite to leave no more than it started with, got %d from %d", after, before)
	}
}
