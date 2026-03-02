package platform

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/hanzoai/base/core"
)

// mockApp implements the minimal core.App interface for OrgDB tests.
type mockApp struct {
	core.BaseApp
	dataDir string
}

func (a *mockApp) DataDir() string { return a.dataDir }

func testOrgDB(t *testing.T) (*OrgDB, string) {
	t.Helper()
	dir := t.TempDir()
	app := &mockApp{dataDir: dir}
	return NewOrgDB(app, "test-master-key-32-bytes-long!!!"), dir
}

// --- OrgDB provisioning ---

func TestOrgDB_ProvisionOrg(t *testing.T) {
	db, dir := testOrgDB(t)

	path, err := db.ProvisionOrg("acme")
	if err != nil {
		t.Fatal(err)
	}

	expected := filepath.Join(dir, "orgs", "acme", "org.db")
	if path != expected {
		t.Fatalf("expected %s, got %s", expected, path)
	}

	// Directory should exist
	if _, err := os.Stat(filepath.Join(dir, "orgs", "acme")); os.IsNotExist(err) {
		t.Fatal("org directory not created")
	}

	// Users subdirectory should exist
	if _, err := os.Stat(filepath.Join(dir, "orgs", "acme", "users")); os.IsNotExist(err) {
		t.Fatal("users subdirectory not created")
	}
}

func TestOrgDB_ProvisionUser(t *testing.T) {
	db, dir := testOrgDB(t)

	_, _ = db.ProvisionOrg("acme")
	path, err := db.ProvisionUser("acme", "user-001")
	if err != nil {
		t.Fatal(err)
	}

	expected := filepath.Join(dir, "orgs", "acme", "users", "user-001", "data.db")
	if path != expected {
		t.Fatalf("expected %s, got %s", expected, path)
	}

	if _, err := os.Stat(filepath.Dir(path)); os.IsNotExist(err) {
		t.Fatal("user directory not created")
	}
}

func TestOrgDB_ProvisionMultipleOrgs(t *testing.T) {
	db, _ := testOrgDB(t)

	orgs := []string{"acme", "globex", "initech", "umbrella"}
	for _, org := range orgs {
		if _, err := db.ProvisionOrg(org); err != nil {
			t.Fatalf("provision org %s: %v", org, err)
		}
	}

	// All should be lookupable
	for _, org := range orgs {
		if _, ok := db.GetOrgDBPath(org); !ok {
			t.Fatalf("org %s not found after provisioning", org)
		}
	}
}

// --- Slug validation ---

func TestOrgDB_InvalidSlug(t *testing.T) {
	db, _ := testOrgDB(t)

	cases := []string{"", "..", "../etc", "org/../../etc", "UPPER", "has space", "special!"}
	for _, slug := range cases {
		if _, err := db.ProvisionOrg(slug); err == nil {
			t.Fatalf("expected error for slug %q", slug)
		}
	}
}

func TestOrgDB_ValidSlugs(t *testing.T) {
	db, _ := testOrgDB(t)

	cases := []string{"acme", "org-123", "my_org", "a", "ab", "a-b-c-d"}
	for _, slug := range cases {
		if _, err := db.ProvisionOrg(slug); err != nil {
			t.Fatalf("unexpected error for slug %q: %v", slug, err)
		}
	}
}

// --- DEK derivation ---

func TestOrgDB_OrgDEK_Deterministic(t *testing.T) {
	db, _ := testOrgDB(t)

	dek1 := db.OrgDEK("acme")
	dek2 := db.OrgDEK("acme")

	if dek1 != dek2 {
		t.Fatal("OrgDEK should be deterministic for same input")
	}
	if len(dek1) != 64 { // 32 bytes = 64 hex chars
		t.Fatalf("expected 64 hex chars, got %d", len(dek1))
	}
}

func TestOrgDB_OrgDEK_UniquePerOrg(t *testing.T) {
	db, _ := testOrgDB(t)

	dek1 := db.OrgDEK("acme")
	dek2 := db.OrgDEK("globex")

	if dek1 == dek2 {
		t.Fatal("different orgs should have different DEKs")
	}
}

func TestOrgDB_UserDEK_UniquePerUser(t *testing.T) {
	db, _ := testOrgDB(t)

	dek1 := db.UserDEK("acme", "user-001")
	dek2 := db.UserDEK("acme", "user-002")
	dek3 := db.UserDEK("globex", "user-001")

	if dek1 == dek2 {
		t.Fatal("different users in same org should have different DEKs")
	}
	if dek1 == dek3 {
		t.Fatal("same user ID in different orgs should have different DEKs")
	}
}

func TestOrgDB_DEK_OrgVsUser_Different(t *testing.T) {
	db, _ := testOrgDB(t)

	orgDEK := db.OrgDEK("acme")
	userDEK := db.UserDEK("acme", "acme") // same string as org slug

	if orgDEK == userDEK {
		t.Fatal("org DEK and user DEK should differ even with same input (different HKDF info)")
	}
}

func TestOrgDB_DEK_EmptyMasterKey(t *testing.T) {
	dir := t.TempDir()
	app := &mockApp{dataDir: dir}
	db := NewOrgDB(app, "") // no master key

	if dek := db.OrgDEK("acme"); dek != "" {
		t.Fatalf("expected empty DEK with no master key, got %s", dek)
	}
	if dek := db.UserDEK("acme", "user-001"); dek != "" {
		t.Fatalf("expected empty DEK with no master key, got %s", dek)
	}
}

// --- Lookup ---

