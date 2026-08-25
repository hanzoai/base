package core_test

import (
	"testing"

	"github.com/hanzoai/base/core"
	"github.com/hanzoai/base/tests"
)

// A collection may be created with its id set to its own name — NewBaseCollection
// takes the id as an optional argument and nothing forbids the two matching, and
// callers do it to keep a stable, readable id across deployments.
//
// The name/id collision check has to exclude the collection being saved. It did
// not, so it found the collection ITSELF and refused: such a collection passed
// creation (nothing to collide with yet) and could never be updated again. Every
// later schema change — a new field, a new index — died at startup with "The name
// must not match an existing collection id", and the deployment stayed on the old
// schema or stopped booting.
func TestCollectionWithIdEqualToItsNameCanBeUpdated(t *testing.T) {
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatal(err)
	}
	defer app.Cleanup()

	c := core.NewBaseCollection("ledger", "ledger")
	c.Fields.Add(&core.TextField{Name: "memo"})
	if err := app.Save(c); err != nil {
		t.Fatalf("create: %v", err)
	}

	// The upgrade an existing deployment performs on restart.
	again, err := app.FindCollectionByNameOrId("ledger")
	if err != nil {
		t.Fatal(err)
	}
	again.Fields.Add(&core.NumberField{Name: "amount"})
	if err := app.Save(again); err != nil {
		t.Fatalf("update: %v — a collection whose id is its own name cannot be changed", err)
	}

	got, err := app.FindCollectionByNameOrId("ledger")
	if err != nil {
		t.Fatal(err)
	}
	if got.Fields.GetByName("amount") == nil {
		t.Fatal("the new field was not saved")
	}
}

// The check still has to do its real job: a DIFFERENT collection's id is not
// available as a name, or FindCollectionByNameOrId would have two answers.
func TestANameStillCannotTakeAnotherCollectionsId(t *testing.T) {
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatal(err)
	}
	defer app.Cleanup()

	first := core.NewBaseCollection("alpha", "shared_id")
	if err := app.Save(first); err != nil {
		t.Fatalf("create first: %v", err)
	}

	second := core.NewBaseCollection("beta")
	if err := app.Save(second); err != nil {
		t.Fatalf("create second: %v", err)
	}
	second.Name = "shared_id" // the other collection's id
	if err := app.Save(second); err == nil {
		t.Fatal("a collection took another collection's id as its name")
	}
}
