package apis

import (
	"net/http"
	"sync"
	"time"

	"github.com/hanzoai/base/core"
	"github.com/hanzoai/base/tools/claims"
	"github.com/hanzoai/base/tools/hook"
	"github.com/hanzoai/base/tools/security"
)

// grantTTL is how long a grant is good for. It is minted immediately before the
// stream is opened, so what it has to cover is one round trip.
const grantTTL = 30 * time.Second

// grantMiddlewareId names the middleware that spends a grant.
const grantMiddlewareId = "baseStreamGrant"

// grant is the authorization a realtime stream is opened with.
//
// A stream is opened by EventSource, which sends no headers, so the credential
// the caller already holds cannot travel on the request that opens it. A grant
// travels instead: an authenticated POST mints one, the GET spends it, and it is
// good once and for half a minute.
//
// The string itself is random and stands for nothing. What it stands FOR is held
// here in the process — the Base the minting request resolved to and the identity
// it resolved as — so a stream is opened with exactly the authorization of the
// request that asked for it, and the caller's own credential never has to be
// re-presented over a channel that cannot carry it.
//
// The shorter path is to put the caller's own token in the query, which is what
// most SSE APIs do. It spends the wrong thing: an IAM token opens every service
// in the estate for as long as it lives, and a query string is read by every
// proxy, ingress and access log between the browser and here. A grant opens one
// stream, on one Base, once, for thirty seconds.
type grant struct {
	app      core.App
	auth     *core.Record
	sub      string
	org      string
	orgs     []string
	orgAdmin bool
	deadline time.Time
}

// apply puts the grant's authorization on the request, leaving it in the state a
// header-authenticated request arrives in: the Base it reads and writes, the
// record its rules resolve @request.auth against, and the org it acts in.
func (g *grant) apply(e *core.RequestEvent) {
	e.App = g.app
	e.Auth = g.auth
	e.Set(RequestEventKeySub, g.sub)
	e.Set(RequestEventKeyOrg, g.org)
	e.Set(RequestEventKeyOrgs, g.orgs)
	e.Set(RequestEventKeyOrgAdmin, g.orgAdmin)
	e.Request.Header.Set(claims.HeaderUserID, g.sub)
	e.Request.Header.Set(claims.HeaderOrgID, g.org)
}

// grants are the unspent grants of the process.
//
// A stream is one connection to one process, and which Base serves it is what
// the grant names, so the table cannot belong to any one Base.
type grants struct {
	mu sync.Mutex
	m  map[string]*grant
}

var streamGrants = &grants{m: map[string]*grant{}}

// mint records g and returns the string that spends it.
func (t *grants) mint(g *grant) string {
	id := security.RandomString(40)

	t.mu.Lock()
	defer t.mu.Unlock()

	// Drop what was minted and never spent. The table only ever holds the last
	// half minute of grants, so this is the whole of its housekeeping.
	now := time.Now()
	for k, old := range t.m {
		if now.After(old.deadline) {
			delete(t.m, k)
		}
	}

	t.m[id] = g

	return id
}

// spend removes a grant and returns it, or nil if it was never minted, has
// already been spent, or is past its deadline.
func (t *grants) spend(id string) *grant {
	t.mu.Lock()
	defer t.mu.Unlock()

	g, ok := t.m[id]
	if !ok {
		return nil
	}
	delete(t.m, id)

	if time.Now().After(g.deadline) {
		return nil
	}

	return g
}

// realtimeGrant mints the grant a stream is opened with.
//
// It answers on an ordinary authenticated request, so the caller presents its
// credential the way it presents it everywhere else, in a header.
func realtimeGrant(e *core.RequestEvent) error {
	sub, _ := e.Get(RequestEventKeySub).(string)
	org, _ := e.Get(RequestEventKeyOrg).(string)
	orgs, _ := e.Get(RequestEventKeyOrgs).([]string)
	orgAdmin, _ := e.Get(RequestEventKeyOrgAdmin).(bool)

	id := streamGrants.mint(&grant{
		app:      e.App,
		auth:     e.Auth,
		sub:      sub,
		org:      org,
		orgs:     orgs,
		orgAdmin: orgAdmin,
		deadline: time.Now().Add(grantTTL),
	})

	return e.JSON(http.StatusOK, map[string]string{"token": id})
}

// spendGrant authenticates a stream from the grant in its query.
//
// It is bound at the one address a grant opens and nowhere else, so what a grant
// reaches is a property of the route rather than of a string that travels in a
// URL. A request carrying no grant is an anonymous subscriber and is served by
// the Base the process runs on, which is what an anonymous caller reads anyway.
func spendGrant() *hook.Handler[*core.RequestEvent] {
	return &hook.Handler[*core.RequestEvent]{
		Id: grantMiddlewareId,
		// After the credential middlewares, so a request that can present a
		// header presents one and this never comes up.
		Priority: DefaultLoadAuthTokenMiddlewarePriority + 2,
		Func: func(e *core.RequestEvent) error {
			if e.Auth != nil {
				return e.Next()
			}

			id := e.Request.URL.Query().Get("token")
			if id == "" {
				return e.Next()
			}

			g := streamGrants.spend(id)
			if g == nil {
				return e.UnauthorizedError("The stream grant is spent, expired or unknown.", nil)
			}

			g.apply(e)

			return e.Next()
		},
	}
}
