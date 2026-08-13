package core_test

import (
	"strings"
	"testing"

	"github.com/hanzoai/base/core"
	"github.com/hanzoai/base/tests"
	"github.com/hanzoai/base/tools/search"
)

// theGrammar is every sign operator fexpr scans. Four of them negate — "!=" and
// "!~" and their any-match twins — and they are why this sweep exists, but the
// whole set is swept because which operator reaches which builder is settled
// inside the compiler and not by the spelling.
var theGrammar = []string{
	"=", "!=", "~", "!~", "<", "<=", ">", ">=",
	"?=", "?!=", "?~", "?!~", "?<", "?<=", "?>", "?>=",
}

// aMultiValue is every operand shape that carries a multi-match subquery, which
// is every shape that builds a negation: the compiler wraps such an operand in
// NOT and the pair of them in NOT EXISTS, so "every value matches" is stated as
// "no value fails".
var aMultiValue = []struct{ collection, operand string }{
	{"demo5", "rel_many.id"},
	{"demo5", "rel_many.title"},
	{"demo5", "rel_many:each"},
	{"demo5", "rel_many:length"},
	{"demo5", "select_many:each"},
	{"demo5", "select_many:length"},
	{"demo5", "rel_many.self_rel_many.id"},
	{"demo5", "rel_many.json_array.0"},
	{"demo5", "rel_many.title:lower"},
	{"demo5", "strftime('%Y-%m', rel_many.created)"},
	{"demo5", "@collection.demo4.self_rel_many.id"},
	{"demo5", "@request.body.select_many:each"},
	{"demo5", "@request.body.select_many:length"},
	{"demo4", "self_rel_many.id"},
	{"demo4", "self_rel_many.self_rel_many.title"},
	{"demo4", "rel_many_cascade.id"},
	{"demo4", "demo5_via_rel_many.id"},
	{"demo4", "demo5_via_rel_many.rel_many.id"},
	{"demo4", "@request.body.self_rel_many:each"},
}

// theOtherSide is what a multi-valued operand gets compared against. The last
// entries are the ones that carry their own AfterBuild — an identity, and the
// ":changed" modifier, which substitutes its result INTO the comparison rather
// than replacing it — so the sweep covers a negation composed with each.
var theOtherSide = []string{
	`'optionA'`,
	`1`,
	`null`,
	`true`,
	`false`,
	`''`,
	`id`,
	`created`,
	`@request.auth.id`,
	`@request.auth.email`,
	`@request.body.id:changed`,
	`@request.body.select_many:length`,
}

// TestFilterStatesTheEmptySetNowhereItCanBeNegated sweeps the filter grammar
// for a fragment that means "no rows" sitting where the SQL around it inverts.
//
// "0=1" is false, which is what "matches nothing" has to be when it IS the
// predicate. Negated, the same three characters are every row. The multi-match
// forms negate — each wraps an operand in NOT and the pair in NOT EXISTS — and
// the ":changed" modifier substitutes its result into a comparison rather than
// replacing one, so a fragment that lands there lands inside something. The
// question is whether any filter a caller can write puts the one inside the
// other.
//
// Every operator is crossed with every operand shape that carries a multi-match
// subquery and with every kind of value on the other side, including the two
// that carry an AfterBuild of their own. Each case is built and then RUN, so a
// fragment that does not compose shows up as SQL the engine refuses rather than
// as a string that happened to match.
func TestFilterStatesTheEmptySetNowhereItCanBeNegated(t *testing.T) {
	t.Parallel()

	app, _ := tests.NewTestApp()
	defer app.Cleanup()

	// Signed in, so @request.auth.* resolves to a real join. The absent
	// identity has its own test below, because there the empty set IS the
	// whole predicate and saying so is the point.
	authRecord, err := app.FindRecordById("users", "4q1xlclmfloku33")
	if err != nil {
		t.Fatal(err)
	}

	requestInfo := &core.RequestInfo{
		Method: "PATCH",
		Auth:   authRecord,
		Body: map[string]any{
			"id":            "someIdValue",
			"title":         "someTitleValue",
			"select_many":   []string{"optionA", "optionC"},
			"self_rel_many": []string{"test1", "test2"},
		},
	}

	var built, ran int

	for _, many := range aMultiValue {
		collection, err := app.FindCollectionByNameOrId(many.collection)
		if err != nil {
			t.Fatal(err)
		}

		// A multi-valued operand on both sides is the many<->many form, which
		// is a different builder, so every shape is also compared to one.
		others := append([]string{}, theOtherSide...)
		for _, other := range aMultiValue {
			if other.collection == many.collection {
				others = append(others, other.operand)
			}
		}

		for _, op := range theGrammar {
			for _, other := range others {
				filter := many.operand + " " + op + " " + other

				resolver := core.NewRecordFieldResolver(app, collection, requestInfo, false)

				expr, err := search.FilterData(filter).BuildExpr(resolver)
				if err != nil {
					// A refused filter is an answer: it returns no rows at all.
					continue
				}

				q := app.RecordQuery(collection)
				if err := resolver.UpdateQuery(q); err != nil {
					continue
				}

				q.AndWhere(expr)
				built++

				sql := q.Build().SQL()
				if strings.Contains(sql, "0=1") {
					t.Errorf("%s in %s built a predicate carrying the empty set as a fragment:\n%s", filter, many.collection, sql)
					continue
				}

				records := []*core.Record{}
				if err := q.All(&records); err != nil {
					t.Errorf("%s in %s built SQL the engine refused: %v\n%s", filter, many.collection, err, sql)
					continue
				}
				ran++
			}
		}
	}

	// Guards the sweep itself: a corpus that stopped building anything would
	// pass every assertion above without having asked a question.
	if built < 2000 || ran != built {
		t.Fatalf("expected the sweep to build and run a few thousand filters, built %d and ran %d", built, ran)
	}

	t.Logf("built and ran %d filters", built)
}

// TestAbsentIdentityIsTheWholePredicate pins where the empty set IS allowed to
// be spelled, and that it is the whole predicate when it is.
//
// Nobody is signed in, so a comparison against that identity matches no row.
// The compiler says so by replacing the built comparison rather than by
// standing in for the identifier, which is what makes it hold for "!=" as well
// as "=" — and, here, for a comparison that also carries a multi-match, whose
// NOT EXISTS is discarded along with the rest.
func TestAbsentIdentityIsTheWholePredicate(t *testing.T) {
	t.Parallel()

	app, _ := tests.NewTestApp()
	defer app.Cleanup()

	collection, err := app.FindCollectionByNameOrId("demo5")
	if err != nil {
		t.Fatal(err)
	}

	for _, op := range theGrammar {
		for _, operand := range []string{"id", "rel_many.id", "rel_many:each", "select_many:length"} {
			filter := operand + " " + op + " @request.auth.id"

			resolver := core.NewRecordFieldResolver(app, collection, &core.RequestInfo{}, false)

			expr, err := search.FilterData(filter).BuildExpr(resolver)
			if err != nil {
				continue
			}

			q := app.RecordQuery(collection)
			if err := resolver.UpdateQuery(q); err != nil {
				t.Fatal(err)
			}

			sql := q.AndWhere(expr).Build().SQL()
			if !strings.HasSuffix(sql, " WHERE 0=1") {
				t.Errorf("%s resolved an absent identity to something other than the whole empty set:\n%s", filter, sql)
			}
		}
	}
}
