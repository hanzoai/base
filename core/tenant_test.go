package core

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNewTenantRegistry_NilConfig(t *testing.T) {
	r := NewTenantRegistry(nil)
	if r != nil {
		t.Fatal("expected nil registry for nil config")
	}
}

func TestTenantRegistry_OrgDB_LazyCreate(t *testing.T) {
	dir := t.TempDir()
	r := NewTenantRegistry(&TenantConfig{
		DataDir:   dir,
		DBConnect: DefaultDBConnect,
	})
	defer r.Close()

	db, err := r.OrgDB("acme")
	if err != nil {
		t.Fatal(err)
	}
	if db == nil {
		t.Fatal("expected non-nil db")
	}

	// Force a write so SQLite creates the file on disk
	_, err = db.NewQuery("CREATE TABLE IF NOT EXISTS _probe (id INTEGER)").Execute()
	if err != nil {
		t.Fatal(err)
	}

	// Directory should exist
	orgDir := filepath.Join(dir, "orgs", "acme")
	if _, err := os.Stat(orgDir); os.IsNotExist(err) {
		t.Fatal("org directory not created")
	}

	// DB file should exist (SQLite creates on first write)
	dbFile := filepath.Join(orgDir, "data.db")
	if _, err := os.Stat(dbFile); os.IsNotExist(err) {
		t.Fatal("data.db not created")
	}
}

func TestTenantRegistry_OrgDB_CachesConnection(t *testing.T) {
	dir := t.TempDir()
	r := NewTenantRegistry(&TenantConfig{
		DataDir:   dir,
		DBConnect: DefaultDBConnect,
	})
	defer r.Close()

	db1, _ := r.OrgDB("acme")
	db2, _ := r.OrgDB("acme")

	// Same pointer (cached)
	if db1 != db2 {
		t.Fatal("second call should return cached db")
	}
}

func TestTenantRegistry_OrgDB_Isolation(t *testing.T) {
	dir := t.TempDir()
	r := NewTenantRegistry(&TenantConfig{
		DataDir:   dir,
		DBConnect: DefaultDBConnect,
	})
	defer r.Close()

	acme, _ := r.OrgDB("acme")
	globex, _ := r.OrgDB("globex")

	// Create table in acme
	_, err := acme.NewQuery("CREATE TABLE settings (k TEXT PRIMARY KEY, v TEXT)").Execute()
	if err != nil {
		t.Fatal(err)
	}
	_, err = acme.NewQuery("INSERT INTO settings (k, v) VALUES ('name', 'Acme Corp')").Execute()
	if err != nil {
		t.Fatal(err)
	}

	// Create same table in globex
	_, err = globex.NewQuery("CREATE TABLE settings (k TEXT PRIMARY KEY, v TEXT)").Execute()
	if err != nil {
		t.Fatal(err)
	}
	_, err = globex.NewQuery("INSERT INTO settings (k, v) VALUES ('name', 'Globex Inc')").Execute()
	if err != nil {
		t.Fatal(err)
	}

	// Read back -- data should be isolated
	var acmeName string
	if err := acme.NewQuery("SELECT v FROM settings WHERE k = 'name'").Row(&acmeName); err != nil {
		t.Fatal(err)
	}
	if acmeName != "Acme Corp" {
		t.Fatalf("expected 'Acme Corp', got %q", acmeName)
	}

	var globexName string
	if err := globex.NewQuery("SELECT v FROM settings WHERE k = 'name'").Row(&globexName); err != nil {
		t.Fatal(err)
	}
	if globexName != "Globex Inc" {
		t.Fatalf("expected 'Globex Inc', got %q", globexName)
	}
}

func TestTenantRegistry_OrgNonconcurrentDB(t *testing.T) {
	dir := t.TempDir()
	r := NewTenantRegistry(&TenantConfig{
		DataDir:   dir,
		DBConnect: DefaultDBConnect,
	})
	defer r.Close()

	ncdb, err := r.OrgNonconcurrentDB("acme")
	if err != nil {
		t.Fatal(err)
	}
	if ncdb == nil {
		t.Fatal("expected non-nil nonconcurrent db")
	}
}

func TestTenantRegistry_InvalidOrgID(t *testing.T) {
	dir := t.TempDir()
	r := NewTenantRegistry(&TenantConfig{
		DataDir:   dir,
		DBConnect: DefaultDBConnect,
	})
	defer r.Close()

	cases := []string{"", "..", "../etc", "UPPER", "has space", "special!"}
	for _, id := range cases {
		if _, err := r.OrgDB(id); err == nil {
			t.Fatalf("expected error for org ID %q", id)
		}
	}
}

func TestTenantRegistry_ValidOrgIDs(t *testing.T) {
	dir := t.TempDir()
	r := NewTenantRegistry(&TenantConfig{
		DataDir:   dir,
		DBConnect: DefaultDBConnect,
	})
	defer r.Close()

	cases := []string{"acme", "org-123", "my_org", "a", "ab", "a-b-c-d"}
	for _, id := range cases {
		if _, err := r.OrgDB(id); err != nil {
			t.Fatalf("unexpected error for org ID %q: %v", id, err)
		}
	}
}

