// Package graph is a Base's records as a typed GraphQL schema: one object type
// per collection, with its fields, its relations and the records that point at
// it; a filter input per collection whose operators per field type are the
// where clause's (core.CompileWhere); an orderBy; cursor connections with a
// total; aggregates; create, update, upsert and delete; and each record's
// history.
//
// It has no transport and no second authorization. The schema is rendered from
// the Base's own collections, so it is per Base and changes with them, and every
// field is answered by a request on the Base's own router made as the caller:
// the record routes, the aggregate route and the history route, with their list
// and view rules, their tenancy and their rate limits, are the REST path's.
// zip's engine parses, binds variables and fragments, applies the bounds and the
// request's resolution budget, and projects each answer through its selection
// (zip.MountGraphFor). A host mounts it with the router it serves the Base with:
//
//	g := graph.New(app, router)
//	zipApp.MountGraphFor("/v1/base/graphql", g.Schema, g.Resolve)
//
// There are no subscriptions: realtime is Base's own SSE stream, under the same
// rules, and a client refetches what changed by id.
package graph

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"sync"

	"github.com/hanzoai/base/core"
)

// prefix begins every type name, so a host composing this schema into a larger
// one (api.hanzo.ai's /v1/graphql, under `base`) adds it without renaming.
const prefix = "Base"

// Graph is one Base's records as GraphQL.
type Graph struct {
	app    core.App
	router http.Handler

	mu  sync.Mutex
	key string
	m   *model
	sdl string
}

// New serves app's records, answering every field through router, the
// handler that serves app's REST routes.
func New(app core.App, router http.Handler) *Graph {
	return &Graph{app: app, router: router}
}

// Schema is the SDL of the Base's records as they stand.
func (g *Graph) Schema(context.Context) (string, error) {
	_, sdl, err := g.current()
	return sdl, err
}

// current is the model and schema of the Base's collections as they stand,
// built again only when a collection has changed since the last one.
func (g *Graph) current() (*model, string, error) {
	collections, _ := g.app.Store().Get(core.StoreKeyCachedCollections).([]*core.Collection)
	if collections == nil {
		var err error
		if collections, err = g.app.FindAllCollections(); err != nil {
			return nil, "", err
		}
	}

	h := sha256.New()
	for _, c := range collections {
		h.Write([]byte(c.Id))
		h.Write([]byte{0})
		h.Write([]byte(c.Updated.String()))
		h.Write([]byte{0})
	}
	key := hex.EncodeToString(h.Sum(nil))

	g.mu.Lock()
	defer g.mu.Unlock()
	if g.m != nil && g.key == key {
		return g.m, g.sdl, nil
	}

	m := build(collections)
	for _, c := range m.colls {
		for _, f := range c.fields {
			if f.kind == kComposite {
				m.used[f.f.Type()] = true
			}
		}
	}
	if m.history {
		m.used[core.FieldTypeActor] = true
	}

	g.m, g.sdl, g.key = m, m.render(), key
	return g.m, g.sdl, nil
}
