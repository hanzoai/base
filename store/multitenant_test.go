package store_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/hanzoai/base/store"
	"github.com/hanzoai/base/tools/claims"
	"github.com/hanzoai/base/tools/filesystem"
)

// newTestStore constructs a store backed by a fileblob bucket in a temp
// directory. Returns the store and the path to the bucket so tests can peek
// at object keys directly.
func newTestStore(t *testing.T, opts ...func(*store.Options)) (*store.MultiTenantStore, string) {
	t.Helper()
	bucketDir := filepath.Join(t.TempDir(), "bucket")
	cacheDir := filepath.Join(t.TempDir(), "cache")

	fs, err := filesystem.NewLocal(bucketDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })

	o := store.Options{
		ObjectStore:        fs,
		CacheRoot:          cacheDir,
		LRUSize:            4, // tiny on purpose to exercise eviction
		IdleTTL:            50 * time.Millisecond,
		CheckpointWrites:   2,
		CheckpointInterval: 10 * time.Millisecond,
	}
	for _, fn := range opts {
		fn(&o)
	}
	s, err := store.New(o)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = s.Close(context.Background())
	})
	return s, bucketDir
}

// ctxWithClaims builds a request context like claims.Inject would.
func ctxWithClaims(orgID, userID string, roles ...string) context.Context {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set(claims.HeaderOrgID, orgID)
	r.Header.Set(claims.HeaderUserID, userID)
	if len(roles) > 0 {
		r.Header.Set(claims.HeaderRoles, joinRoles(roles))
	}

	var ctx context.Context
	claims.Inject(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx = r.Context()
	})).ServeHTTP(httptest.NewRecorder(), r)
	return ctx
}

func joinRoles(roles []string) string {
	out := ""
	for i, r := range roles {
		if i > 0 {
			out += ","
		}
		out += r
	}
	return out
}

func TestKey_ObjectKey(t *testing.T) {
	cases := []struct {
		k    store.Key
		want string
	}{
		{store.Key{OrgID: "acme", UserID: "alice", Scope: store.ScopeUser}, "acme/users/alice.db"},
		{store.Key{OrgID: "acme", Scope: store.ScopeOrg}, "acme/org.db"},
	}
	for _, tc := range cases {
		if got := tc.k.ObjectKey(); got != tc.want {
			t.Errorf("ObjectKey(%v) = %q, want %q", tc.k, got, tc.want)
		}
	}
}

func TestKey_Valid_RejectsTraversal(t *testing.T) {
	bad := []store.Key{
		{OrgID: "../etc", UserID: "x", Scope: store.ScopeUser},
		{OrgID: "acme", UserID: "../passwd", Scope: store.ScopeUser},
		{OrgID: "UPPER", UserID: "x", Scope: store.ScopeUser},
		{OrgID: "", UserID: "x", Scope: store.ScopeUser},
		{OrgID: "acme", UserID: "", Scope: store.ScopeUser},
		{OrgID: "acme has space", UserID: "x", Scope: store.ScopeUser},
	}
	for _, k := range bad {
		if err := k.Valid(); err == nil {
			t.Errorf("expected Valid()=err for %+v", k)
		}
	}
}

func TestKey_Valid_AcceptsSlugs(t *testing.T) {
	good := []store.Key{
		{OrgID: "acme", UserID: "alice", Scope: store.ScopeUser},
		{OrgID: "org-123", UserID: "user_42", Scope: store.ScopeUser},
		{OrgID: "a", UserID: "b", Scope: store.ScopeUser},
		{OrgID: "acme", Scope: store.ScopeOrg},
	}
	for _, k := range good {
		if err := k.Valid(); err != nil {
			t.Errorf("Valid(%+v) = %v, want nil", k, err)
		}
	}
}

