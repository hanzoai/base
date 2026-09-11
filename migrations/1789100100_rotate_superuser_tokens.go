package migrations

import "github.com/hanzoai/base/core"

// Superuser tokens come from IAM. Replacing the secrets a Base signs superuser
// tokens with ends every superuser token a Base signed itself.
func init() {
	core.SystemMigrations.Register(func(txApp core.App) error {
		superusers, err := txApp.FindCollectionByNameOrId(core.CollectionNameSuperusers)
		if err != nil {
			return err
		}

		superusers.RotateTokenSecrets()

		return txApp.Save(superusers)
	}, nil)
}
