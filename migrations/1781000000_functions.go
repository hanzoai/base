package migrations

import (
	"github.com/hanzoai/base/core"
)

// Functions live in the Base they run in.
//
// A collection rather than a directory, because everything a function needs is
// already what a record has: an org's Base is its own file, so a tenant's
// functions are in that file and no query can reach across two; the rules on
// this collection are who may write one and who may call one, evaluated by the
// same record path as any other row; and a function rides the backups, the WAL
// replication and the audit log without any of them learning a new kind of
// thing. Code on disk would be one directory for every tenant in the process.
//
// The id IS the name. A function has one identity, so `POST /v1/functions/greet`
// names the same thing the primary key does, uniqueness is the primary key's
// job, and no route has to turn one identifier into another.
//
// The source is hidden and the rules are nil, which together say the same thing
// twice on purpose: out of the box a function is written and read by a superuser
// and by nobody else. Hidden is the stronger half — a non-superuser's write has
// hidden fields dropped before it reaches the record, so opening the rules alone
// does not open authorship.
//
// It is NOT a system collection, for the one reason that matters: a system
// collection's rules can never be changed, and who may call a function is
// precisely what a deployment has to be able to say. The scheduling collections
// are shipped the same way, by the same list, for the same reason.

func init() {
	core.SystemMigrations.Register(func(txApp core.App) error {
		if _, err := txApp.FindCollectionByNameOrId(core.CollectionNameFunctions); err == nil {
			return nil
		}

		c := core.NewBaseCollection(core.CollectionNameFunctions)
		c.Fields.Add(
			&core.TextField{
				Name:       core.FieldNameId,
				System:     true,
				PrimaryKey: true,
				Required:   true,
				Min:        1,
				Max:        63,
				// A name a URL can carry whole and a log line can be read out of.
				Pattern: `^[a-z0-9][a-z0-9_-]*$`,
			},
			&core.TextField{
				Name:     core.FieldNameSource,
				Required: true,
				Hidden:   true,
				Max:      256 * 1024,
			},
			&core.AutodateField{Name: "created", OnCreate: true},
			&core.AutodateField{Name: "updated", OnCreate: true, OnUpdate: true},
		)

		return txApp.Save(c)
	}, func(txApp core.App) error {
		c, err := txApp.FindCollectionByNameOrId(core.CollectionNameFunctions)
		if err != nil {
			return nil
		}
		return txApp.Delete(c)
	})
}