// TestHydrate_MissCreatesLocalAndOpens verifies that a first Get for a
// brand-new tenant creates the local file and opens a working SQLite.
func TestHydrate_MissCreatesLocalAndOpens(t *testing.T) {
	s, _ := newTestStore(t)
	ctx := ctxWithClaims("acme", "alice")

	db, err := s.ForCtx(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.NewQuery("CREATE TABLE t(id INTEGER PRIMARY KEY, v TEXT)").Execute(); err != nil {
		t.Fatal(err)
	}
	if _, err := db.NewQuery("INSERT INTO t(v) VALUES ('hello')").Execute(); err != nil {
		t.Fatal(err)
	}
	var v string
	if err := db.NewQuery("SELECT v FROM t LIMIT 1").Row(&v); err != nil {
		t.Fatal(err)
	}
	if v != "hello" {
		t.Fatalf("got %q", v)
	}
}

// TestCacheHit verifies that the second Get returns the same *dbx.DB pointer
// as the first (cache hit, no hydration).
func TestCacheHit(t *testing.T) {
	s, _ := newTestStore(t)
	ctx := ctxWithClaims("acme", "alice")

	db1, err := s.ForCtx(ctx)
	if err != nil {
		t.Fatal(err)
	}
	db2, err := s.ForCtx(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if db1 != db2 {
		t.Fatal("expected cache hit to return same DB pointer")
	}
}

// TestIsolationBetweenTenants verifies that two (org, user) pairs see
// independent SQLite databases.
func TestIsolationBetweenTenants(t *testing.T) {
	s, _ := newTestStore(t)

	alice := ctxWithClaims("acme", "alice")
	bob := ctxWithClaims("acme", "bob")
	carol := ctxWithClaims("globex", "carol")

	dbAlice, err := s.ForCtx(alice)
	if err != nil {
		t.Fatal(err)
	}
	dbBob, err := s.ForCtx(bob)
	if err != nil {
		t.Fatal(err)
	}
	dbCarol, err := s.ForCtx(carol)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := dbAlice.NewQuery(`CREATE TABLE t(id INTEGER, v TEXT)`).Execute(); err != nil {
		t.Fatal(err)
	}
	if _, err := dbBob.NewQuery(`CREATE TABLE t(id INTEGER, v TEXT)`).Execute(); err != nil {
		t.Fatal(err)
	}
	if _, err := dbCarol.NewQuery(`CREATE TABLE t(id INTEGER, v TEXT)`).Execute(); err != nil {
		t.Fatal(err)
	}
	if _, err := dbAlice.NewQuery(`INSERT INTO t VALUES (1, 'from-alice')`).Execute(); err != nil {
		t.Fatal(err)
	}
	if _, err := dbBob.NewQuery(`INSERT INTO t VALUES (1, 'from-bob')`).Execute(); err != nil {
		t.Fatal(err)
	}

	var count int
	if err := dbCarol.NewQuery(`SELECT count(*) FROM t`).Row(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("carol leaked data: count=%d", count)
	}

	var v string
	if err := dbAlice.NewQuery(`SELECT v FROM t WHERE id=1`).Row(&v); err != nil {
		t.Fatal(err)
	}
	if v != "from-alice" {
		t.Fatalf("alice saw %q, expected from-alice", v)
	}
}


// TestForCtx_GatewayBypass returns claims.ErrGatewayBypass when the context
// has no claims attached (identity stripped or never injected).
func TestForCtx_GatewayBypass(t *testing.T) {
	s, _ := newTestStore(t)
	// Bare context, no Inject.
	_, err := s.ForCtx(context.Background())
	if !errors.Is(err, claims.ErrGatewayBypass) {
		t.Fatalf("expected ErrGatewayBypass, got %v", err)
	}
}

// TestCheckpoint_WritesToObjectStorage ensures that explicit Checkpoint
// uploads the DB into the backing bucket.
func TestCheckpoint_WritesToObjectStorage(t *testing.T) {
	s, bucketDir := newTestStore(t)
	ctx := ctxWithClaims("acme", "alice")

	db, err := s.ForCtx(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.NewQuery("CREATE TABLE t(v TEXT)").Execute(); err != nil {
		t.Fatal(err)
	}
	if _, err := db.NewQuery("INSERT INTO t VALUES ('persist-me')").Execute(); err != nil {
		t.Fatal(err)
	}

	k := store.Key{OrgID: "acme", UserID: "alice", Scope: store.ScopeUser}
	if err := s.Checkpoint(ctx, k); err != nil {
		t.Fatalf("Checkpoint: %v", err)
	}

	// Object should exist in the bucket on disk.
	objPath := filepath.Join(bucketDir, "acme", "users", "alice.db")
	if !fileExists(t, objPath) {
		t.Fatalf("expected object at %s", objPath)
	}
}

// TestEvictionPersistsAndRehydrates: fill the LRU past its cap, confirm the
// coldest is evicted (and its state is recoverable via re-hydrate from the
// bucket).
func TestEvictionPersistsAndRehydrates(t *testing.T) {
	s, _ := newTestStore(t)

	// LRUSize = 4. Open 5 distinct tenants; the first should be evicted.
	tenants := []struct{ org, user string }{
		{"o1", "u1"}, {"o2", "u2"}, {"o3", "u3"}, {"o4", "u4"}, {"o5", "u5"},
	}
	for _, tn := range tenants {
		ctx := ctxWithClaims(tn.org, tn.user)
		db, err := s.ForCtx(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.NewQuery("CREATE TABLE t(v TEXT)").Execute(); err != nil {
			t.Fatal(err)
		}
		if _, err := db.NewQuery("INSERT INTO t VALUES ('" + tn.user + "')").Execute(); err != nil {
			t.Fatal(err)
		}
		// Force explicit checkpoint so the bucket has the data before LRU
		// eviction triggers best-effort flush.
		k := store.Key{OrgID: tn.org, UserID: tn.user, Scope: store.ScopeUser}
		if err := s.Checkpoint(ctx, k); err != nil {
			t.Fatalf("checkpoint %v: %v", k, err)
		}
	}

	// Re-hydrate the first (evicted) tenant from the bucket.
	ctx := ctxWithClaims("o1", "u1")
	db, err := s.ForCtx(ctx)
	if err != nil {
		t.Fatalf("rehydrate o1/u1: %v", err)
	}
	var v string
	if err := db.NewQuery("SELECT v FROM t").Row(&v); err != nil {
		t.Fatalf("read after rehydrate: %v", err)
	}
	if v != "u1" {
		t.Fatalf("expected u1, got %q", v)
	}
}

// TestClose_FlushesDirty: confirm that Close snapshots every resident handle
// to the bucket before returning.
func TestClose_FlushesDirty(t *testing.T) {
	s, bucketDir := newTestStore(t)

	for _, u := range []string{"a", "b", "c"} {
		ctx := ctxWithClaims("acme", u)
		db, err := s.ForCtx(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.NewQuery("CREATE TABLE t(v TEXT)").Execute(); err != nil {
			t.Fatal(err)
		}
		if _, err := db.NewQuery("INSERT INTO t VALUES ('" + u + "')").Execute(); err != nil {
			t.Fatal(err)
		}
	}

	if err := s.Close(context.Background()); err != nil {
		t.Fatal(err)
	}

	for _, u := range []string{"a", "b", "c"} {
		obj := filepath.Join(bucketDir, "acme", "users", u+".db")
		if !fileExists(t, obj) {
			t.Errorf("expected flushed object for %s at %s", u, obj)
		}
	}
}

// TestConcurrentReaders_OnSameTenant: multiple goroutines hitting ForCtx on
// the same key hydrate exactly once.
func TestConcurrentReaders_OnSameTenant(t *testing.T) {
	s, _ := newTestStore(t)
	ctx := ctxWithClaims("acme", "alice")

	var wg sync.WaitGroup
	seen := make([]interface{}, 16)
	for i := range seen {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			db, err := s.ForCtx(ctx)
			if err != nil {
				t.Error(err)
				return
			}
			seen[i] = db
		}()
	}
	wg.Wait()
	for i := 1; i < len(seen); i++ {
		if seen[i] != seen[0] {
			t.Fatalf("goroutine %d got a different *dbx.DB", i)
		}
	}
}

// TestOrgScope: ForOrg resolves the org-wide DB (distinct from any user DB).
func TestOrgScope(t *testing.T) {
	s, _ := newTestStore(t)
	ctx := ctxWithClaims("acme", "alice")

	orgDB, err := s.ForOrg(ctx)
	if err != nil {
		t.Fatal(err)
	}
	userDB, err := s.ForCtx(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if orgDB == userDB {
		t.Fatal("ForOrg must not alias ForCtx — they are different DBs")
	}
}

// TestDoubleCloseIdempotent: calling Close twice is safe.
func TestDoubleCloseIdempotent(t *testing.T) {
	s, _ := newTestStore(t)
	if err := s.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
}

// helpers

func fileExists(t *testing.T, path string) bool {
	t.Helper()
	info, err := os.Stat(path)
	return err == nil && info != nil
}
