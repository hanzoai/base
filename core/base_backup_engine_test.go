package core

import (
	"strings"
	"testing"

	"github.com/hanzoai/orm/dialect"
)

// A backup is an archive of the data directory, so it is a backup of the
// database only while the engine keeps the database there. An engine that keeps
// it in the server says so, and names itself, rather than writing an archive
// that would restore nothing.
func TestCheckLocalDatabase(t *testing.T) {
	t.Parallel()

	if err := checkLocalDatabase(dialect.SQLite{}); err != nil {
		t.Fatalf("Expected sqlite to archive its own database, got %v", err)
	}

	err := checkLocalDatabase(dialect.Postgres{})
	if err == nil {
		t.Fatal("Expected postgres to refuse, got nil")
	}

	if !strings.Contains(err.Error(), "postgres") {
		t.Fatalf("Expected the refusal to name the engine, got %q", err)
	}
}