func TestTenantRegistry_HasOrg(t *testing.T) {
	dir := t.TempDir()
	r := NewTenantRegistry(&TenantConfig{
		DataDir:   dir,
		DBConnect: DefaultDBConnect,
	})
	defer r.Close()

	if r.HasOrg("acme") {
		t.Fatal("should not have org before opening")
	}

	db, _ := r.OrgDB("acme") // triggers lazy creation

	// Force a write so SQLite creates the file on disk
	db.NewQuery("CREATE TABLE IF NOT EXISTS _probe (id INTEGER)").Execute()

	if !r.HasOrg("acme") {
		t.Fatal("should have org after opening and writing")
	}
}

func TestTenantRegistry_MasterKey(t *testing.T) {
	masterKey := make([]byte, 32)
	for i := range masterKey {
		masterKey[i] = byte(i)
	}

	r := NewTenantRegistry(&TenantConfig{
		DataDir:   t.TempDir(),
		MasterKey: masterKey,
		DBConnect: DefaultDBConnect,
	})
	defer r.Close()

	got := r.MasterKey()
	if len(got) != 32 {
		t.Fatalf("expected 32-byte master key, got %d", len(got))
	}
	for i := range got {
		if got[i] != byte(i) {
			t.Fatal("master key mismatch")
		}
	}
}

func TestTenantRegistry_Close(t *testing.T) {
	dir := t.TempDir()
	r := NewTenantRegistry(&TenantConfig{
		DataDir:   dir,
		DBConnect: DefaultDBConnect,
	})

	// Open a couple orgs
	r.OrgDB("acme")
	r.OrgDB("globex")

	if err := r.Close(); err != nil {
		t.Fatal(err)
	}

	// After close, new opens should work (fresh state)
	// The registry itself still exists but dbs map is empty
	if len(r.dbs) != 0 {
		t.Fatal("dbs map should be empty after close")
	}
}

func TestBaseApp_OrgDB_Disabled(t *testing.T) {
	app := NewBaseApp(BaseAppConfig{
		DataDir: t.TempDir(),
	})

	db, err := app.OrgDB("acme")
	if err != nil {
		t.Fatal(err)
	}
	if db != nil {
		t.Fatal("expected nil db when multi-tenancy disabled")
	}

	if app.Tenants() != nil {
		t.Fatal("expected nil tenants when multi-tenancy disabled")
	}
}

func TestBaseApp_OrgDB_EnabledViaConfig(t *testing.T) {
	dir := t.TempDir()
	app := NewBaseApp(BaseAppConfig{
		DataDir:     dir,
		MultiTenant: true,
	})

	if err := app.Bootstrap(); err != nil {
		t.Fatal(err)
	}
	defer app.ResetBootstrapState()

	if app.Tenants() == nil {
		t.Fatal("expected non-nil tenants when MultiTenant=true")
	}

	db, err := app.OrgDB("acme")
	if err != nil {
		t.Fatal(err)
	}
	if db == nil {
		t.Fatal("expected non-nil db")
	}

	// Verify we can write and read
	_, err = db.NewQuery("CREATE TABLE test (id INTEGER PRIMARY KEY, val TEXT)").Execute()
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.NewQuery("INSERT INTO test (val) VALUES ('hello')").Execute()
	if err != nil {
		t.Fatal(err)
	}
	var val string
	if err := db.NewQuery("SELECT val FROM test").Row(&val); err != nil {
		t.Fatal(err)
	}
	if val != "hello" {
		t.Fatalf("expected 'hello', got %q", val)
	}
}

func TestBaseApp_OrgDB_EnabledViaMasterKey(t *testing.T) {
	dir := t.TempDir()
	masterKey := make([]byte, 32)
	for i := range masterKey {
		masterKey[i] = byte(i + 1)
	}

	app := NewBaseApp(BaseAppConfig{
		DataDir:   dir,
		MasterKey: masterKey,
	})

	if err := app.Bootstrap(); err != nil {
		t.Fatal(err)
	}
	defer app.ResetBootstrapState()

	if app.Tenants() == nil {
		t.Fatal("expected non-nil tenants when MasterKey is set")
	}

	if mk := app.Tenants().MasterKey(); len(mk) != 32 {
		t.Fatalf("expected 32-byte master key on registry, got %d", len(mk))
	}
}

func TestBaseApp_OrgDB_IsolationFromMainDB(t *testing.T) {
	dir := t.TempDir()
	app := NewBaseApp(BaseAppConfig{
		DataDir:     dir,
		MultiTenant: true,
	})

	if err := app.Bootstrap(); err != nil {
		t.Fatal(err)
	}
	defer app.ResetBootstrapState()

	// Write to main DB
	mainDB := app.DB()
	_, err := mainDB.NewQuery("CREATE TABLE main_table (id INTEGER)").Execute()
	if err != nil {
		t.Fatal(err)
	}

	// Write to org DB
	orgDB, _ := app.OrgDB("acme")
	_, err = orgDB.NewQuery("CREATE TABLE org_table (id INTEGER)").Execute()
	if err != nil {
		t.Fatal(err)
	}

	// Org DB should NOT have main_table
	var count int
	err = orgDB.NewQuery("SELECT count(*) FROM sqlite_master WHERE type='table' AND name='main_table'").Row(&count)
	if err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatal("org DB should not have main_table")
	}

	// Main DB should NOT have org_table
	err = mainDB.NewQuery("SELECT count(*) FROM sqlite_master WHERE type='table' AND name='org_table'").Row(&count)
	if err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatal("main DB should not have org_table")
	}
}
