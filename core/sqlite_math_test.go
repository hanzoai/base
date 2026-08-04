package core

import (
	"testing"

	"github.com/hanzoai/dbx"
)

// engineHasMathFunctions asks the linked SQLite directly, with no help from the
// code under test, so the tests below have a ground truth to compare against.
func engineHasMathFunctions(t *testing.T) bool {
	t.Helper()

	db, err := dbx.Open("sqlite", "file:core_math_truth?mode=memory")
	if err != nil {
		// The probe treats this as inconclusive and so must the test, but a
		// build where it always happens has a guard that can never fire.
		t.Skipf("no usable sqlite driver registered: %v", err)
	}
	defer db.Close()

	var distance float64

	return db.NewQuery(mathProbeSQL).Row(&distance) == nil
}

// TestSQLiteMathFunctions fails a build whose linked SQLite cannot answer the
// search layer's geoDistance filter — the job the old compile-time guard in
// sqlite_math_required.go was doing, done by measuring the engine instead of
// asserting a build tag. It is reproduced here rather than left to
// TestTokenFunctionsGeoDistanceExec in tools/search because that package does
// not import core, so its failure says nothing about this one.
func TestSQLiteMathFunctions(t *testing.T) {
	if !engineHasMathFunctions(t) {
		t.Fatal("the SQLite engine linked into this build has no math functions; " +
			"build with CGO_ENABLED=0, -tags sqlite_math_functions, or -tags libsqlite3")
	}
}

// TestVerifySQLiteMathFunctionsAgreesWithEngine pins the probe to the engine.
// A probe that stopped working — a DSN that no longer opens, a driver error
// phrased some other way — would go on returning nil and quietly pass every
// deficient build, which is the one failure this file cannot afford.
func TestVerifySQLiteMathFunctionsAgreesWithEngine(t *testing.T) {
	present := engineHasMathFunctions(t)
	err := VerifySQLiteMathFunctions()

	switch {
	case present && err != nil:
		t.Fatalf("engine has the math functions but the probe rejected the build: %v", err)
	case !present && err == nil:
		t.Fatal("engine has no math functions but the probe passed the build")
	}
}

// TestSQLiteMathProbeIsConclusive guards the escape hatch. VerifySQLiteMathFunctions
// declines to fail a build it could not measure, so a probe that never manages
// to open its database is indistinguishable from a healthy one. Here it must open.
func TestSQLiteMathProbeIsConclusive(t *testing.T) {
	db, err := dbx.Open("sqlite", "file:base_sqlite_math_probe?mode=memory")
	if err != nil {
		t.Fatalf("the probe's own DSN does not open, so the probe can only ever return inconclusive: %v", err)
	}
	defer db.Close()

	var distance float64
	if err := db.NewQuery(mathProbeSQL).Row(&distance); err == nil && distance <= 0 {
		t.Fatalf("expected a positive distance from the probe expression, got %v", distance)
	}
}
