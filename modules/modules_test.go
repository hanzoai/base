package modules_test

import (
	"errors"
	"testing"

	"github.com/hanzoai/base/modules"
	"github.com/hanzoai/base/tests"
)

func TestRegisterGetList(t *testing.T) {
	modules.Register(modules.Module{
		Name:  "registry_test",
		Title: "Registry test",
		Collections: []map[string]any{
			{"name": "registry_things", "type": "base", "fields": []any{map[string]any{"name": "title", "type": "text"}}},
		},
	})

	m, ok := modules.Get("registry_test")
	if !ok || m.Title != "Registry test" || len(m.Names()) != 1 || m.Names()[0] != "registry_things" {
		t.Fatalf("get: %+v %v", m, ok)
	}

	// what Get returns is a copy
	m.Collections[0]["name"] = "changed"
	if again, _ := modules.Get("registry_test"); again.Names()[0] != "registry_things" {
		t.Fatal("a caller changed the registered module")
	}

	found := false
	for _, l := range modules.List() {
		found = found || l.Name == "registry_test"
	}
	if !found {
		t.Fatal("list does not carry the module")
	}

	defer func() {
		if recover() == nil {
			t.Fatal("registering a name twice must panic")
		}
	}()
	modules.Register(modules.Module{Name: "registry_test", Collections: m.Collections})
}

func TestInstall(t *testing.T) {
	app, _ := tests.NewTestApp()
	defer app.Cleanup()

	modules.Register(modules.Module{
		Name: "install_test",
		Collections: []map[string]any{
			{"name": "install_things", "type": "base", "fields": []any{map[string]any{"name": "title", "type": "text"}}},
		},
	})

	m, _ := modules.Get("install_test")
	if m.Installed(app) {
		t.Fatal("installed before install")
	}

	g, err := modules.Install(app, "install_test")
	if err != nil || len(g.Created) != 1 {
		t.Fatalf("install: %+v %v", g, err)
	}
	if !m.Installed(app) {
		t.Fatal("not installed after install")
	}

	if _, err := modules.Install(app, "nothing_by_this_name"); !errors.Is(err, modules.ErrUnknownModule) {
		t.Fatalf("an unknown module must be ErrUnknownModule, got %v", err)
	}
}
