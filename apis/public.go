package apis

import (
	"net/http"
	"sync"

	"github.com/hanzoai/base/core"
	"github.com/hanzoai/base/tools/router"
)

// declared are the routes a publishable credential may reach. It starts empty:
// an address nobody declared is an address such a credential is refused.
var declared struct {
	sync.RWMutex
	routes []*router.Route[*core.RequestEvent]
}

// Public declares that a publishable credential — a key printed in a web page —
// may reach these routes. It is stated where the route is registered:
//
//	Public(g.GET("/chains", p.handleListChains))
//
// so that the product owning a route owns the answer about it, and the address
// is written once. The alternative was a list of another package's addresses
// kept in the rule: it went stale the moment a plugin mounted a route, which is
// how twelve reads meant to be reachable by such a key answered 403.
//
// The routes are read back by their mounted [router.Route.Pattern], which is
// assembled by the mux and is what a request reports having matched, so a
// pattern that merely ends the same way is a different route.
func Public(routes ...*router.Route[*core.RequestEvent]) {
	declared.Lock()
	declared.routes = append(declared.routes, routes...)
	declared.Unlock()
}

// IsPublic reports whether the route a request matched was declared [Public].
//
// The declaration is held per process rather than per app, because the answer
// is read where neither the router that mounted the route nor the plugin that
// declared it is in scope. A server builds one router, so the two are the same
// set; a process that built two routers over the same addresses would read one
// answer for both.
func IsPublic(r *http.Request) bool {
	if r.Pattern == "" {
		return false
	}

	declared.RLock()
	defer declared.RUnlock()

	for _, route := range declared.routes {
		if route.Pattern() == r.Pattern {
			return true
		}
	}

	return false
}
