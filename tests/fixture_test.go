package tests

import (
	"database/sql"
	"path"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/hanzoai/base/core"
)

// The fixture every test app copies already carries every migration this tree
// registers, so no test app spends its first seconds running them. A migration
// added without `go run ./tests/fixture` fails here, naming it.
//
// It reads the fixture's own record of what was applied rather than opening an
// app on it, because opening one is what runs the migrations.
func TestTheFixtureIsMigrated(t *testing.T) {
	_, file, _, _ := runtime.Caller(0)

	db, err := sql.Open("sqlite", "file:"+filepath.Join(path.Dir(file), "data", "data.db")+"?mode=ro&immutable=1")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	rows, err := db.Query("SELECT file FROM _migrations")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()

	applied := map[string]bool{}
	for rows.Next() {
		var f string
		if err := rows.Scan(&f); err != nil {
			t.Fatal(err)
		}
		applied[f] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}

	list := core.MigrationsList{}
	list.Copy(core.SystemMigrations)
	list.Copy(core.AppMigrations)

	var missing []string
	for _, m := range list.Items() {
		if !applied[m.File] {
			missing = append(missing, m.File)
		}
	}
	if len(missing) > 0 {
		t.Fatalf("tests/data lacks %d migrations, which every test app would run: %v\nrun `go run ./tests/fixture`", len(missing), missing)
	}
}
