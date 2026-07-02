package migrations

import (
	"github.com/hanzoai/base/core"
	"github.com/hanzoai/base/tools/types"
)

// Saved views/filters/sorts/groups — the metadata that turns a bare record
// table into a CRM/CMS surface (table/board/calendar with saved filter, sort,
// column, and group-by config per object).
//
// Persisted as SYSTEM COLLECTIONS (not new Go field types), so they ride the
// existing record CRUD + rules + realtime with zero core changes — one way to
// store metadata: a collection. The client translates saved _view_filters rows
// into Base's own `?filter=` query DSL at read time (tools/search/filter.go), so
// there is no second query engine either.
//
//	_views          one saved view of a target collection (type, name, group-by)
//	_view_fields    per-view column visibility + order + width
//	_view_filters   per-view filter rules (field, operator, value) in AND/OR groups
//	_view_sorts     per-view sort rules (field, direction, order)
//	_view_groups    per-view group buckets (board columns / row groups)
//
// Owner-scoped: a caller sees/manages only their own views. Ownership is the IAM
// user id (@request.auth.id), matching every other tenant-scoped collection.
func init() {
	core.SystemMigrations.Register(func(txApp core.App) error {
		for _, create := range []func(core.App) error{
			createViewsCollection,
			createViewFieldsCollection,
			createViewFiltersCollection,
			createViewSortsCollection,
			createViewGroupsCollection,
		} {
			if err := create(txApp); err != nil {
				return err
			}
		}
		return nil
	}, func(txApp core.App) error {
		// down: drop in reverse (children before parent).
		for _, name := range []string{"_view_groups", "_view_sorts", "_view_filters", "_view_fields", "_views"} {
			c, err := txApp.FindCollectionByNameOrId(name)
			if err != nil {
				continue // already gone
			}
			if err := txApp.Delete(c); err != nil {
				return err
			}
		}
		return nil
	})
}

// ownViewsRule scopes every view-metadata collection to its owner.
const ownViewsRule = "owner = @request.auth.id"

func newSystemCollection(name string) *core.Collection {
	c := core.NewBaseCollection(name)
	c.System = true
	own := ownViewsRule
	c.ListRule = types.Pointer(own)
	c.ViewRule = types.Pointer(own)
	c.CreateRule = types.Pointer(`@request.auth.id != ""`) // any signed-in user; owner enforced by client + rule
	c.UpdateRule = types.Pointer(own)
	c.DeleteRule = types.Pointer(own)
	c.Fields.Add(&core.TextField{Name: "owner", Required: true})
	return c
}

func addTimestamps(c *core.Collection) {
	c.Fields.Add(&core.AutodateField{Name: "created", OnCreate: true})
	c.Fields.Add(&core.AutodateField{Name: "updated", OnCreate: true, OnUpdate: true})
}

func createViewsCollection(txApp core.App) error {
	c := newSystemCollection("_views")
	c.Fields.Add(&core.TextField{Name: "collection", Required: true}) // the target object (collection name/id)
	c.Fields.Add(&core.TextField{Name: "name", Required: true})       // human label
	c.Fields.Add(&core.SelectField{Name: "type", MaxSelect: 1, Values: []string{"table", "board", "calendar"}})
	c.Fields.Add(&core.TextField{Name: "icon"})
	c.Fields.Add(&core.TextField{Name: "group_by_field"})            // board columns / row groups source (a select/relation field)
	c.Fields.Add(&core.TextField{Name: "calendar_field"})            // date field for the calendar view
	c.Fields.Add(&core.NumberField{Name: "position"})               // ordering in the view bar
	c.Fields.Add(&core.SelectField{Name: "filter_logic", MaxSelect: 1, Values: []string{"and", "or"}})
	c.Fields.Add(&core.BoolField{Name: "is_default"})               // the object's default view
	addTimestamps(c)
	c.AddIndex("idx_views_owner_collection", false, "owner, collection", "")
	return txApp.Save(c)
}

func createViewFieldsCollection(txApp core.App) error {
	c := newSystemCollection("_view_fields")
	c.Fields.Add(&core.TextField{Name: "view", Required: true}) // parent _views id
	c.Fields.Add(&core.TextField{Name: "field", Required: true})
	c.Fields.Add(&core.BoolField{Name: "visible"})
	c.Fields.Add(&core.NumberField{Name: "position"})
	c.Fields.Add(&core.NumberField{Name: "width"})
	addTimestamps(c)
	c.AddIndex("idx_view_fields_view", false, "view", "")
	return txApp.Save(c)
}

func createViewFiltersCollection(txApp core.App) error {
	c := newSystemCollection("_view_filters")
	c.Fields.Add(&core.TextField{Name: "view", Required: true})
	c.Fields.Add(&core.TextField{Name: "field", Required: true})
	// operator mirrors the Base filter DSL surface (=,!=,>,>=,<,<=,~,!~) plus the
	// empty/not-empty predicates the UI exposes; the client compiles it to ?filter=.
	c.Fields.Add(&core.SelectField{Name: "operator", MaxSelect: 1, Values: []string{
		"eq", "neq", "gt", "gte", "lt", "lte", "like", "nlike", "empty", "nempty",
	}})
	c.Fields.Add(&core.JSONField{Name: "value"}) // scalar, list, or relation id(s)
	c.Fields.Add(&core.NumberField{Name: "group"}) // filter-group index for nested AND/OR
	c.Fields.Add(&core.NumberField{Name: "position"})
	addTimestamps(c)
	c.AddIndex("idx_view_filters_view", false, "view", "")
	return txApp.Save(c)
}

func createViewSortsCollection(txApp core.App) error {
	c := newSystemCollection("_view_sorts")
	c.Fields.Add(&core.TextField{Name: "view", Required: true})
	c.Fields.Add(&core.TextField{Name: "field", Required: true})
	c.Fields.Add(&core.SelectField{Name: "direction", MaxSelect: 1, Values: []string{"asc", "desc"}})
	c.Fields.Add(&core.NumberField{Name: "position"})
	addTimestamps(c)
	c.AddIndex("idx_view_sorts_view", false, "view", "")
	return txApp.Save(c)
}

func createViewGroupsCollection(txApp core.App) error {
	c := newSystemCollection("_view_groups")
	c.Fields.Add(&core.TextField{Name: "view", Required: true})
	c.Fields.Add(&core.TextField{Name: "field_value", Required: true}) // the select value / relation id this bucket holds
	c.Fields.Add(&core.NumberField{Name: "position"})
	c.Fields.Add(&core.BoolField{Name: "collapsed"})
	addTimestamps(c)
	c.AddIndex("idx_view_groups_view", false, "view", "")
	return txApp.Save(c)
}
