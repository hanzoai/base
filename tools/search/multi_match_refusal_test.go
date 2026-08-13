package search_test

import (
	"database/sql"
	"strings"
	"testing"

	"github.com/hanzoai/base/tools/search"
	"github.com/hanzoai/orm/dialect"
	"github.com/hanzoai/orm/query"
)

// multiValued resolves every field to one identifier carrying the subquery it
// was handed, which is what a record resolver does for a multi-valued field.
type multiValued struct {
	sub *search.MultiMatchSubquery
}

func (r *multiValued) Dialect() dialect.Dialect { return dialect.For("sqlite") }

func (r *multiValued) UpdateQuery(*query.SelectQuery) error { return nil }

func (r *multiValued) Resolve(string) (*search.ResolverResult, error) {
	return &search.ResolverResult{
		Identifier:         "[[demo.values]]",
		MultiMatchSubQuery: r.sub,
	}, nil
}

// TestMultiMatchRefusesWhatItCannotBuild pins that a multi-match it cannot
// assemble is refused rather than stood in for.
//
// A multi-match states "every value matches" as "no value fails", so it wraps
// an operand in NOT and the pair in NOT EXISTS. A stand-in for "matches
// nothing" is the literal 0=1, and 0=1 means the opposite of itself once
// something negates it — so where a fragment can be negated, there is nothing
// it can be replaced BY. The subquery is filled in field by field as a field
// path is walked and it is complete before it reaches an expression, so what it
// is missing is answerable as an error, and an error is a refused filter.
func TestMultiMatchRefusesWhatItCannotBuild(t *testing.T) {
	sqlDB, err := sql.Open("sqlite", "file::memory:?cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	db := query.NewFromDB(sqlDB, "sqlite")

	complete := search.MultiMatchSubquery{
		TargetTableAlias: "demo",
		FromTableName:    "demo",
		FromTableAlias:   "__mm_demo",
		ValueIdentifier:  "[[__mm_demo.values]]",
	}

	shortfalls := map[string]func(*search.MultiMatchSubquery){
		"no target table alias": func(m *search.MultiMatchSubquery) { m.TargetTableAlias = "" },
		"no source table":       func(m *search.MultiMatchSubquery) { m.FromTableName = "" },
		"no source table alias": func(m *search.MultiMatchSubquery) { m.FromTableAlias = "" },
		"no value":              func(m *search.MultiMatchSubquery) { m.ValueIdentifier = "" },
	}

	// The three arrangements of a multi-valued operand: on the left, on the
	// right, and on both, which are three different builders.
	filters := []search.FilterData{
		"a %s 'x'",
		"'x' %s a",
		"a %s b",
	}

	// The any-match operators deliberately build no multi-match — matching any
	// value is what the join alone already says — so the sweep is the eight
	// that do.
	operators := []string{"=", "!=", "~", "!~", "<", "<=", ">", ">="}

	for _, filter := range filters {
		for _, op := range operators {
			f := search.FilterData(strings.Replace(string(filter), "%s", op, 1))

			// control: a complete subquery builds, and says the empty set
			// nowhere.
			usable := complete
			expr, err := f.BuildExpr(&multiValued{sub: &usable})
			if err != nil {
				t.Fatalf("%q was refused with a complete subquery: %v", f, err)
			}
			if sql := expr.Build(db, query.Params{}); strings.Contains(sql, "0=1") {
				t.Fatalf("%q built a predicate carrying the empty set as a fragment:\n%s", f, sql)
			}

			for name, breakIt := range shortfalls {
				missing := complete
				breakIt(&missing)

				if _, err := f.BuildExpr(&multiValued{sub: &missing}); err == nil {
					t.Errorf("%q with a subquery that has %s was built rather than refused", f, name)
				}
			}

			if _, err := f.BuildExpr(&multiValued{sub: nil}); err != nil {
				t.Errorf("%q without any subquery is an ordinary comparison and should build: %v", f, err)
			}
		}
	}
}
