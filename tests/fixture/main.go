// Command fixture brings tests/data up to the migrations this tree registers.
//
//	go run ./tests/fixture
//
// Every test app is a copy of tests/data, and a copy that lacks a migration runs
// it before the test begins. Run per app, a dozen migrations cost each one about
// four seconds, which is how core and apis stopped fitting the runner's limit: the
// work was the same in every test and none of it was the test. The fixture records
// the schema the migrations produce, so a test app opens it instead of building it,
// and tests.TestTheFixtureIsMigrated fails when a new migration is not in it yet.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/hanzoai/base/tests"
	"github.com/hanzoai/dbx"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "fixture:", err)
		os.Exit(1)
	}
}

func run() error {
	_, file, _, _ := runtime.Caller(0)
	dir := filepath.Join(filepath.Dir(file), "..", "data")

	// a test app is the fixture with every pending migration applied
	app, err := tests.NewTestApp()
	if err != nil {
		return err
	}
	defer app.Cleanup()

	for _, db := range []struct {
		name string
		db   dbx.Builder
	}{
		{"data.db", app.NonconcurrentDB()},
		{"auxiliary.db", app.AuxNonconcurrentDB()},
	} {
		dst := filepath.Join(dir, db.name)
		tmp := dst + ".next"
		_ = os.Remove(tmp)

		// VACUUM INTO writes one self-contained file, with nothing left in a WAL
		if _, err := db.db.NewQuery("VACUUM INTO {:path}").Bind(dbx.Params{"path": tmp}).Execute(); err != nil {
			return fmt.Errorf("%s: %w", db.name, err)
		}
		if err := os.Rename(tmp, dst); err != nil {
			return fmt.Errorf("%s: %w", db.name, err)
		}
		fmt.Println("wrote", dst)
	}

	return nil
}
