//go:build cgo && !sqlite_math_functions

package core

// Base's search layer generates SQL that calls acos, cos, sin, radians and
// sqrt — see the geoDistance token function in tools/search. Those are SQLite
// math functions, and SQLite only has them when compiled with
// SQLITE_ENABLE_MATH_FUNCTIONS.
//
// The pure-Go backend (CGO_ENABLED=0, modernc) always has them, and that is
// what the Dockerfile ships, so production is fine. The cgo backend gets them
// only behind csqlite's `sqlite_math_functions` build tag — so a cgo build
// silently produces a binary whose SQL surface is SMALLER than the one the
// code above it writes against. Nothing complains until someone filters by
// geoDistance and gets "no such function: acos" from an endpoint that works in
// production.
//
// Two build modes that answer differently is the bug; this makes them answer
// the same or not build at all. Fix by adding the tag:
//
//	go build -tags sqlite_math_functions ./...
//	go test  -tags sqlite_math_functions ./...
//
// or by building the way the product ships, CGO_ENABLED=0.
//
// Deliberately a compile error rather than a runtime check: a capability the
// query builder assumes is not something to discover from a customer's failed
// search.
func init() {
	_ = cgoBuildNeedsSQLiteMathFunctions
}
