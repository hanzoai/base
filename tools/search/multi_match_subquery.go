package search

import (
	"errors"
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

// usable reports whether the subquery names everything a SELECT needs, or says
// what it is missing.
//
// A subquery is filled in field by field as a field path is walked, so what it
// holds is settled only once the walk is done. That is still before it reaches
// an expression, which is why the shortfall is answerable here and not in Build.
func (m *MultiMatchSubquery) usable() error {
	switch {
	case m == nil:
		return errors.New("no multi-match subquery")
	case m.TargetTableAlias == "":
		return errors.New("multi-match subquery names no target table")
	case m.FromTableName == "":
		return errors.New("multi-match subquery names no source table")
	case m.FromTableAlias == "":
		return errors.New("multi-match subquery names no source table alias")
	case m.ValueIdentifier == "":
		return errors.New("multi-match subquery names no value")
	}

	return nil
}

// Build converts the expression into a SQL fragment.
//
// Implements [query.Expression] interface.
func (m *MultiMatchSubquery) Build(db *query.DB, params query.Params) string {
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
