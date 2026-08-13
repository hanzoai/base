package org

import (
	"archive/zip"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

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

// The two halves have to name the same thing. A restore moves aside everything
// it is about to replace, so an archive that does not carry the orgs and a
// restore that does not spare them would delete every one of them.
func TestPlatformRestoreSparesWhatTheBackupDidNotCarry(t *testing.T) {
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatal(err)
	}
	defer app.Cleanup()

	iam := newIssuer(t)
	if err := Register(app, Config{IAMEndpoint: iam.url}); err != nil {
		t.Fatal(err)
	}

	var created, restored []string
	app.OnBackupCreate().BindFunc(func(e *core.BackupEvent) error {
		created = e.Exclude
		return e.Next()
	})
	app.OnBackupRestore().BindFunc(func(e *core.BackupEvent) error {
		restored = e.Exclude
		return e.Next()
	})

	if err := app.CreateBackup(context.Background(), "test.zip"); err != nil {
		t.Fatal(err)
	}
	// The restore itself stops on the missing archive, after the exclusions
	// are settled — which is the part under test.
	_ = app.RestoreBackup(context.Background(), "missing.zip")

	for name, got := range map[string][]string{"backup": created, "restore": restored} {
		if !contains(got, orgsDirName) {
			t.Fatalf("the %s does not name %q: %v", name, orgsDirName, got)
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
