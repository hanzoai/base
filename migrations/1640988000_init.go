package migrations

import (
	"fmt"
	"path/filepath"
	"runtime"

	"github.com/hanzoai/base/core"
	"github.com/hanzoai/base/tools/types"
	"github.com/hanzoai/dbx"
)

// Register is a short alias for `AppMigrations.Register()`
// that is usually used in external/user defined migrations.
func Register(
	up func(app core.App) error,
	down func(app core.App) error,
	optFilename ...string,
) {
	var optFiles []string
	if len(optFilename) > 0 {
		optFiles = optFilename
	} else {
		_, path, _, _ := runtime.Caller(1)
		optFiles = append(optFiles, filepath.Base(path))
	}
	core.AppMigrations.Register(up, down, optFiles...)
}

func init() {
	core.SystemMigrations.Register(func(txApp core.App) error {
		if err := runPrelude(txApp, txApp.DB()); err != nil {
			return err
		}

		if err := createParamsTable(txApp); err != nil {
			return fmt.Errorf("_params exec error: %w", err)
		}

		// -----------------------------------------------------------

		d := txApp.Dialect()

		_, execerr := txApp.DB().NewQuery(fmt.Sprintf(`
			CREATE TABLE {{_collections}} (
				[[id]]         TEXT PRIMARY KEY DEFAULT ('r'||%s) NOT NULL,
				[[system]]     BOOLEAN DEFAULT FALSE NOT NULL,
				[[type]]       TEXT DEFAULT 'base' NOT NULL,
				[[name]]       TEXT UNIQUE NOT NULL,
				[[fields]]     %s DEFAULT '[]' NOT NULL,
				[[indexes]]    %s DEFAULT '[]' NOT NULL,
				[[listRule]]   TEXT DEFAULT NULL,
				[[viewRule]]   TEXT DEFAULT NULL,
				[[createRule]] TEXT DEFAULT NULL,
				[[updateRule]] TEXT DEFAULT NULL,
				[[deleteRule]] TEXT DEFAULT NULL,
				[[options]]    %s DEFAULT '{}' NOT NULL,
				[[created]]    TEXT DEFAULT (%s) NOT NULL,
				[[updated]]    TEXT DEFAULT (%s) NOT NULL
			);

			CREATE INDEX IF NOT EXISTS idx__collections_type on {{_collections}} ([[type]]);
		`, d.Random(14), d.Json(), d.Json(), d.Json(), d.Now(), d.Now())).Execute()
		if execerr != nil {
			return fmt.Errorf("_collections exec error: %w", execerr)
		}

		if err := createSuperusersCollection(txApp); err != nil {
			return fmt.Errorf("_superusers error: %w", err)
		}

		if err := createUsersCollection(txApp); err != nil {
			return fmt.Errorf("users error: %w", err)
		}

		return nil
	}, func(txApp core.App) error {
		tables := []string{
			"users",
			core.CollectionNameSuperusers,
			"_params",
			"_collections",
		}

		for _, name := range tables {
			if _, err := txApp.DB().DropTable(name).Execute(); err != nil {
				return err
			}
		}

		return nil
	})
}

func createParamsTable(txApp core.App) error {
	d := txApp.Dialect()

	_, execErr := txApp.DB().NewQuery(fmt.Sprintf(`
		CREATE TABLE {{_params}} (
			[[id]]      TEXT PRIMARY KEY DEFAULT ('r'||%s) NOT NULL,
			[[value]]   %s DEFAULT NULL,
			[[created]] TEXT DEFAULT (%s) NOT NULL,
			[[updated]] TEXT DEFAULT (%s) NOT NULL
		);
	`, d.Random(14), d.Json(), d.Now(), d.Now())).Execute()

	return execErr
}

func createSuperusersCollection(txApp core.App) error {
	superusers := core.NewAuthCollection(core.CollectionNameSuperusers)
	superusers.System = true
	superusers.Fields.Add(&core.EmailField{
		Name:     "email",
		System:   true,
		Required: true,
	})
	superusers.Fields.Add(&core.AutodateField{
		Name:     "created",
		System:   true,
		OnCreate: true,
	})
	superusers.Fields.Add(&core.AutodateField{
		Name:     "updated",
		System:   true,
		OnCreate: true,
		OnUpdate: true,
	})
	superusers.AuthToken.Duration = 86400 // 1 day

	return txApp.Save(superusers)
}

func createUsersCollection(txApp core.App) error {
	users := core.NewAuthCollection("users", "_users_auth_")

	ownerRule := "id = @request.auth.id"
	users.ListRule = types.Pointer(ownerRule)
	users.ViewRule = types.Pointer(ownerRule)
	users.CreateRule = types.Pointer("")
	users.UpdateRule = types.Pointer(ownerRule)
	users.DeleteRule = types.Pointer(ownerRule)

	users.Fields.Add(&core.TextField{
		Name: "name",
		Max:  255,
	})
	users.Fields.Add(&core.FileField{
		Name:      "avatar",
		MaxSelect: 1,
		MimeTypes: []string{"image/jpeg", "image/png", "image/svg+xml", "image/gif", "image/webp"},
	})
	users.Fields.Add(&core.AutodateField{
		Name:     "created",
		OnCreate: true,
	})
	users.Fields.Add(&core.AutodateField{
		Name:     "updated",
		OnCreate: true,
		OnUpdate: true,
	})
	users.OAuth2.MappedFields.Name = "name"
	users.OAuth2.MappedFields.AvatarURL = "avatar"

	return txApp.Save(users)
}

// runPrelude gives the schema whatever the engine expects it to already have —
// the collations an index can name, and the like. It is idempotent, so a
// database that predates it gets them on the next boot.
//
// Only the data schema asks for it. The log schema names no collation, and
// when both live in one database the second caller waits on the first one's
// uncommitted catalog row for as long as that transaction is open.
func runPrelude(app core.App, db dbx.Builder) error {
	prelude := app.Dialect().Prelude()
	if prelude == "" {
		return nil
	}

	_, err := db.NewQuery(prelude).Execute()

	return err
}
