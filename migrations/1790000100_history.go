package migrations

import (
	"database/sql"
	"errors"

	"github.com/hanzoai/base/core"
)

// _history holds a row for every create, update and delete of a record in a
// collection that keeps its history (the base collection History option): the
// collection's id, the record's id, what was done, who did it and which fields
// changed. core/record_history.go writes it in the record's own transaction.
//
// Every rule is nil, so only a superuser reads the table directly. A person
// reads one record's history at /v1/collections/{collection}/records/{id}/history,
// which asks the record's own view rule.
func init() {
	core.SystemMigrations.Register(func(txApp core.App) error {
		if _, err := txApp.FindCollectionByNameOrId(core.CollectionNameHistory); err == nil {
			return nil
		} else if !errors.Is(err, sql.ErrNoRows) {
			return err
		}

		c := core.NewBaseCollection(core.CollectionNameHistory)
		c.System = true
		c.Fields.Add(
			&core.TextField{Name: "collection", Required: true, Max: 100, System: true},
			&core.TextField{Name: "record", Required: true, Max: 100, System: true},
			&core.SelectField{
				Name: "action", Required: true, MaxSelect: 1, System: true,
				Values: []string{core.HistoryCreate, core.HistoryUpdate, core.HistoryDelete},
			},
			&core.ActorField{Name: "actor", OnCreate: true, System: true},
			&core.JSONField{Name: "changes", MaxSize: 8 << 20, System: true},
			&core.AutodateField{Name: "created", OnCreate: true, System: true},
		)
		c.AddIndex("idx_history_record", false, "collection, record, created", "")

		return txApp.Save(c)
	}, nil)
}
