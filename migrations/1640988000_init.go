package migrations

import (
	"fmt"
	"path/filepath"
	"runtime"

	"github.com/hanzoai/base/core"
	"github.com/hanzoai/base/tools/types"
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
		if err := createParamsTable(txApp); err != nil {
			return fmt.Errorf("_params exec error: %w", err)
		}

		// -----------------------------------------------------------

		_, execerr := txApp.DB().NewQuery(`
			CREATE TABLE {{_collections}} (
				[[id]]         TEXT PRIMARY KEY DEFAULT ('r'||lower(hex(randomblob(7)))) NOT NULL,
				[[system]]     BOOLEAN DEFAULT FALSE NOT NULL,
				[[type]]       TEXT DEFAULT "base" NOT NULL,
				[[name]]       TEXT UNIQUE NOT NULL,
				[[fields]]     JSON DEFAULT "[]" NOT NULL,
				[[indexes]]    JSON DEFAULT "[]" NOT NULL,
				[[listRule]]   TEXT DEFAULT NULL,
				[[viewRule]]   TEXT DEFAULT NULL,
				[[createRule]] TEXT DEFAULT NULL,
				[[updateRule]] TEXT DEFAULT NULL,
				[[deleteRule]] TEXT DEFAULT NULL,
				[[options]]    JSON DEFAULT "{}" NOT NULL,
				[[created]]    TEXT DEFAULT (strftime('%Y-%m-%d %H:%M:%fZ')) NOT NULL,
				[[updated]]    TEXT DEFAULT (strftime('%Y-%m-%d %H:%M:%fZ')) NOT NULL
			);

			CREATE INDEX IF NOT EXISTS idx__collections_type on {{_collections}} ([[type]]);
		`).Execute()
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
	_, execErr := txApp.DB().NewQuery(`
		CREATE TABLE {{_params}} (
			[[id]]      TEXT PRIMARY KEY DEFAULT ('r'||lower(hex(randomblob(7)))) NOT NULL,
			[[value]]   JSON DEFAULT NULL,
			[[created]] TEXT DEFAULT (strftime('%Y-%m-%d %H:%M:%fZ')) NOT NULL,
			[[updated]] TEXT DEFAULT (strftime('%Y-%m-%d %H:%M:%fZ')) NOT NULL
		);
	`).Execute()

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
