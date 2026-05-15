package migrations

import (
	"fmt"

	"github.com/hanzoai/base/core"
)

// IAM-native cleanup: drop the four legacy local-auth artifact
// collections (_mfas, _otps, _externalAuths, _authOrigins) and strip
// the `password` field from every surviving auth collection.
//
// On a fresh install these tables never exist (the init migration
// stopped emitting them), so the steps no-op. On an upgrade from a
// pre-IAM Base build, this migration retires the dead schema. There
// is no Down: IAM is the only auth source going forward.
func init() {
	core.SystemMigrations.Register(func(txApp core.App) error {
		legacyAuxCollections := []string{
			"_mfas",
			"_otps",
			"_externalAuths",
			"_authOrigins",
		}

		for _, name := range legacyAuxCollections {
			col, err := txApp.FindCollectionByNameOrId(name)
			if err != nil {
				continue // already absent or never created — fine
			}
			if err := txApp.Delete(col); err != nil {
				return fmt.Errorf("failed to drop legacy auth collection %q: %w", name, err)
			}
		}

		// Strip the `password` field (and its underlying column) from
		// every auth collection that still carries one. Save through
		// the collection model so SyncRecordTableSchema drops the
		// column atomically with the field-list update.
		authCollections, err := txApp.FindAllCollections(core.CollectionTypeAuth)
		if err != nil {
			return fmt.Errorf("failed to enumerate auth collections: %w", err)
		}

		for _, col := range authCollections {
			if col.Fields.GetByName("password") == nil {
				continue
			}

			col.Fields.RemoveByName("password")
			if err := txApp.SaveNoValidate(col); err != nil {
				return fmt.Errorf("failed to persist password-stripped %q: %w", col.Name, err)
			}
		}

		return nil
	}, nil)
}
