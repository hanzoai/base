package migrations_test

import (
	"testing"

	"github.com/hanzoai/base/core"

	_ "github.com/hanzoai/base/migrations"
)

var viewMetadata = []string{"_views", "_view_fields", "_view_filters", "_view_sorts", "_view_groups"}

// The _view* system collections are gone from a new Base and from one that
// had them, rows and all.
func TestViewMetadataIsRetired(t *testing.T) {
	app := core.NewBaseApp(core.BaseAppConfig{DataDir: t.TempDir()})
	if err := app.Bootstrap(); err != nil {
		t.Fatal(err)
	}
	defer app.ResetBootstrapState()

	gone := func() {
		t.Helper()
		for _, name := range viewMetadata {
			if _, err := app.FindCollectionByNameOrId(name); err == nil {
				t.Fatalf("%s is still a collection", name)
			}
			if app.HasTable(name) {
				t.Fatalf("%s is still a table", name)
			}
		}
	}
	gone()

	// a Base from before the retirement: the collections exist and hold a row
	rerun(t, app, "1780000000_views_metadata.go")
	views, err := app.FindCollectionByNameOrId("_views")
	if err != nil {
		t.Fatalf("the old migration did not recreate _views: %v", err)
	}
	r := core.NewRecord(views)
	r.Load(map[string]any{"owner": "u1", "collection": "people", "name": "Mine"})
	if err := app.Save(r); err != nil {
		t.Fatal(err)
	}

	rerun(t, app, "1790000200_retire_view_metadata.go")
	gone()
}
