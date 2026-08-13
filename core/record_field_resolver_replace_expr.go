package core

import (
	"strings"

	"github.com/hanzoai/dbx"
)

var _ dbx.Expression = (*replaceWithExpression)(nil)

// replaceWithExpression defines a custom expression that will replace
// a placeholder identifier found in "old" with the result of "new".
type replaceWithExpression struct {
	placeholder string
	old         dbx.Expression
	new         dbx.Expression
}

// Build converts the expression into a SQL fragment.
//
// What this returns lands wherever the placeholder sat, which is inside a
// comparison and not the whole predicate, so it has no way to spell "no rows":
// a fragment means whatever the SQL around it makes it mean. Every part is
// therefore settled where the expression is constructed — the placeholder is a
// literal, the replacement is built and its error returned, and "old" is the
// expression [search.ResolverResult.AfterBuild] is handed.
//
// Implements [dbx.Expression] interface.
func (e *replaceWithExpression) Build(db *dbx.DB, params dbx.Params) string {
	return strings.ReplaceAll(e.old.Build(db, params), e.placeholder, e.new.Build(db, params))
}
