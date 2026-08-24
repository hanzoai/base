package org

import (
	"net/http"
	"strings"

	"github.com/hanzoai/base/apis"
	"github.com/hanzoai/base/core"
)

// publishableReachesOnlyPublic refuses a publishable key every address that is
// not public.
//
// The list of addresses it may reach is stated once, in public below, and the
// refusal is decided by what the CREDENTIAL is. A rule that instead asks
// whether some other rule recognised the address answers "no opinion" for every
// address that rule does not cover, which is every address in the process bar
// one subtree — so a key printed in a web page reached a database's connection
// string by being pointed somewhere the Base rule had nothing to say about.
// Naming what is public is a list that can be read; naming what is private is a
// list that is never finished.
func publishableReachesOnlyPublic(e *core.RequestEvent) error {
	if publishable(e.Request) && !public(e) {
		return e.ForbiddenError("A publishable key is public and reaches only what is public.", nil)
	}

	return e.Next()
}

// publishable reports whether the credential a request carries is a key printed
// in a web page.
//
// It reads the request rather than what a door resolved from it. The doors do
// not run at every address — /v1/iam unbinds them so the proxy hands IAM the
// caller's own credential — and what a credential reaches has to be answerable
// where nothing resolved it.
func publishable(r *http.Request) bool {
	return IsPublishableKey(credential(r))
}

// credential is the key a request carries: the bearer token, or the `key` query
// parameter. A publishable key travels in the query — that is what makes the
// page source the credential — so both spellings are read in one place, and a
// rule reading one while a door reads the other cannot disagree about what
// arrived.
func credential(r *http.Request) string {
	if token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer "); ok && token != "" {
		return token
	}

	return r.URL.Query().Get("key")
}

// public is every address a publishable key reaches, and the whole of it.
//
// Each one answers on the Base's own collection rules: the collection decides
// what a read hands back and who may create, and the key only says which org's
// Base to ask. That is the arrangement a publishable key is for, and it is the
// only arrangement it is admitted to. Everything else is refused — an org's
// Bases and its provider credentials, its databases and their connection
// strings, IAM, the superuser surface, a backup, and whatever an extension or a
// JS hook registered at an address nobody here has read.
//
// Addresses are compared as WHOLE patterns, method and this deployment's mount
// prefix included, the way apis.CreatesRecord names the one route it grants. A
// pattern that merely ends the same way is a different route, and an extension
// registers whatever path it likes.
func public(e *core.RequestEvent) bool {
	// The public submit path — a contact form, a signup, a waitlist. The
	// collection states who may create in it, and a collection open to anyone
	// is open to a page.
	if name := apis.CreatesRecord(e.Request); name != "" {
		c, err := e.App.FindCachedCollectionByNameOrId(name)
		return err == nil && c.CreateRule != nil && *c.CreateRule == ""
	}

	p := apis.Prefix()

	switch e.Request.Pattern {
	// The rows a page renders. listRule and viewRule say which rows, and a
	// collection that states no rule is reachable by a superuser alone.
	case http.MethodGet + " " + p + "/collections/{collection}/records",
		http.MethodGet + " " + p + "/collections/{collection}/records/{id}":
		return true

	// The same rows through the REST wire: the same handler, deciding on the
	// same listRule. One credential, one answer, whichever wire asked.
	case http.MethodGet + " " + p + "/rest/{collection}",
		http.MethodHead + " " + p + "/rest/{collection}":
		return true

	// The files those rows name. A protected field re-reads viewRule here.
	case http.MethodGet + " " + p + "/files/{collection}/{recordId}/{filename}":
		return true

	// Liveness, which answers a request carrying no credential at all, so a key
	// widens nothing.
	case http.MethodGet + " " + p + "/health",
		http.MethodGet + " /healthz":
		return true
	}

	return false
}
