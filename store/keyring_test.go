package store_test

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/hanzoai/base/store"
	"github.com/hanzoai/base/tools/filesystem"
	"github.com/luxfi/age"
)

// sidecarSuffix mirrors the unexported store constant — the on-disk contract
// for where a DB's wrapped key material lives.
const sidecarSuffix = ".agekey"

func localFS(t *testing.T) *filesystem.System {
	t.Helper()
	fs, err := filesystem.NewLocal(filepath.Join(t.TempDir(), "bucket"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	return fs
}

// ageSeal/ageOpen exercise a TenantKey at the exact at-rest boundary (luxfi/age).
func ageSeal(t *testing.T, tk *store.TenantKey, msg []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	w, err := age.Encrypt(&buf, tk.Recipient)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write(msg); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func ageOpen(tk *store.TenantKey, ct []byte) ([]byte, error) {
	r, err := age.Decrypt(bytes.NewReader(ct), tk.Identity)
	if err != nil {
		return nil, err
	}
	return io.ReadAll(r)
}

func sidecarBytes(t *testing.T, fs *filesystem.System, sk string) []byte {
	t.Helper()
	r, err := fs.GetReader(sk)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	b, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// PROOF 1: org A cannot decrypt org B's DB. Distinct KEKs → distinct keys.
func TestKeyring_CrossOrgDecryptFails(t *testing.T) {
	fs := localFS(t)
	kr, err := store.NewKeyring(newMemRoot(), fs)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	ta, err := kr.Resolve(ctx, store.Key{OrgID: "orga", UserID: "u", Scope: store.ScopeUser})
	if err != nil {
		t.Fatal(err)
	}
	tb, err := kr.Resolve(ctx, store.Key{OrgID: "orgb", UserID: "u", Scope: store.ScopeUser})
	if err != nil {
		t.Fatal(err)
	}

	msg := []byte("orga confidential rows")
	ct := ageSeal(t, ta, msg)

	if _, err := ageOpen(tb, ct); err == nil {
		t.Fatal("ISOLATION BREACH: org B key decrypted org A ciphertext")
	}
	pt, err := ageOpen(ta, ct)
	if err != nil || !bytes.Equal(pt, msg) {
		t.Fatalf("org A must decrypt its own data: %v", err)
	}
}

// PROOF 2: within one org, every scope/tenant gets an independent key and no
// ciphertext is cross-decryptable.
func TestKeyring_SameOrgTenantsIsolated(t *testing.T) {
	fs := localFS(t)
	kr, err := store.NewKeyring(newMemRoot(), fs)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	keys := []store.Key{
		{OrgID: "acme", UserID: "alice", Scope: store.ScopeUser},
		{OrgID: "acme", UserID: "bob", Scope: store.ScopeUser},
		{OrgID: "acme", Scope: store.ScopeOrg},
		{OrgID: "acme", App: "fleet", Scope: store.ScopeApp},
		{OrgID: "acme", App: "fleet", Project: "east", Scope: store.ScopeProject},
	}
	tks := make([]*store.TenantKey, len(keys))
	for i, k := range keys {
		tk, err := kr.Resolve(ctx, k)
		if err != nil {
			t.Fatalf("resolve %s: %v", k, err)
		}
		tks[i] = tk
	}

	for i := range keys {
		ct := ageSeal(t, tks[i], []byte("payload for "+keys[i].String()))
		for j := range keys {
			_, err := ageOpen(tks[j], ct)
			switch {
			case i == j && err != nil:
				t.Fatalf("tenant %s failed to self-decrypt: %v", keys[i], err)
			case i != j && err == nil:
				t.Fatalf("ISOLATION BREACH: %s decrypted %s ciphertext", keys[j], keys[i])
			}
		}
	}
}

// PROOF 3: a tampered sidecar is rejected (AES-256-GCM authentication).
func TestKeyring_TamperedSidecarRejected(t *testing.T) {
	fs := localFS(t)
	root := newMemRoot()
	kr, err := store.NewKeyring(root, fs)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	k := store.Key{OrgID: "acme", UserID: "alice", Scope: store.ScopeUser}
	if _, err := kr.Resolve(ctx, k); err != nil {
		t.Fatal(err)
	}

	sk := k.ObjectKey() + sidecarSuffix
	blob := sidecarBytes(t, fs, sk)
	blob[len(blob)-1] ^= 0xff // flip a ciphertext/tag byte
	if err := fs.Upload(blob, sk); err != nil {
		t.Fatal(err)
	}

	// Fresh keyring (no cache), same KEK: unwrap of the tampered blob must fail.
	kr2, err := store.NewKeyring(root, fs)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := kr2.Resolve(ctx, k); err == nil {
		t.Fatal("tampered sidecar was accepted — GCM authentication not enforced")
	}
}

// PROOF 4: a wrapped-key sidecar is bound to its exact objectKey (AAD), so it
// cannot be relocated onto another tenant's slot.
func TestKeyring_SidecarNotPortableAcrossTenants(t *testing.T) {
	fs := localFS(t)
	root := newMemRoot()
	kr, err := store.NewKeyring(root, fs)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	ka := store.Key{OrgID: "acme", UserID: "alice", Scope: store.ScopeUser}
	kb := store.Key{OrgID: "acme", UserID: "bob", Scope: store.ScopeUser}
	if _, err := kr.Resolve(ctx, ka); err != nil {
		t.Fatal(err)
	}

	// Copy alice's wrapped blob onto bob's sidecar path.
	blob := sidecarBytes(t, fs, ka.ObjectKey()+sidecarSuffix)
	if err := fs.Upload(blob, kb.ObjectKey()+sidecarSuffix); err != nil {
		t.Fatal(err)
	}

	kr2, err := store.NewKeyring(root, fs)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := kr2.Resolve(ctx, kb); err == nil {
		t.Fatal("relocated wrapped key accepted for bob — AAD binding broken")
	}
}

// PROOF 5: a different org KEK cannot unwrap the sidecar (KEK dependence).
func TestKeyring_WrongKEKCannotUnwrap(t *testing.T) {
	fs := localFS(t)
	ctx := context.Background()
	k := store.Key{OrgID: "acme", UserID: "alice", Scope: store.ScopeUser}

	kr, err := store.NewKeyring(newMemRoot(), fs)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := kr.Resolve(ctx, k); err != nil {
		t.Fatal(err)
	}

	// A different RootSource yields a different KEK for the same org.
	krWrong, err := store.NewKeyring(newMemRoot(), fs)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := krWrong.Resolve(ctx, k); err == nil {
		t.Fatal("a different org KEK unwrapped the sidecar — KEK not enforced")
	}
}

// PROOF 6: key material persists — a fresh keyring reloads the SAME identity.
func TestKeyring_PersistsAndReloads(t *testing.T) {
	fs := localFS(t)
	root := newMemRoot()
	ctx := context.Background()
	k := store.Key{OrgID: "acme", UserID: "alice", Scope: store.ScopeUser}

	kr1, err := store.NewKeyring(root, fs)
	if err != nil {
		t.Fatal(err)
	}
	t1, err := kr1.Resolve(ctx, k)
	if err != nil {
		t.Fatal(err)
	}
	ct := ageSeal(t, t1, []byte("hello across restarts"))

	kr2, err := store.NewKeyring(root, fs)
	if err != nil {
		t.Fatal(err)
	}
	t2, err := kr2.Resolve(ctx, k)
	if err != nil {
		t.Fatal(err)
	}
	pt, err := ageOpen(t2, ct)
	if err != nil || string(pt) != "hello across restarts" {
		t.Fatalf("reloaded identity failed to decrypt: %v", err)
	}
}

// PROOF 7: KEK rotation re-wraps the sidecar WITHOUT rewriting the DB — the
// underlying identity is unchanged, the old KEK is retired.
func TestKeyring_RotateKEK(t *testing.T) {
	fs := localFS(t)
	oldRoot := newMemRoot()
	newRoot := newMemRoot()
	ctx := context.Background()
	k := store.Key{OrgID: "acme", UserID: "alice", Scope: store.ScopeUser}

	kr, err := store.NewKeyring(oldRoot, fs)
	if err != nil {
		t.Fatal(err)
	}
	tk, err := kr.Resolve(ctx, k)
	if err != nil {
		t.Fatal(err)
	}
	// Simulate the on-disk DB ciphertext (encrypted to the tenant identity).
	dbCiphertext := ageSeal(t, tk, []byte("stable DB bytes — never rewritten"))
	sidecarBefore := sidecarBytes(t, fs, k.ObjectKey()+sidecarSuffix)

	if err := kr.Rotate(ctx, k, oldRoot, newRoot); err != nil {
		t.Fatalf("rotate: %v", err)
	}

	// Sidecar changed (re-wrapped) ...
	sidecarAfter := sidecarBytes(t, fs, k.ObjectKey()+sidecarSuffix)
	if bytes.Equal(sidecarBefore, sidecarAfter) {
		t.Fatal("sidecar unchanged after rotation")
	}

	// ... but the DB ciphertext is STILL decryptable via the new KEK path,
	// because the identity itself never changed.
	krNew, err := store.NewKeyring(newRoot, fs)
	if err != nil {
		t.Fatal(err)
	}
	tkNew, err := krNew.Resolve(ctx, k)
	if err != nil {
		t.Fatalf("resolve after rotation: %v", err)
	}
	pt, err := ageOpen(tkNew, dbCiphertext)
	if err != nil || string(pt) != "stable DB bytes — never rewritten" {
		t.Fatalf("post-rotation decrypt of UNCHANGED DB ciphertext failed: %v", err)
	}

	// The OLD KEK can no longer unwrap the rotated sidecar.
	krOld, err := store.NewKeyring(oldRoot, fs)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := krOld.Resolve(ctx, k); err == nil {
		t.Fatal("old KEK still unwraps sidecar after rotation")
	}
}

// PROOF 8: plaintext never survives into the at-rest ciphertext.
func TestKeyring_CiphertextHidesPlaintext(t *testing.T) {
	fs := localFS(t)
	kr, err := store.NewKeyring(newMemRoot(), fs)
	if err != nil {
		t.Fatal(err)
	}
	tk, err := kr.Resolve(context.Background(), store.Key{OrgID: "acme", UserID: "alice", Scope: store.ScopeUser})
	if err != nil {
		t.Fatal(err)
	}
	secret := []byte("TOP-SECRET-ROW-VALUE-marker-7c1f")
	ct := ageSeal(t, tk, secret)
	if bytes.Contains(ct, secret) {
		t.Fatal("plaintext leaked into age ciphertext")
	}
}

// PROOF 9: the store refuses to construct without a KeyProvider (fail secure —
// no plaintext-at-rest path exists).
func TestNew_RequiresKeys(t *testing.T) {
	fs := localFS(t)
	if _, err := store.New(store.Options{ObjectStore: fs, CacheRoot: t.TempDir()}); err == nil {
		t.Fatal("store.New must require Keys (per-tenant encryption is mandatory)")
	}
}

// PROOF 10: end-to-end — the actual object written to durable storage is age
// ciphertext, not plaintext SQLite, and contains no row values in the clear.
func TestStore_ObjectAtRestIsCiphertext(t *testing.T) {
	s, bucketDir := newTestStore(t)
	ctx := ctxWithClaims("acme", "alice")

	db, err := s.ForCtx(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.NewQuery("CREATE TABLE t(v TEXT)").Execute(); err != nil {
		t.Fatal(err)
	}
	if _, err := db.NewQuery("INSERT INTO t VALUES ('MARKER-PLAINTEXT-9f3a')").Execute(); err != nil {
		t.Fatal(err)
	}
	k := store.Key{OrgID: "acme", UserID: "alice", Scope: store.ScopeUser}
	if err := s.Checkpoint(ctx, k); err != nil {
		t.Fatal(err)
	}

	raw, err := os.ReadFile(filepath.Join(bucketDir, "acme", "users", "alice.db"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.HasPrefix(raw, []byte("SQLite format 3")) {
		t.Fatal("object at rest is plaintext SQLite — encryption not applied")
	}
	if bytes.Contains(raw, []byte("MARKER-PLAINTEXT-9f3a")) {
		t.Fatal("row value present in plaintext in the at-rest object")
	}
	// Confirm it IS a valid age file (starts with the age header).
	if !bytes.HasPrefix(raw, []byte("age-encryption.org/v1")) {
		t.Fatalf("at-rest object is neither SQLite nor age; first bytes: %q", raw[:min(24, len(raw))])
	}
}
