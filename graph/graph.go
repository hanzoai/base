// Package graph is a Base's records as a typed GraphQL schema: one object type
// per collection, with its fields, its relations and the records that point at
// it; a filter input per collection whose operators per field type are the
// where clause's (core.CompileWhere); an orderBy; cursor connections with a
// total; aggregates; create, update, upsert and delete; and each record's
// history.
//
// It has no transport and no second authorization. The schema is rendered from
// the Base's own collections, so it is per Base and changes with them, and it
// is the caller's: a member sees the Base whole, a visitor the collections that
// admit visitors, anyone else those an anonymous caller may list (apis.ReachOf).
// Every field is answered by a request on the Base's own router made as the
// caller:
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
	"net"
	"net/http"
	"sync"

	"github.com/hanzoai/base/apis"
	"github.com/hanzoai/base/core"
	"github.com/zap-proto/zip"
)

// prefix begins every type name, so a host composing this schema into a larger
// one (api.hanzo.ai's /v1/graphql, under `base`) adds it without renaming.
const prefix = "Base"

// Graph is one Base's records as GraphQL.
type Graph struct {
	app    core.App
	router http.Handler

	mu    sync.Mutex
	reach map[apis.Reach]*scope
}

// scope is the graph one reach sees, and the collections it was built from.
type scope struct {
	key string
	m   *model
	sdl string
}

// New serves app's records, answering every field through router, the
// handler that serves app's REST routes.
func New(app core.App, router http.Handler) *Graph {
	return &Graph{app: app, router: router, reach: map[apis.Reach]*scope{}}
}

// Schema is the SDL of the Base's records as they stand, as far as the caller
// reaches.
func (g *Graph) Schema(ctx context.Context) (string, error) {
	_, sdl, err := g.current(ctx)
	return sdl, err
}

// current is the model and schema of the Base's collections as they stand and
// as far as the caller reaches, built again only when a collection has changed
// since the last one built for that reach.
func (g *Graph) current(ctx context.Context) (*model, string, error) {
	all, _ := g.app.Store().Get(core.StoreKeyCachedCollections).([]*core.Collection)
	if all == nil {
		var err error
		if all, err = g.app.FindAllCollections(); err != nil {
			return nil, "", err
		}
	}

	reach := g.reachOf(ctx)
	collections := make([]*core.Collection, 0, len(all))
	for _, c := range all {
		if sees(reach, c) {
			collections = append(collections, c)
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
	if s := g.reach[reach]; s != nil && s.key == key {
		return s.m, s.sdl, nil
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

	s := &scope{key: key, m: m, sdl: m.render()}
	g.reach[reach] = s
	return s.m, s.sdl, nil
}

// reachOf is how far the caller of the request behind ctx reaches into the
// Base. With no request behind it, nobody is calling, which reaches as far as
// a guest.
func (g *Graph) reachOf(ctx context.Context) apis.Reach {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apis.Prefix()+"/collections", http.NoBody)
	if err != nil {
		return apis.ReachGuest
	}
	if h := zip.Headers(ctx); h != nil {
		req.Header = h
	}
	if ip := zip.CallerOf(ctx).IP; ip != "" {
		req.RemoteAddr = net.JoinHostPort(ip, "0")
	}
	return apis.ReachOf(g.app, req)
}

// sees reports whether a caller of the reach given may see a collection in the
// schema: every one for a member, those that admit visitors for a visitor, and
// for anyone else those whose list rule admits anybody.
func sees(reach apis.Reach, c *core.Collection) bool {
	switch reach {
	case apis.ReachMember:
		return true
	case apis.ReachVisitor:
		return c.AdmitsVisitors()
	default:
		return c.ListRule != nil && *c.ListRule == ""
	}
}
