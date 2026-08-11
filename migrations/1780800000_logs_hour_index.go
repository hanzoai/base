package migrations

import (
	"fmt"

	"github.com/hanzoai/base/core"
)

// The hourly log rollup used to bucket its instants with a calendar function,
// which each engine spells differently and only some can build an index on.
// It buckets by the leading characters of the stored text now, so the index
// backing it has to be rebuilt to match.
func init() {
	core.SystemMigrations.Add(&core.Migration{
		Up: func(txApp core.App) error {
			_, err := txApp.AuxDB().NewQuery(fmt.Sprintf(`
				DROP INDEX IF EXISTS idx_logs_created_hour;
				CREATE INDEX IF NOT EXISTS idx_logs_created_hour on {{_logs}} (%s);
			`, core.LogHour)).Execute()

			return err
		},
		Down: func(txApp core.App) error {
			_, err := txApp.AuxDB().NewQuery(`DROP INDEX IF EXISTS idx_logs_created_hour`).Execute()
			return err
		},
	})
}
