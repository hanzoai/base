package tests

import (
	"database/sql"
	"fmt"
	"os"
	"strings"
	"sync/atomic"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// BASE_TEST_POSTGRES names a PostgreSQL server for the suite to run against.
// Unset — the ordinary case, and what `go test ./...` does — every test app
// opens the embedded SQLite file exactly as before.
//
// It is read HERE and nowhere else. Where a Base keeps its data is the host's
// question, answered by core.BaseAppConfig.DataDSN, and base's own startup path
// reads no environment to decide it. This is the test harness answering that
// question for itself, which is a different thing from Base answering it.
//
// The value is a DSN for a server, not for one database — the harness carves
// its own space out of it:
//
//	BASE_TEST_POSTGRES='postgres://postgres:pw@127.0.0.1:5432/postgres?sslmode=disable'
var postgres = os.Getenv("BASE_TEST_POSTGRES")

// Postgres reports whether the suite is running against a PostgreSQL server.
// A test that asserts a spelling only one engine has consults it; a test that
// asserts an ANSWER should not need to.
func Postgres() bool { return postgres != "" }

// schemas counts the schemas handed out by this process. Two processes test
// concurrently — `go test ./...` builds one binary per package and runs them in
// parallel — so the pid joins the count to keep the names apart.
var schemas atomic.Int64

// postgresSchema carves a fresh, empty schema out of the server and returns a
// DSN pointed at it, plus the release.
//
// A schema rather than a database because it is three orders of magnitude
// cheaper — measured at ~1ms against ~85ms for the fastest CREATE DATABASE, and
// the suite asks for hundreds. It isolates as completely for this purpose: the
// dialect reads the catalog through current_schema(), so an app sees its own
// tables and no others.
//
// A failure here is fatal rather than a fallback to SQLite. Being told to run
// on PostgreSQL and quietly running on a file is how a green gate comes to mean
// nothing.
func postgresSchema() (string, func()) {
	if postgres == "" {
		return "", nil
	}

	name := fmt.Sprintf("hz_test_%d_%d", os.Getpid(), schemas.Add(1))

	if err := exec("CREATE SCHEMA " + name); err != nil {
		panic(fmt.Errorf("tests: creating schema %s: %w", name, err))
	}

	sep := "?"
	if strings.Contains(postgres, "?") {
		sep = "&"
	}

	return postgres + sep + "search_path=" + name, func() {
		// Best effort: the schema is gone with the server when CI ends, and a
		// test that has already reported should not fail on the way out.
		_ = exec("DROP SCHEMA " + name + " CASCADE")
	}
}

func exec(query string) error {
	db, err := sql.Open("pgx", postgres)
	if err != nil {
		return err
	}
	defer db.Close()

	_, err = db.Exec(query)

	return err
}
