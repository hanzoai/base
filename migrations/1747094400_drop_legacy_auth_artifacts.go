package migrations

import (
	"encoding/json"
	"fmt"

	"github.com/hanzoai/dbx"
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
//
// We operate at the raw SQL layer because the deleted password Field
// type means the collection-model unmarshaller cannot rehydrate the
// pre-rip auth-collection schema rows. Going through the regular
// app.Delete + collection-save path explodes on the
// "unknown field type: password" decoder error.
func init() {
	core.SystemMigrations.Register(func(txApp core.App) error {
		legacyAuxCollections := []string{
			"_mfas",
			"_otps",
			"_externalAuths",
			"_authOrigins",
		}

		for _, name := range legacyAuxCollections {
			// Drop the record table first if it exists. Use IF EXISTS
			// so a fresh install (where the init migration no longer
			// creates these tables) is a no-op.
			if _, err := txApp.DB().NewQuery(
				fmt.Sprintf("DROP TABLE IF EXISTS {{%s}}", name),
			).Execute(); err != nil {
				return fmt.Errorf("failed to drop legacy auth table %q: %w", name, err)
			}
			if _, err := txApp.DB().Delete("_collections", dbx.HashExp{"name": name}).Execute(); err != nil {
				return fmt.Errorf("failed to remove legacy auth collection row %q: %w", name, err)
			}
		}

		// Strip the `password` field from every auth collection. Pull
		// the JSON fields-list straight out of the row, drop entries
		// whose name is "password", write it back, and ALTER TABLE to
		// drop the column. Skip the underlying ALTER TABLE if the
		// column never existed.
		var rows []struct {
			Id     string
			Name   string
			Fields string
		}
		err := txApp.DB().NewQuery("SELECT id, name, fields FROM {{_collections}} WHERE type = 'auth'").All(&rows)
		if err != nil {
			return fmt.Errorf("failed to enumerate auth collections: %w", err)
		}

		for _, row := range rows {
			var fields []map[string]any
			if err := json.Unmarshal([]byte(row.Fields), &fields); err != nil {
				return fmt.Errorf("failed to decode fields for %q: %w", row.Name, err)
			}

			filtered := fields[:0]
			hadPassword := false
			for _, f := range fields {
				if name, _ := f["name"].(string); name == "password" {
					hadPassword = true
					continue
				}
				filtered = append(filtered, f)
			}
			if !hadPassword {
				continue
			}

			encoded, err := json.Marshal(filtered)
			if err != nil {
				return fmt.Errorf("failed to encode trimmed fields for %q: %w", row.Name, err)
			}

			if _, err := txApp.DB().Update(
				"_collections",
				dbx.Params{"fields": string(encoded)},
				dbx.HashExp{"id": row.Id},
			).Execute(); err != nil {
				return fmt.Errorf("failed to persist password-stripped %q: %w", row.Name, err)
			}

			// Drop the column on the record table; SQLite supports
			// ALTER TABLE DROP COLUMN since 3.35. Soft-fail if the
			// column doesn't exist (older fork that never wrote one).
			tableName := row.Name
			if _, err := txApp.DB().NewQuery(
				fmt.Sprintf("ALTER TABLE {{%s}} DROP COLUMN [[password]]", tableName),
			).Execute(); err != nil {
				txApp.Logger().Warn("could not drop password column (likely already absent)", "table", tableName, "error", err)
			}
		}

		return txApp.ReloadCachedCollections()
	}, nil)
}
