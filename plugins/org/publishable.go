package org

import (
	"net/http"
	"slices"

	"github.com/hanzoai/base/apis"
	"github.com/hanzoai/base/core"
)

// publishableReachesOnlyPublic refuses a publishable key every address that is
// not public.
//
// The refusal is decided by what the CREDENTIAL is, and what it may reach is
// decided by a declaration each route carries. A rule that instead asks whether
// some other rule recognised the address answers "no opinion" for every address
// that rule does not cover, which is every address in the process bar one
// subtree — so a key printed in a web page reached a database's connection
// string by being pointed somewhere the Base rule had nothing to say about.
// Naming what is public is a list that can be read; naming what is private is a
// list that is never finished.
func publishableReachesOnlyPublic(e *core.RequestEvent) error {
	if publishable(e.Request) && !public(e) {
		return e.ForbiddenError("A publishable key is public and reaches only what is public.", nil)
	}

	return e.Next()
}

// publishable reports whether a request presents a key printed in a web page —
// in ANY channel it could have arrived on, not only the one a door resolves.
//
// It reads the request rather than what a door resolved from it. The doors do
// not run at every address — /v1/iam unbinds them so the proxy hands IAM the
// caller's own credential — and what a credential reaches has to be answerable
// where nothing resolved it.
//
// Every channel, because the proxies forward every channel. Asking only which
// credential a door would resolve is a question a decoy walks past: a bare
// `nonsense` in Authorization is a token by any reading, so it was judged, found
// harmless and admitted, while the pk- beside it under X-API-Key — or in a
// second Authorization value, or a second `key` parameter — went on to the
// issuer. Nothing is dropped here and no other credential is disturbed; a
// request presenting a publishable key is simply held to what a publishable key
// reaches.
func publishable(r *http.Request) bool {
	return slices.ContainsFunc(apis.Credentials(r), IsPublishableKey)
}

// public reports whether the address a request reached is one a publishable key
// may reach.
//
// There are two kinds of answer and they are different in kind. Nearly every
// address is answered by DECLARATION: the product that registers a route says
// so beside it, and apis.Public holds the whole of that. The rule here holds
// none of those addresses, because a rule holding another package's routes is a
// copy that goes stale the moment that package mounts one — which is how a
// dozen reads a publishable key is meant to make came to answer 403.
//
// The one address answered by DATA is the public submit path — a contact form,
// a signup, a waitlist. Whether it is public is the collection's statement and
// not the route's: the collection says who may create in it, and a collection
// open to anyone is open to a page. Declaring the route would say yes for every
// collection.
//
// Everything else is refused: an org's Bases and its provider credentials, its
// databases and their connection strings, IAM, the superuser surface, a backup,
// and whatever an extension or a JS hook registered at an address nobody here
// has read. Naming what is public is a list that can be read; naming what is
// private is a list that is never finished.
func public(e *core.RequestEvent) bool {
	if name := apis.CreatesRecord(e.Request); name != "" {
		c, err := e.App.FindCachedCollectionByNameOrId(name)
		return err == nil && c.CreateRule != nil && *c.CreateRule == ""
	}

	return apis.IsPublic(e.Request)
}
