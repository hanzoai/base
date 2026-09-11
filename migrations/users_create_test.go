package migrations_test

import (
	"testing"

	"github.com/hanzoai/base/core"
	"github.com/hanzoai/base/tools/types"
	"github.com/hanzoai/dbx"

	_ "github.com/hanzoai/base/migrations"
)

// TestUsersCreateIsSuperusersOnly pins who may add a users row.
//
// People come from IAM and are never rows here, so a new Base lets only
// superusers add one. A Base from before that whose users rules are still the
// ones the init migration wrote gets the same when it upgrades; a Base whose
// operator changed any of them keeps them.
func TestUsersCreateIsSuperusersOnly(t *testing.T) {
	app := core.NewBaseApp(core.BaseAppConfig{DataDir: t.TempDir()})
	if err := app.Bootstrap(); err != nil {
		t.Fatal(err)
	}
	defer app.ResetBootstrapState()

	users := findUsers(t, app)
	if users.CreateRule != nil {
		t.Fatalf("a new Base lets %q add users rows, want superusers only", *users.CreateRule)
	}

	// The rules as the init migration used to write them.
	users.CreateRule = types.Pointer("")
	save(t, app, users)
	rerun(t, app, "1789100000_close_users_create.go")
	if users = findUsers(t, app); users.CreateRule != nil {
		t.Fatalf("an unchanged users collection kept createRule %q", *users.CreateRule)
	}

	// The same, after an operator changed one rule.
	users.CreateRule = types.Pointer("")
	users.ListRule = types.Pointer(`@request.auth.id != ""`)
	save(t, app, users)
	rerun(t, app, "1789100000_close_users_create.go")
	if users = findUsers(t, app); users.CreateRule == nil || *users.CreateRule != "" {
		t.Fatal("the upgrade replaced a createRule on a users collection an operator had changed")
	}
}

func findUsers(t *testing.T, app core.App) *core.Collection {
	t.Helper()

	users, err := app.FindCollectionByNameOrId("users")
	if err != nil {
		t.Fatal(err)
	}
	return users
}

func save(t *testing.T, app core.App, c *core.Collection) {
	t.Helper()

	if err := app.Save(c); err != nil {
		t.Fatal(err)
	}
}

// rerun forgets that a system migration was applied and applies it again, which
// is what the first start of a new binary does to a Base from before it.
func rerun(t *testing.T, app core.App, file string) {
	t.Helper()

	if _, err := app.DB().Delete(core.DefaultMigrationsTable, dbx.HashExp{"file": file}).Execute(); err != nil {
		t.Fatal(err)
	}
	if err := app.RunSystemMigrations(); err != nil {
		t.Fatal(err)
	}
	if err := app.ReloadCachedCollections(); err != nil {
		t.Fatal(err)
	}
}
