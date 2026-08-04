package core

import (
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/hanzoai/dbx"
)

// Base's search layer emits SQL that calls acos, cos, sin and radians — see the
// geoDistance token function in tools/search. SQLite only has those when it was
// compiled with SQLITE_ENABLE_MATH_FUNCTIONS, so a build can link an engine
// whose SQL surface is smaller than the one the query builder writes against,
// and nothing complains until someone filters by geoDistance and gets
// "no such function: acos" from an endpoint that works elsewhere.
//
// Which builds are deficient is a property of the linked engine, not of any
// build tag. Measured on this tree:
//
//	CGO_ENABLED=0                              present  (pure-Go modernc; what the Dockerfile ships)
//	CGO_ENABLED=1                              ABSENT   <- the one deficient config
//	CGO_ENABLED=1 -tags sqlite_math_functions  present  (csqlite compiles the amalgamation with -DSQLITE_ENABLE_MATH_FUNCTIONS)
//	CGO_ENABLED=1 -tags libsqlite3             present  (links the system sqlcipher, which already has them)
//
// This was previously a compile-time guard that failed any cgo build not
// carrying the sqlite_math_functions tag. It tested a proxy rather than the
// property: it rejected -tags libsqlite3 builds whose SQL surface is complete
// (row four — that tag selects the system library, so the amalgamation's -D
// flag is never compiled), it could never catch a system libsqlcipher that
// genuinely lacked the functions, and no library can satisfy it from the
// inside, because build tags are chosen by the top-level build command. Its
// only mechanism was an undefined symbol, so every consumer of this package
// lost `go build ./...` to an error that named a symbol rather than the
// problem.
//
// So ask the engine instead of the build.

// errNoSQLiteMathFunctions is returned by VerifySQLiteMathFunctions when the
// linked SQLite is measurably missing the math functions.
var errNoSQLiteMathFunctions = errors.New("base: the SQLite engine linked into this binary has no math functions")

// mathProbeSQL is the shape tools/search emits for geoDistance: every math
// function the filter builder can produce, in one round trip.
const mathProbeSQL = `SELECT 6371 * acos(cos(radians(1)) * cos(radians(2)) * cos(radians(3) - radians(4)) + sin(radians(1)) * sin(radians(2)))`

var (
	mathOnce sync.Once
	mathErr  error
)

// VerifySQLiteMathFunctions reports whether the SQLite engine linked into this
// binary implements the math functions the search layer's geoDistance filter
// emits. It answers from a throwaway in-memory database, so a caller's own
// connection is neither opened nor kept warm on its behalf, and it measures
// once per process — the answer is a property of the linked engine, not of any
// database file.
//
// A definitive "no such function" is an error: a capability the query builder
// assumes is not something to discover from a customer's failed search. A probe
// that could not run at all — no "sqlite" driver registered under the
// no_default_driver tag, or an environment that refuses an in-memory database —
// is not: this reports a measured absence, never a failure to measure.
func VerifySQLiteMathFunctions() error {
	mathOnce.Do(func() { mathErr = probeSQLiteMathFunctions() })
	return mathErr
}

func probeSQLiteMathFunctions() error {
	db, err := dbx.Open("sqlite", "file:base_sqlite_math_probe?mode=memory")
	if err != nil {
		return nil // inconclusive
	}
	defer db.Close()

	var distance float64
	if err := db.NewQuery(mathProbeSQL).Row(&distance); err != nil {
		// Both backends phrase a missing scalar function this way; anything
		// else is a probe that failed for reasons of its own.
		if strings.Contains(err.Error(), "no such function") {
			return fmt.Errorf(
				"%w (%v) — search filters using geoDistance would fail at query time. "+
					"Rebuild with CGO_ENABLED=0 (how Base ships), or with -tags sqlite_math_functions, "+
					"or with -tags libsqlite3 to link a system SQLite that already has them",
				errNoSQLiteMathFunctions, err,
			)
		}
		return nil // inconclusive
	}

	return nil
}
