package base

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/hanzoai/base/tools/filesystem"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
)

// TestHostPoolIsTheOneBaseUses: a host that hands Base its own PostgreSQL pool
// gets a Base whose tables are in that pool's schema and nowhere else, with no
// file beside it; and a host that hands Base its filesystem is the one uploads
// go to.
func TestHostPoolIsTheOneBaseUses(t *testing.T) {
	server := os.Getenv("BASE_TEST_POSTGRES")
	if server == "" {
		t.Skip("BASE_TEST_POSTGRES names no server")
	}
	admin, err := sql.Open("pgx", server)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	schema := fmt.Sprintf("hz_host_pool_%d", os.Getpid())
	if _, err := admin.Exec(`CREATE SCHEMA ` + schema); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = admin.Exec(`DROP SCHEMA ` + schema + ` CASCADE`) })

	asked := 0
	open := func() (*sql.DB, error) {
		asked++
		cfg, err := pgx.ParseConfig(server)
		if err != nil {
			return nil, err
		}
		cfg.RuntimeParams["search_path"] = schema
		cfg.DefaultQueryExecMode = pgx.QueryExecModeDescribeExec
		return stdlib.OpenDB(*cfg), nil
	}
	files := t.TempDir()
	filesAsked := 0

	dir := t.TempDir()
	app := NewWithConfig(Config{
		DefaultDataDir: dir, HideStartBanner: true, DataDB: open, AuxDB: open,
		Files: func() (*filesystem.System, error) { filesAsked++; return filesystem.NewLocal(files) },
	})
	if err := app.Bootstrap(); err != nil {
		t.Fatal(err)
	}
	defer app.ResetBootstrapState()
	if err := app.RunAllMigrations(); err != nil {
		t.Fatal(err)
	}
	if asked != 4 {
		t.Fatalf("Base asked the host for %d pools; two databases, readers and writer, is four", asked)
	}
	var n int
	if err := admin.QueryRow(`SELECT count(*) FROM information_schema.tables WHERE table_schema = $1 AND table_name = '_collections'`, schema).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("_collections is not in the host's schema")
	}
	if _, err := os.Stat(filepath.Join(dir, "data.db")); err == nil {
		t.Fatalf("a file was made beside a Base the host placed")
	}

	fsys, err := app.NewFilesystem()
	if err != nil {
		t.Fatal(err)
	}
	defer fsys.Close()
	if filesAsked != 1 {
		t.Fatalf("NewFilesystem did not ask the host")
	}
	if _, err := os.Stat(filepath.Join(dir, "storage")); err == nil {
		t.Fatalf("a storage directory was made in the Base's own directory")
	}
}
