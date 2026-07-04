package replicator_test

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"github.com/hanzoai/base/store/replicator"
	"github.com/hanzoai/replicate"
	"github.com/hanzoai/replicate/file"
	"github.com/hanzoai/sqlite"
)

func openSQL(t *testing.T, path string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", sqlite.PragmaDSN(path, sqlite.DefaultPragmas))
	if err != nil {
		t.Fatal(err)
	}
	return db
}

func wipeLocal(t *testing.T, dbPath string) {
	t.Helper()
	for _, suf := range []string{"", "-wal", "-shm"} {
		if err := os.Remove(dbPath + suf); err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
	}
	if err := os.RemoveAll(replicate.NewDB(dbPath).MetaPath()); err != nil {
		t.Fatal(err)
	}
}

// The task's headline HA test: write -> replicate -> wipe local (pod death) ->
// restore -> read. Proves a tenant DB survives losing its pod.
func TestRoundTrip_WriteReplicateWipeRestoreRead(t *testing.T) {
	ctx := context.Background()
	client := file.NewReplicaClient(t.TempDir())
	dbPath := filepath.Join(t.TempDir(), "tenant.db")

	// 1) write initial data
	w := openSQL(t, dbPath)
	if _, err := w.ExecContext(ctx, `CREATE TABLE t(v TEXT)`); err != nil {
		t.Fatal(err)
	}
	if _, err := w.ExecContext(ctx, `INSERT INTO t VALUES ('persist-me')`); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	// 2) stream to the replica
	h, err := replicator.Open(dbPath, client)
	if err != nil {
		t.Fatal(err)
	}
	if err := h.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	if err := h.Close(ctx); err != nil {
		t.Fatal(err)
	}

	// 3) pod death — wipe local DB, WAL, meta
	wipeLocal(t, dbPath)
	if _, err := os.Stat(dbPath); !os.IsNotExist(err) {
		t.Fatal("local DB not wiped")
	}

	// 4) restore from the replica
	restored, err := replicator.Restore(ctx, dbPath, client)
	if err != nil {
		t.Fatalf("restore: %v", err)
	}
	if !restored {
		t.Fatal("expected restored=true (replica existed)")
	}

	// 5) read back
	r := openSQL(t, dbPath)
	defer r.Close()
	var v string
	if err := r.QueryRowContext(ctx, `SELECT v FROM t`).Scan(&v); err != nil {
		t.Fatalf("read after restore: %v", err)
	}
	if v != "persist-me" {
		t.Fatalf("got %q, want persist-me", v)
	}
}

// A fresh tenant with no replica yet restores to (false, nil) — the store then
// creates the DB locally rather than failing.
func TestRestore_NoReplicaReturnsFalse(t *testing.T) {
	ctx := context.Background()
	client := file.NewReplicaClient(t.TempDir()) // empty replica dir
	dbPath := filepath.Join(t.TempDir(), "fresh.db")

	restored, err := replicator.Restore(ctx, dbPath, client)
	if err != nil {
		t.Fatalf("restore of empty replica: %v", err)
	}
	if restored {
		t.Fatal("expected restored=false for a fresh tenant with no replica")
	}
}

// A second write cycle after restore replicates and restores again — proves
// the DB keeps streaming after a failover (no orphaned replication state).
func TestRoundTrip_ReplicateAfterRestore(t *testing.T) {
	ctx := context.Background()
	client := file.NewReplicaClient(t.TempDir())
	dbPath := filepath.Join(t.TempDir(), "tenant.db")

	w := openSQL(t, dbPath)
	if _, err := w.ExecContext(ctx, `CREATE TABLE t(v INTEGER)`); err != nil {
		t.Fatal(err)
	}
	if _, err := w.ExecContext(ctx, `INSERT INTO t VALUES (1)`); err != nil {
		t.Fatal(err)
	}
	w.Close()

	h, err := replicator.Open(dbPath, client)
	if err != nil {
		t.Fatal(err)
	}
	if err := h.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	h.Close(ctx)
	wipeLocal(t, dbPath)

	if _, err := replicator.Restore(ctx, dbPath, client); err != nil {
		t.Fatal(err)
	}

	// second cycle: write more, replicate, wipe, restore
	w2 := openSQL(t, dbPath)
	if _, err := w2.ExecContext(ctx, `INSERT INTO t VALUES (2)`); err != nil {
		t.Fatal(err)
	}
	w2.Close()

	h2, err := replicator.Open(dbPath, client)
	if err != nil {
		t.Fatal(err)
	}
	if err := h2.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	h2.Close(ctx)
	wipeLocal(t, dbPath)

	if _, err := replicator.Restore(ctx, dbPath, client); err != nil {
		t.Fatal(err)
	}

	r := openSQL(t, dbPath)
	defer r.Close()
	var n int
	if err := r.QueryRowContext(ctx, `SELECT count(*) FROM t`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("expected 2 rows after two replicate/restore cycles, got %d", n)
	}
}
