package replicator_test

import (
	"bytes"
	"context"
	"database/sql"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/hanzoai/base/store/encreplica"
	"github.com/hanzoai/base/store/replicator"
	"github.com/hanzoai/replicate"
	"github.com/hanzoai/sqlite"
	"github.com/luxfi/age"
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

// tenantClient builds a per-tenant age-encrypting replica client over a local
// blob dir — the SAME shape used in production (with an S3/vfs backend).
func tenantClient(t *testing.T, replicaDir string, id *age.HybridIdentity) *encreplica.Client {
	t.Helper()
	c, err := encreplica.New(encreplica.NewLocalBlobs(replicaDir), id.Recipient(), id)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// scanNoPlaintext walks the replica storage and fails if the marker appears in
// the clear; returns how many age-encrypted blobs it saw.
func scanNoPlaintext(t *testing.T, root string, marker []byte) (ageBlobs int) {
	t.Helper()
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, rerr := os.ReadFile(p)
		if rerr != nil {
			return rerr
		}
		if bytes.Contains(b, marker) {
			t.Fatalf("PLAINTEXT LEAK: marker %q found in replica file %s", marker, p)
		}
		if bytes.Contains(b, []byte("SQLite format 3")) {
			t.Fatalf("PLAINTEXT LEAK: raw SQLite header in replica file %s", p)
		}
		if bytes.Contains(b, []byte("age-encryption.org/v1")) {
			ageBlobs++
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return ageBlobs
}

// The critical test Red demanded: after replicating a marker row through the
// WIRED encrypting client, the replica bytes contain NO plaintext marker (and
// no raw SQLite), and every LTX blob is age ciphertext. Mirrors keyring Proof 10.
func TestReplicaBytesAreEncrypted_NoPlaintextMarker(t *testing.T) {
	ctx := context.Background()
	marker := []byte("MARKER-PLAINTEXT-9f3a-DO-NOT-LEAK")

	id, err := age.GenerateHybridIdentity()
	if err != nil {
		t.Fatal(err)
	}
	replicaDir := t.TempDir()
	client := tenantClient(t, replicaDir, id)
	dbPath := filepath.Join(t.TempDir(), "tenant.db")

	w := openSQL(t, dbPath)
	if _, err := w.ExecContext(ctx, `CREATE TABLE t(v TEXT)`); err != nil {
		t.Fatal(err)
	}
	if _, err := w.ExecContext(ctx, `INSERT INTO t VALUES ('`+string(marker)+`')`); err != nil {
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
	if err := h.Close(ctx); err != nil {
		t.Fatal(err)
	}

	ageBlobs := scanNoPlaintext(t, replicaDir, marker)
	if ageBlobs == 0 {
		t.Fatal("no age-encrypted LTX blob was written — replica may be empty or unencrypted")
	}
}

// Headline HA test through the encrypting client: write -> replicate -> wipe
// local (pod death) -> restore -> read.
func TestRoundTrip_WriteReplicateWipeRestoreRead(t *testing.T) {
	ctx := context.Background()
	id, err := age.GenerateHybridIdentity()
	if err != nil {
		t.Fatal(err)
	}
	client := tenantClient(t, t.TempDir(), id)
	dbPath := filepath.Join(t.TempDir(), "tenant.db")

	w := openSQL(t, dbPath)
	if _, err := w.ExecContext(ctx, `CREATE TABLE t(v TEXT)`); err != nil {
		t.Fatal(err)
	}
	if _, err := w.ExecContext(ctx, `INSERT INTO t VALUES ('persist-me')`); err != nil {
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
	if _, err := os.Stat(dbPath); !os.IsNotExist(err) {
		t.Fatal("local DB not wiped")
	}

	restored, err := replicator.Restore(ctx, dbPath, client)
	if err != nil {
		t.Fatalf("restore: %v", err)
	}
	if !restored {
		t.Fatal("expected restored=true (replica existed)")
	}

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

// A different tenant's key cannot restore the replica — the stream is bound to
// the per-tenant age key (age-decrypt fails inside the client).
func TestRestore_CrossTenantFails(t *testing.T) {
	ctx := context.Background()
	replicaDir := t.TempDir()

	idA, err := age.GenerateHybridIdentity()
	if err != nil {
		t.Fatal(err)
	}
	clientA := tenantClient(t, replicaDir, idA)
	dbPath := filepath.Join(t.TempDir(), "tenant.db")

	w := openSQL(t, dbPath)
	if _, err := w.ExecContext(ctx, `CREATE TABLE t(v TEXT)`); err != nil {
		t.Fatal(err)
	}
	if _, err := w.ExecContext(ctx, `INSERT INTO t VALUES ('secret')`); err != nil {
		t.Fatal(err)
	}
	w.Close()

	h, err := replicator.Open(dbPath, clientA)
	if err != nil {
		t.Fatal(err)
	}
	if err := h.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	h.Close(ctx)
	wipeLocal(t, dbPath)

	// Tenant B, same replica storage, different key — must NOT restore.
	idB, err := age.GenerateHybridIdentity()
	if err != nil {
		t.Fatal(err)
	}
	clientB := tenantClient(t, replicaDir, idB)
	if _, err := replicator.Restore(ctx, dbPath, clientB); err == nil {
		t.Fatal("ISOLATION BREACH: cross-tenant restore succeeded")
	}
}

func TestRestore_NoReplicaReturnsFalse(t *testing.T) {
	ctx := context.Background()
	id, err := age.GenerateHybridIdentity()
	if err != nil {
		t.Fatal(err)
	}
	client := tenantClient(t, t.TempDir(), id)
	dbPath := filepath.Join(t.TempDir(), "fresh.db")

	restored, err := replicator.Restore(ctx, dbPath, client)
	if err != nil {
		t.Fatalf("restore of empty replica: %v", err)
	}
	if restored {
		t.Fatal("expected restored=false for a fresh tenant with no replica")
	}
}

func TestRoundTrip_ReplicateAfterRestore(t *testing.T) {
	ctx := context.Background()
	id, err := age.GenerateHybridIdentity()
	if err != nil {
		t.Fatal(err)
	}
	client := tenantClient(t, t.TempDir(), id)
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
