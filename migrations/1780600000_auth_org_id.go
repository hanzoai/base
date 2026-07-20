package migrations

import "github.com/hanzoai/base/core"

// Multi-tenant identity: expose the caller's IAM org on the auth record so
// collection rules can scope by tenant (`@request.auth.org_id = org`).
//
// resolveJWKSToken (apis/middlewares.go) already stamps the JWT `owner` claim
// onto the ephemeral auth record as `org_id`, but ONLY when the auth collection
// declares that field; the rule engine likewise resolves `@request.auth.org_id`
// from the record's PublicExport, which serializes SCHEMA fields only. Both need
// `org_id` to be a real field on `users`. Idempotent. `_superusers` is skipped —
// superusers bypass record rules, so org on the admin record is irrelevant.
func init() {
	core.SystemMigrations.Register(func(txApp core.App) error {
		c, err := txApp.FindCollectionByNameOrId("users")
		if err != nil {
			return err
		}
		if c.Fields.GetByName("org_id") == nil {
			c.Fields.Add(&core.TextField{Name: "org_id"})
			return txApp.Save(c)
		}
		return nil
	}, func(txApp core.App) error {
		c, err := txApp.FindCollectionByNameOrId("users")
		if err != nil {
			return nil
		}
		if c.Fields.GetByName("org_id") != nil {
			c.Fields.RemoveByName("org_id")
			return txApp.Save(c)
		}
		return nil
	})
}
