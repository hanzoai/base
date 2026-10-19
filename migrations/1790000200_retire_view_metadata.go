package migrations

import (
	"database/sql"
	"errors"

	"github.com/hanzoai/base/core"
)

// The five _view* system collections 1780000000_views_metadata made are
// retired. Nothing ever read them — not Base, not the admin UI, not a client —
// and as system collections they could not be what an org's views need to be:
// shared by the org, changed by the org's admin, and the org's own schema to
// extend. Team views are an ordinary `views` collection now, which the CRM
// module installs (modules/crm), and a person's own views live in their IAM
// preferences.
//
// A system collection refuses deletion, so each one is released from that
// first. Its rows go with its table.
func init() {
	core.SystemMigrations.Register(func(txApp core.App) error {
		// children before the parent, though none references another by relation
		for _, name := range []string{"_view_groups", "_view_sorts", "_view_filters", "_view_fields", "_views"} {
			c, err := txApp.FindCollectionByNameOrId(name)
			if errors.Is(err, sql.ErrNoRows) {
				continue
			}
			if err != nil {
				return err
			}

			c.System = false
			if err := txApp.Delete(c); err != nil {
				return err
			}
		}
		return nil
	}, nil)
}
