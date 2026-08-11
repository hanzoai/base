package migrations

import (
	"fmt"

	"github.com/hanzoai/base/core"
)

func init() {
	core.SystemMigrations.Add(&core.Migration{
		Up: func(txApp core.App) error {
			d := txApp.Dialect()

			_, execErr := txApp.AuxDB().NewQuery(fmt.Sprintf(`
				CREATE TABLE IF NOT EXISTS {{_logs}} (
					[[id]]      TEXT PRIMARY KEY DEFAULT ('r'||%s) NOT NULL,
					[[level]]   INTEGER DEFAULT 0 NOT NULL,
					[[message]] TEXT DEFAULT '' NOT NULL,
					[[data]]    %s DEFAULT '{}' NOT NULL,
					[[created]] TEXT DEFAULT (%s) NOT NULL
				);

				CREATE INDEX IF NOT EXISTS idx_logs_level on {{_logs}} ([[level]]);
				CREATE INDEX IF NOT EXISTS idx_logs_message on {{_logs}} ([[message]]);
				CREATE INDEX IF NOT EXISTS idx_logs_created_hour on {{_logs}} (%s);
			`, d.Random(14), d.Json(), d.Now(), core.LogHour)).Execute()

			return execErr
		},
		Down: func(txApp core.App) error {
			_, err := txApp.AuxDB().DropTable("_logs").Execute()
			return err
		},
		ReapplyCondition: func(txApp core.App, runner *core.MigrationsRunner, fileName string) (bool, error) {
			// reapply only if the _logs table doesn't exist
			exists := txApp.AuxHasTable("_logs")
			return !exists, nil
		},
	})
}
