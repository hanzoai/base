package org

import (
	"archive/zip"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hanzoai/base/apis"
	"github.com/hanzoai/base/core"
	"github.com/hanzoai/base/tests"
)

// A backup carries the Base it is taken on. An org's Base sits in a
// subdirectory of the platform's data directory but is a different Base, with
// its own handles and its own write-ahead log — neither inside the transaction
// this archive is taken in nor checkpointed by it — so it is not what this
// archive holds.
//
// The archive says so itself, because a reader can list what a zip holds and
// cannot see what it does not.
func TestPlatformBackupCarriesItsOwnBase(t *testing.T) {
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatal(err)
	}
	defer app.Cleanup()

	iam := newIssuer(t)
	if err := Register(app, Config{IAMEndpoint: iam.url}); err != nil {
		t.Fatal(err)
	}

	db := NewOrgDB(app, "")
	if _, err := db.ProvisionOrg("acme"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(db.OrgDBPath("acme"), []byte("acme's Base"), 0600); err != nil {
		t.Fatal(err)
	}

	if err := app.CreateBackup(context.Background(), "test.zip"); err != nil {
		t.Fatal(err)
	}

	zr, err := zip.OpenReader(filepath.Join(app.DataDir(), core.LocalBackupsDirName, "test.zip"))
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()

	var found []string
	platform := false
	for _, f := range zr.File {
		if strings.HasPrefix(f.Name, orgsDirName+"/") {
			found = append(found, f.Name)
		}
		if f.Name == "data.db" {
			platform = true
		}
	}
	if len(found) > 0 {
		t.Fatalf("the platform archive carries another Base: %v", found)
	}
	if !platform {
		t.Fatal("the platform archive does not carry its own Base")
	}

	if !strings.Contains(zr.Comment, orgsDirName) {
		t.Fatalf("the archive does not state that it omits %q: %q", orgsDirName, zr.Comment)
	}
}

// The same sentence one level down. A user's database sits under its org's
// data directory and is quiesced by nothing the org's backup does, so an
// archive that swept it up would hold it torn — copied while being written,
// beside a write-ahead log copied at another instant. Inside one org's own
// boundary, so this is about the file being readable afterwards rather than
// about who can read it.
func TestOrgBackupCarriesItsOwnBase(t *testing.T) {
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatal(err)
	}
	defer app.Cleanup()

	iam := newIssuer(t)
	if err := Register(app, Config{IAMEndpoint: iam.url}); err != nil {
		t.Fatal(err)
	}

	// The org's own Base, opened the way a request opens it.
	acme, err := app.Store().Get(apis.StoreKeyBases).(apis.Bases)("acme")
	if err != nil {
		t.Fatal(err)
	}

	db := NewOrgDB(app, "")
	user, err := db.ProvisionUser("acme", "alice")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(user, []byte("alice's database"), 0600); err != nil {
		t.Fatal(err)
	}

	if err := acme.CreateBackup(context.Background(), "test.zip"); err != nil {
		t.Fatal(err)
	}

	zr, err := zip.OpenReader(filepath.Join(acme.DataDir(), core.LocalBackupsDirName, "test.zip"))
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()

	var found []string
	own := false
	for _, f := range zr.File {
		if strings.HasPrefix(f.Name, usersDirName+"/") {
			found = append(found, f.Name)
		}
		if f.Name == "data.db" {
			own = true
		}
	}
	if len(found) > 0 {
		t.Fatalf("the org's archive carries a user's database: %v", found)
	}
	if !own {
		t.Fatal("the org's archive does not carry its own Base")
	}

	if !strings.Contains(zr.Comment, usersDirName) {
		t.Fatalf("the archive does not state that it omits %q: %q", usersDirName, zr.Comment)
	}
}

// The two halves have to name the same thing. A restore moves aside everything
// it is about to replace, so an archive that does not carry a nested store and
// a restore that does not spare it would delete every one of them.
//
// One table, because it is one statement: every Base this plugin opens says the
// same thing, and the platform's Base is not a special case of it.
func TestRestoreSparesWhatTheBackupDidNotCarry(t *testing.T) {
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatal(err)
	}
	defer app.Cleanup()

	iam := newIssuer(t)
	if err := Register(app, Config{IAMEndpoint: iam.url}); err != nil {
		t.Fatal(err)
	}

	acme, err := app.Store().Get(apis.StoreKeyBases).(apis.Bases)("acme")
	if err != nil {
		t.Fatal(err)
	}

	for base, on := range map[string]core.App{"platform": app, "org": acme} {
		var created, restored []string
		on.OnBackupCreate().BindFunc(func(e *core.BackupEvent) error {
			created = e.Exclude
			return e.Next()
		})
		on.OnBackupRestore().BindFunc(func(e *core.BackupEvent) error {
			restored = e.Exclude
			return e.Next()
		})

		if err := on.CreateBackup(context.Background(), "test.zip"); err != nil {
			t.Fatal(err)
		}
		// The restore itself stops on the missing archive, after the exclusions
		// are settled — which is the part under test.
		_ = on.RestoreBackup(context.Background(), "missing.zip")

		for half, got := range map[string][]string{"backup": created, "restore": restored} {
			for _, want := range []string{orgsDirName, usersDirName} {
				if !contains(got, want) {
					t.Fatalf("the %s %s does not name %q: %v", base, half, want, got)
				}
			}
		}
	}
}

func contains(list []string, want string) bool {
	for _, v := range list {
		if v == want {
			return true
		}
	}
	return false
}
