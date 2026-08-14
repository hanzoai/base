package jsvm

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/dop251/goja"
	"github.com/hanzoai/base/core"
	"github.com/hanzoai/base/tests"
)

// firing is one call of an extension's hook: the Base it fired on, and what
// that Base could see through $app at the time.
type firing struct {
	dir  string
	rows string
}

// witness collects firings from the hook body. The executor pool runs a hook on
// whatever goroutine the write was on, so this is written from several.
type witness struct {
	mu   sync.Mutex
	list []firing
}

func (w *witness) saw(dir, rows string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.list = append(w.list, firing{dir: dir, rows: rows})
}

func (w *witness) rowsFor(dir string) []string {
	w.mu.Lock()
	defer w.mu.Unlock()

	var out []string
	for _, f := range w.list {
		if f.dir == dir {
			out = append(out, f.rows)
		}
	}
	return out
}

func (w *witness) dirs() []string {
	w.mu.Lock()
	defer w.mu.Unlock()

	seen := map[string]bool{}
	var out []string
	for _, f := range w.list {
		if !seen[f.dir] {
			seen[f.dir] = true
			out = append(out, f.dir)
		}
	}
	sort.Strings(out)
	return out
}

// The one hook the tests below load. It is tagged, so the Bases every other
// test in this package builds bind a hook that names a collection they do not
// have and therefore never fires.
const hookSource = `
	onRecordAfterCreateSuccess((e) => {
		e.next()

		const names = $app.findAllRecords("hooked").map((r) => r.get("name"))
		names.sort()

		__saw($app.dataDir(), names.join(","))
	}, "hooked")
`

