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
		if j.On != nil {
			mergedJoins.WriteString(" ON ")
			mergedJoins.WriteString(j.On.Build(db, params))
		}
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
