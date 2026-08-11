package search

import (
	"fmt"
	"strings"

	"github.com/hanzoai/orm/query"
)

var _ query.Expression = (*MultiMatchSubquery)(nil)

// Join defines common fields required for a single SQL JOIN clause.
type Join struct {
	TableName  string
	TableAlias string
	On         query.Expression
}

// Condition is what the join is made on. A join whose relation is a function
// of the row it is joined to has nothing of its own to match, and an engine
// that requires an outer join to state a condition still has to be given one.
func (j *Join) Condition() query.Expression {
	if j.On != nil {
		return j.On
	}

	return query.NewExp("1=1")
}

// MultiMatchSubquery defines a multi-match record subquery expression.
type MultiMatchSubquery struct {
	TargetTableAlias string
	FromTableName    string
	FromTableAlias   string
	ValueIdentifier  string
	Joins            []*Join
	Params           query.Params
}

// Build converts the expression into a SQL fragment.
//
// Implements [query.Expression] interface.
func (m *MultiMatchSubquery) Build(db *query.DB, params query.Params) string {
	if m.TargetTableAlias == "" || m.FromTableName == "" || m.FromTableAlias == "" {
		return "0=1"
	}

	if params == nil {
		params = m.Params
	} else {
		// merge by updating the parent params
		for k, v := range m.Params {
			params[k] = v
		}
	}

	var mergedJoins strings.Builder
	for i, j := range m.Joins {
		if i > 0 {
			mergedJoins.WriteString(" ")
		}
		mergedJoins.WriteString("LEFT JOIN ")
		mergedJoins.WriteString(db.QuoteTableName(j.TableName))
		mergedJoins.WriteString(" ")
		mergedJoins.WriteString(db.QuoteTableName(j.TableAlias))
		mergedJoins.WriteString(" ON ")
		mergedJoins.WriteString(j.Condition().Build(db, params))
	}

	return fmt.Sprintf(
		`SELECT %s as [[multiMatchValue]] FROM %s %s %s WHERE %s = %s`,
		db.QuoteColumnName(m.ValueIdentifier),
		db.QuoteTableName(m.FromTableName),
		db.QuoteTableName(m.FromTableAlias),
		mergedJoins.String(),
		db.QuoteColumnName(m.FromTableAlias+".id"),
		db.QuoteColumnName(m.TargetTableAlias+".id"),
	)
}