func TestOrgDB_GetOrgDBPath_NotProvisioned(t *testing.T) {
	db, _ := testOrgDB(t)

	if _, ok := db.GetOrgDBPath("nonexistent"); ok {
		t.Fatal("should not find non-provisioned org")
	}
}

func TestOrgDB_GetUserDBPath_NotProvisioned(t *testing.T) {
	db, _ := testOrgDB(t)

	if _, ok := db.GetUserDBPath("acme", "user-001"); ok {
		t.Fatal("should not find non-provisioned user")
	}
}

func TestOrgDB_GetOrgDBPath_AfterProvision(t *testing.T) {
	db, _ := testOrgDB(t)

	expected, _ := db.ProvisionOrg("acme")
	got, ok := db.GetOrgDBPath("acme")
	if !ok {
		t.Fatal("should find provisioned org")
	}
	if got != expected {
		t.Fatalf("expected %s, got %s", expected, got)
	}
}

// --- Delete ---

func TestOrgDB_DeleteUser(t *testing.T) {
	db, _ := testOrgDB(t)

	db.ProvisionOrg("acme")
	path, _ := db.ProvisionUser("acme", "user-001")

	// Create the actual DB file so the directory has content
	os.WriteFile(path, []byte("test"), 0600)

	if err := db.DeleteUser("acme", "user-001"); err != nil {
		t.Fatal(err)
	}

	if _, ok := db.GetUserDBPath("acme", "user-001"); ok {
		t.Fatal("user should be deleted from lookup")
	}

	if _, err := os.Stat(filepath.Dir(path)); !os.IsNotExist(err) {
		t.Fatal("user directory should be removed")
	}
}

// --- Integration: OrgDB + PoolManager ---

func TestOrgDB_WithPoolManager(t *testing.T) {
	db, _ := testOrgDB(t)
	pm := NewDBPoolManager(DBPoolConfig{MaxPools: 16, NumShards: 1})
	defer pm.Close()

	// Provision org
	orgPath, _ := db.ProvisionOrg("acme")

	// Open a pool for it
	pool, err := pm.Get(orgPath)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Release()

	// Create table and insert data
	_, err = pool.Nonconcurrent.NewQuery("CREATE TABLE config (k TEXT PRIMARY KEY, v TEXT)").Execute()
	if err != nil {
		t.Fatalf("create table: %v", err)
	}
	_, err = pool.Nonconcurrent.NewQuery("INSERT INTO config (k, v) VALUES ('name', 'Acme Corp')").Execute()
	if err != nil {
		t.Fatalf("insert: %v", err)
	}

	// Read back via concurrent pool
	var name string
	if err := pool.Concurrent.NewQuery("SELECT v FROM config WHERE k = 'name'").Row(&name); err != nil {
		t.Fatalf("select: %v", err)
	}
	if name != "Acme Corp" {
		t.Fatalf("expected 'Acme Corp', got %q", name)
	}

	// Provision user in same org
	userPath, _ := db.ProvisionUser("acme", "user-42")
	userPool, err := pm.Get(userPath)
	if err != nil {
		t.Fatal(err)
	}
	defer userPool.Release()

	// User DB is independent from org DB
	_, err = userPool.Nonconcurrent.NewQuery("CREATE TABLE orders (id INTEGER PRIMARY KEY, symbol TEXT)").Execute()
	if err != nil {
		t.Fatalf("create user table: %v", err)
	}
	_, err = userPool.Nonconcurrent.NewQuery("INSERT INTO orders (symbol) VALUES ('AAPL')").Execute()
	if err != nil {
		t.Fatalf("insert user data: %v", err)
	}

	// Org DB should NOT have the orders table
	var count int
	err = pool.Concurrent.NewQuery("SELECT count(*) FROM sqlite_master WHERE name='orders'").Row(&count)
	if err != nil {
		t.Fatalf("check org db: %v", err)
	}
	if count != 0 {
		t.Fatal("org DB should not have user's orders table — data isolation broken")
	}

	// Stats
	stats := pm.Stats()
	if stats.Opens != 2 {
		t.Fatalf("expected 2 opens (org + user), got %d", stats.Opens)
	}
}

// --- Multi-org isolation ---

func TestOrgDB_MultiOrgIsolation(t *testing.T) {
	db, _ := testOrgDB(t)
	pm := NewDBPoolManager(DBPoolConfig{MaxPools: 16, NumShards: 1})
	defer pm.Close()

	// Two orgs
	path1, _ := db.ProvisionOrg("acme")
	path2, _ := db.ProvisionOrg("globex")

	p1, _ := pm.Get(path1)
	defer p1.Release()
	p2, _ := pm.Get(path2)
	defer p2.Release()

	// Each org creates the same table name
	p1.Nonconcurrent.NewQuery("CREATE TABLE accounts (id INTEGER, name TEXT)").Execute()
	p2.Nonconcurrent.NewQuery("CREATE TABLE accounts (id INTEGER, name TEXT)").Execute()

	p1.Nonconcurrent.NewQuery("INSERT INTO accounts (name) VALUES ('Alice')").Execute()
	p2.Nonconcurrent.NewQuery("INSERT INTO accounts (name) VALUES ('Bob')").Execute()

	// Data is isolated
	var name1, name2 string
	p1.Concurrent.NewQuery("SELECT name FROM accounts").Row(&name1)
	p2.Concurrent.NewQuery("SELECT name FROM accounts").Row(&name2)

	if name1 != "Alice" {
		t.Fatalf("acme should have Alice, got %q", name1)
	}
	if name2 != "Bob" {
		t.Fatalf("globex should have Bob, got %q", name2)
	}
}
