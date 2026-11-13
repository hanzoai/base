package core_test

import (
	"strings"

	"github.com/hanzoai/base/core"
)

// engineType spells a column type the way the app's engine does: the suite
// writes SQLite's, and PostgreSQL keeps a document in the binary form.
func engineType(app core.App, sqlite string) string {
	if core.LocalDatabase(app.Dialect()) {
		return sqlite
	}
	if strings.HasPrefix(sqlite, "JSON ") {
		return "JSONB " + strings.TrimPrefix(sqlite, "JSON ")
	}
	return sqlite
}

// typedRefusal reports whether err is a server engine refusing a filter whose
// operands are not of one type (a text id against a number, a boolean, a length):
// SQLite compares them and answers false, a typed engine refuses. The filter that
// is refused returns no rows, which is the answer a refused filter always is.
func typedRefusal(err error) bool {
	msg := err.Error()

	return strings.Contains(msg, "SQLSTATE 42883") ||
		strings.Contains(msg, "SQLSTATE 22P02") ||
		strings.Contains(msg, "SQLSTATE 42804") ||
		strings.Contains(msg, "failed to encode")
}
