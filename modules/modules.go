// Package modules holds the named sets of collections an org installs into its
// Base: the CRM is one. A module is an import document with a name, and
// installing it is a grow (core.GrowCollections) — additive, so installing a
// module again, or a newer version of it, adds what is missing and changes
// nothing the org already has.
//
// A binary offers the modules it links: each one registers itself in its
// package's init, and a binary blank-imports the ones it serves.
package modules

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"sync"

	"github.com/hanzoai/base/core"
)

// ErrUnknownModule is returned for a module nothing registered.
var ErrUnknownModule = errors.New("unknown module")

// Module is a named set of collections.
type Module struct {
	// Name is how the module is addressed (POST /v1/modules/{name}).
	Name string `json:"name"`

	// Title and Description are what a person reads about it.
	Title       string `json:"title"`
	Description string `json:"description"`

	// Collections are the module's collection documents, in the shape an
	// import takes. A relation names its collection by name.
	Collections []map[string]any `json:"collections"`
}

var (
	mu       sync.RWMutex
	registry = map[string]Module{}
)

// Register makes a module installable. Registering a name twice panics: two
// packages claiming one name is a build mistake, not a runtime condition.
func Register(m Module) {
	if m.Name == "" || len(m.Collections) == 0 {
		panic("modules: a module needs a name and collections")
	}

	mu.Lock()
	defer mu.Unlock()

	if _, ok := registry[m.Name]; ok {
		panic(fmt.Sprintf("modules: %q registered twice", m.Name))
	}
	registry[m.Name] = clone(m)
}

// Get returns a copy of the named module.
func Get(name string) (Module, bool) {
	mu.RLock()
	defer mu.RUnlock()

	m, ok := registry[name]
	if !ok {
		return Module{}, false
	}
	return clone(m), true
}

// List returns a copy of every registered module, by name.
func List() []Module {
	mu.RLock()
	defer mu.RUnlock()

	out := make([]Module, 0, len(registry))
	for _, m := range registry {
		out = append(out, clone(m))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Install grows the named module into app and reports what changed.
func Install(app core.App, name string) (*core.Growth, error) {
	m, ok := Get(name)
	if !ok {
		return nil, fmt.Errorf("%w %q", ErrUnknownModule, name)
	}
	return core.GrowCollections(app, m.Collections)
}

// Names returns the names of the module's collections, in order.
func (m Module) Names() []string {
	names := make([]string, 0, len(m.Collections))
	for _, c := range m.Collections {
		if n, _ := c["name"].(string); n != "" {
			names = append(names, n)
		}
	}
	return names
}

// Installed reports whether app has every collection the module names.
func (m Module) Installed(app core.App) bool {
	for _, n := range m.Names() {
		if _, err := app.FindCachedCollectionByNameOrId(n); err != nil {
			return false
		}
	}
	return true
}

// clone deep-copies a module, so no caller can change what is registered.
func clone(m Module) Module {
	raw, err := json.Marshal(m.Collections)
	if err != nil {
		panic(fmt.Sprintf("modules: %q does not encode: %v", m.Name, err))
	}
	out := m
	out.Collections = nil
	if err := json.Unmarshal(raw, &out.Collections); err != nil {
		panic(fmt.Sprintf("modules: %q does not decode: %v", m.Name, err))
	}
	return out
}
