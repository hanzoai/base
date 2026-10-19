package migrations

import (
	"database/sql"
	"errors"

	"github.com/hanzoai/base/core"
)

// The caller's authority in the org it acts in, on the auth record, so a rule
// can ask for it: `@request.auth.admin = true` (it administers the org) and
// `@request.auth.role = "owner"` (its role there: owner, admin or member).
//
// resolveJWKSToken stamps both from the verified token onto the unsaved record
// it mirrors a person into, and the rule engine reads them off that record. They
// are schema fields because a record carries only the fields its collection
// declares. Superusers are skipped: rules never stop them.
func init() {
	core.SystemMigrations.Register(func(txApp core.App) error {
		users, err := txApp.FindCollectionByNameOrId("users")
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}

		changed := false
		if users.Fields.GetByName(core.FieldNameAuthAdmin) == nil {
			users.Fields.Add(&core.BoolField{Name: core.FieldNameAuthAdmin})
			changed = true
		}
		if users.Fields.GetByName(core.FieldNameAuthRole) == nil {
			users.Fields.Add(&core.TextField{Name: core.FieldNameAuthRole, Max: 32})
			changed = true
		}
		if !changed {
			return nil
		}

		return txApp.Save(users)
	}, func(txApp core.App) error {
		users, err := txApp.FindCollectionByNameOrId("users")
		if err != nil {
			return nil
		}
		users.Fields.RemoveByName(core.FieldNameAuthAdmin)
		users.Fields.RemoveByName(core.FieldNameAuthRole)
		return txApp.Save(users)
	})
}
