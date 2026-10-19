package apis

import (
	"encoding/json"
	"fmt"

	"github.com/hanzoai/base/core"
	"github.com/spf13/cast"
)

// An org admin changes its own org's schema through the same routes a superuser
// does, over the collections that are the org's to change: ordinary base
// collections (SchemaManageable). Every check below is asked of the collection
// as it is and as the request would leave it, so no change can turn a
// collection the org may not touch into one it may, or the reverse.
//
// A superuser is never asked: its authority is the platform's and wider than
// any one org's, and it is never reached through an org admin's.

// refuseUnmanageable answers 403 when a request that is not a superuser's would
// touch a collection an org admin may not change.
func refuseUnmanageable(e *core.RequestEvent, action string, collections ...*core.Collection) error {
	if e.HasSuperuserAuth() {
		return nil
	}

	for _, c := range collections {
		if !SchemaManageable(c) {
			name := ""
			if c != nil {
				name = c.Name
			}
			return e.ForbiddenError(fmt.Sprintf("An org admin cannot %s the collection %q: only the org's own base collections are its to change.", action, name), nil)
		}
	}

	return nil
}

// logOrgAdminSchemaChange records a schema change an org admin made.
func logOrgAdminSchemaChange(e *core.RequestEvent, action string, collections ...string) {
	if e.HasSuperuserAuth() {
		return
	}

	org, _ := e.Get(RequestEventKeyOrg).(string)
	sub, _ := e.Get(RequestEventKeySub).(string)

	e.App.Logger().Info("base: schema change by an org admin",
		"org", org,
		"sub", sub,
		"action", action,
		"collections", collections,
	)
}

// importedCollections is every collection an import document names, as it is
// now (nil for one the import would create) and as the import would leave it.
func importedCollections(app core.App, docs []map[string]any) (current, after []*core.Collection, err error) {
	for _, doc := range docs {
		identifier := cast.ToString(doc["id"])
		if identifier == "" {
			identifier = cast.ToString(doc["name"])
		}

		var would *core.Collection
		existing, _ := app.FindCollectionByNameOrId(identifier)
		if existing != nil {
			current = append(current, existing)

			// a fresh copy, so the unmarshal below leaves the cached one alone
			would, err = app.FindCollectionByNameOrId(existing.Id)
			if err != nil {
				return nil, nil, err
			}
		} else {
			would = core.NewCollection(cast.ToString(doc["type"]), cast.ToString(doc["name"]), cast.ToString(doc["id"]))
		}

		raw, err := json.Marshal(doc)
		if err != nil {
			return nil, nil, err
		}
		if err := json.Unmarshal(raw, would); err != nil {
			return nil, nil, err
		}

		after = append(after, would)
	}

	return current, after, nil
}
