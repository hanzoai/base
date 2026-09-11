package migrations

import (
	"database/sql"
	"errors"

	"github.com/hanzoai/base/core"
)

// People come from IAM and are never rows here, so only superusers add a users
// row. A users collection whose five rules are still the ones the init migration
// wrote, createRule "" among them, gets a null createRule. Any other rule set was
// chosen by an operator and stays as it is.
func init() {
	core.SystemMigrations.Register(func(txApp core.App) error {
		users, err := txApp.FindCollectionByNameOrId("users")
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}

		owner := "id = @request.auth.id"
		for _, rule := range []*string{users.ListRule, users.ViewRule, users.UpdateRule, users.DeleteRule} {
			if rule == nil || *rule != owner {
				return nil
			}
		}
		if users.CreateRule == nil || *users.CreateRule != "" {
			return nil
		}

		users.CreateRule = nil

		return txApp.Save(users)
	}, nil)
}