// loadExtension writes hookSource into a hooks directory and registers the
// plugin against app, exactly as a deployment with a .base.js file does.
func loadExtension(t *testing.T, app core.App) *witness {
	t.Helper()

	hooksDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(hooksDir, "test.base.js"), []byte(hookSource), 0644); err != nil {
		t.Fatal(err)
	}

	w := &witness{}

	err := Register(app, Config{
		HooksDir:      hooksDir,
		MigrationsDir: filepath.Join(hooksDir, "migrations"),
		TypesDir:      t.TempDir(),
		HooksPoolSize: 1,
		OnInit: func(vm *goja.Runtime) {
			vm.Set("__saw", w.saw)
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	return w
}

// openBase builds a Base of its own, the way plugins/org opens an org's: a data
// directory, a constructor, a bootstrap. Nothing about it mentions extensions.
func openBase(t *testing.T) core.App {
	t.Helper()

	app := core.NewBaseApp(core.BaseAppConfig{DataDir: t.TempDir(), IsDev: true})
	if err := app.Bootstrap(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = app.ResetBootstrapState() })

	collection := core.NewBaseCollection("hooked")
	collection.Fields.Add(&core.TextField{Name: "name"})
	if err := app.Save(collection); err != nil {
		t.Fatal(err)
	}

	return app
}

// write puts one row into a Base's "hooked" collection.
func write(t *testing.T, app core.App, name string) {
	t.Helper()

	collection, err := app.FindCollectionByNameOrId("hooked")
	if err != nil {
		t.Fatal(err)
	}

	record := core.NewRecord(collection)
	record.Set("name", name)
	if err := app.Save(record); err != nil {
		t.Fatal(err)
	}
}

// A hook is a statement about a Base — "when a record is created here, do this"
// — and the Base it is about arrives with the event. So it holds for every Base
// this process opens, not only the one whose data directory the hook files were
// read from.
//
// Before this held, an operator's own guard, written in JS, ran on the platform
// Base alone: in a deployment where the tenants are the data, it fired for
// almost nothing and said nothing about it. JS migrations have always applied to
// every Base; a hook that did not was the odd half of one surface.
func TestAnExtensionsHooksReachEveryBase(t *testing.T) {
	app, _ := tests.NewTestApp()
	defer app.Cleanup()

	w := loadExtension(t, app)

	// A Base opened after the files were read.
	tenant := openBase(t)
	write(t, tenant, "t1")

	rows := w.rowsFor(tenant.DataDir())
	if len(rows) != 1 {
		t.Fatalf("the hook fired %d times on a Base opened after the extension loaded, want 1", len(rows))
	}
	if rows[0] != "t1" {
		t.Fatalf("the hook read %q, want t1", rows[0])
	}
}

// The Base a hook fires on is the Base it reads. Two tenants writing the same
// collection see their own rows and no others — the isolation is the file, and
// binding a hook on every Base does not reach across it.
func TestAHookSeesOnlyTheBaseItFiredOn(t *testing.T) {
	app, _ := tests.NewTestApp()
	defer app.Cleanup()

	w := loadExtension(t, app)

	first := openBase(t)
	second := openBase(t)

	write(t, first, "a1")
	write(t, second, "b1")
	write(t, second, "b2")

	if got, want := w.rowsFor(first.DataDir()), []string{"a1"}; !equal(got, want) {
		t.Fatalf("the first Base's hook saw %v, want %v", got, want)
	}
	if got, want := w.rowsFor(second.DataDir()), []string{"b1", "b1,b2"}; !equal(got, want) {
		t.Fatalf("the second Base's hook saw %v, want %v", got, want)
	}

	// Nothing fired anywhere but on the two Bases that were written to.
	for _, dir := range w.dirs() {
		if dir != first.DataDir() && dir != second.DataDir() {
			t.Fatalf("the hook fired on %q, which nothing wrote to", dir)
		}
	}

	// And neither Base's rows ever appeared in the other's firing.
	for _, rows := range w.rowsFor(first.DataDir()) {
		if strings.Contains(rows, "b") {
			t.Fatalf("the first Base's hook read the second's rows: %q", rows)
		}
	}
	for _, rows := range w.rowsFor(second.DataDir()) {
		if strings.Contains(rows, "a") {
			t.Fatalf("the second Base's hook read the first's rows: %q", rows)
		}
	}
}

// The Base the extension was loaded on keeps its hooks. Stating them on every
// Base is an addition, not a move.
func TestAnExtensionsHooksStayOnTheBaseThatLoadedIt(t *testing.T) {
	app, _ := tests.NewTestApp()
	defer app.Cleanup()

	w := loadExtension(t, app)

	collection := core.NewBaseCollection("hooked")
	collection.Fields.Add(&core.TextField{Name: "name"})
	if err := app.Save(collection); err != nil {
		t.Fatal(err)
	}
	write(t, app, "p1")

	if got, want := w.rowsFor(app.DataDir()), []string{"p1"}; !equal(got, want) {
		t.Fatalf("the loading Base's hook saw %v, want %v", got, want)
	}
}

// A tick arrives on no Base and a process has one router, so cronAdd and
// routerAdd have nothing per-Base to be handed and stay where they are said.
// A hook is handed the Base its event fired on, which is the whole difference.
func TestACronAndARouteStayOnTheBaseThatLoadedThem(t *testing.T) {
	app, _ := tests.NewTestApp()
	defer app.Cleanup()

	hooksDir := t.TempDir()
	source := `
		cronAdd("extension", "0 0 * * *", () => {})
		routerAdd("GET", "/extension", (e) => e.json(200, {}))
	`
	if err := os.WriteFile(filepath.Join(hooksDir, "test.base.js"), []byte(source), 0644); err != nil {
		t.Fatal(err)
	}

	err := Register(app, Config{
		HooksDir:      hooksDir,
		MigrationsDir: filepath.Join(hooksDir, "migrations"),
		TypesDir:      t.TempDir(),
		HooksPoolSize: 1,
	})
	if err != nil {
		t.Fatal(err)
	}

	if !app.Cron().HasJob("extension") {
		t.Fatal("the Base that loaded the extension did not get its cron job")
	}

	tenant := openBase(t)
	if tenant.Cron().HasJob("extension") {
		t.Fatal("an extension's cron job was registered on a second Base")
	}
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
