package org

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

	got, err := db.ProvisionOrg("acme")
	if err != nil {
		t.Fatal(err)
	}

	expected := filepath.Join(dir, "orgs", "acme")
	if got != expected {
		t.Fatalf("expected %s, got %s", expected, got)
	}

	// Directory should exist
	if _, err := os.Stat(expected); os.IsNotExist(err) {
		t.Fatal("org directory not created")
	}

	// Users subdirectory should exist
	if _, err := os.Stat(filepath.Join(expected, "users")); os.IsNotExist(err) {
		t.Fatal("users subdirectory not created")
	}

	// And no Base yet. Provisioning makes the place; opening makes the Base,
	// and until it does the honest answer to "is there one" is no.
	if _, ok := db.GetOrgDBPath("acme"); ok {
		t.Fatal("a directory was reported as a Base")
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

	// Each has a place of its own, and none of them has a Base until one is
	// opened there.
	for _, org := range orgs {
		if _, err := os.Stat(db.OrgDir(org)); err != nil {
			t.Fatalf("org %s has no directory: %v", org, err)
		}
		if _, ok := db.GetOrgDBPath(org); ok {
			t.Fatalf("org %s reported a Base it does not have", org)
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

// mustOrgDEK and mustUserDEK keep the assertions below about the keys rather
// than about error plumbing.
func mustOrgDEK(t *testing.T, db *OrgDB, org string) string {
	t.Helper()
	dek, err := db.OrgDEK(org)
	if err != nil {
		t.Fatalf("OrgDEK(%q): %v", org, err)
	}
	return dek
}

func mustUserDEK(t *testing.T, db *OrgDB, org, user string) string {
	t.Helper()
	dek, err := db.UserDEK(org, user)
	if err != nil {
		t.Fatalf("UserDEK(%q, %q): %v", org, user, err)
	}
	return dek
}

func TestOrgDB_OrgDEK_Deterministic(t *testing.T) {
	db, _ := testOrgDB(t)

	dek1 := mustOrgDEK(t, db, "acme")
	dek2 := mustOrgDEK(t, db, "acme")

	if dek1 != dek2 {
		t.Fatal("OrgDEK should be deterministic for same input")
	}
	if len(dek1) != 64 { // 32 bytes = 64 hex chars
		t.Fatalf("expected 64 hex chars, got %d", len(dek1))
	}
}

func TestOrgDB_OrgDEK_UniquePerOrg(t *testing.T) {
	db, _ := testOrgDB(t)

	dek1 := mustOrgDEK(t, db, "acme")
	dek2 := mustOrgDEK(t, db, "globex")

	if dek1 == dek2 {
		t.Fatal("different orgs should have different DEKs")
	}
}

func TestOrgDB_UserDEK_UniquePerUser(t *testing.T) {
	db, _ := testOrgDB(t)

	dek1 := mustUserDEK(t, db, "acme", "user-001")
	dek2 := mustUserDEK(t, db, "acme", "user-002")
	dek3 := mustUserDEK(t, db, "globex", "user-001")

	if dek1 == dek2 {
		t.Fatal("different users in same org should have different DEKs")
	}
	if dek1 == dek3 {
		t.Fatal("same user ID in different orgs should have different DEKs")
	}
}

func TestOrgDB_DEK_OrgVsUser_Different(t *testing.T) {
	db, _ := testOrgDB(t)

	orgDEK := mustOrgDEK(t, db, "acme")
	userDEK := mustUserDEK(t, db, "acme", "acme") // same string as org slug

	if orgDEK == userDEK {
		t.Fatal("org DEK and user DEK should differ even with the same input (different namespace and subsystem)")
	}
}

func TestOrgDB_DEK_EmptyMasterKey(t *testing.T) {
	dir := t.TempDir()
	app := &mockApp{dataDir: dir}
	db := NewOrgDB(app, "") // no master key

	if dek := mustOrgDEK(t, db, "acme"); dek != "" {
		t.Fatalf("expected empty DEK with no master key, got %s", dek)
	}
	if dek := mustUserDEK(t, db, "acme", "user-001"); dek != "" {
		t.Fatalf("expected empty DEK with no master key, got %s", dek)
	}
}

// A master key of the wrong length must be an error, not silently no key. This
// is the fail-open case: a deployment that sets a 31-byte key would otherwise
// write plaintext while believing it had encryption.
func TestOrgDB_DEK_WrongLengthMasterKey(t *testing.T) {
	app := &mockApp{dataDir: t.TempDir()}
	db := NewOrgDB(app, "too-short")

	if _, err := db.OrgDEK("acme"); err == nil {
		t.Fatal("expected an error from a master key that is not 32 bytes")
	}
	if _, err := db.UserDEK("acme", "user-001"); err == nil {
		t.Fatal("expected an error from a master key that is not 32 bytes")
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

	if _, err := db.ProvisionOrg("acme"); err != nil {
		t.Fatal(err)
	}

	// The lookup reports the file, so it stays false until the file is there.
	if _, ok := db.GetOrgDBPath("acme"); ok {
		t.Fatal("reported a Base before one was opened")
	}

	expected := db.OrgDBPath("acme")
	if err := os.WriteFile(expected, []byte("a base"), 0600); err != nil {
		t.Fatal(err)
	}

	got, ok := db.GetOrgDBPath("acme")
	if !ok {
		t.Fatal("should find the org's Base")
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
