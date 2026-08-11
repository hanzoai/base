package apis

import (
	"net/url"
	"strings"
	"testing"
)

// A wire query becomes Base's own, exactly. These are the shapes the console
// grid actually sends, so a regression here is a broken table view.
//
// Note what is NOT here any more: an assertion that a filter renders to a STRING.
// It used to, and the string it rendered ({:p0} plus a sibling param) was never
// substituted by anything, so every filtered read answered 400 while this test
// stayed green. A predicate is data now, and the test that proves it works is the
// HTTP one that filters a real collection and gets rows back.
func TestRestQueryTranslates(t *testing.T) {
	for _, s := range []struct {
		name    string
		in      string
		counted bool
		want    map[string]string
	}{{
		name: "select becomes fields",
		in:   "select=id,name",
		want: map[string]string{"fields": "id,name", "skipTotal": "1"},
	}, {
		name: "select=* asks for everything, which is the default",
		in:   "select=*",
		want: map[string]string{"skipTotal": "1"},
	}, {
		name: "a bare order term is ascending",
		in:   "order=created",
		want: map[string]string{"sort": "+created", "skipTotal": "1"},
	}, {
		name: "descending, and several terms keep their order",
		in:   "order=created.desc,name.asc",
		want: map[string]string{"sort": "-created,+name", "skipTotal": "1"},
	}, {
		name: "limit and a page-aligned offset become page/perPage",
		in:   "limit=25&offset=50",
		want: map[string]string{"perPage": "25", "page": "3", "skipTotal": "1"},
	}, {
		name:    "asking for a count drops skipTotal",
		in:      "limit=10",
		counted: true,
		want:    map[string]string{"perPage": "10", "page": "1"},
	}, {
		// A predicate never reaches the query string at all — it travels as data.
		name: "a predicate leaves no trace in the query params",
		in:   "status=eq.active",
		want: map[string]string{"skipTotal": "1"},
	}} {
		t.Run(s.name, func(t *testing.T) {
			in, err := url.ParseQuery(s.in)
			if err != nil {
				t.Fatalf("bad test input: %v", err)
			}
			got, _, err := restQuery(in, s.counted)
			if err != nil {
				t.Fatalf("restQuery(%q) errored: %v", s.in, err)
			}
			if len(got) != len(s.want) {
				t.Fatalf("restQuery(%q) = %v, want %v", s.in, got, s.want)
			}
			for k, want := range s.want {
				if got.Get(k) != want {
					t.Errorf("restQuery(%q)[%s] = %q, want %q", s.in, k, got.Get(k), want)
				}
			}
		})
	}
}

// The predicates a query carries, as data.
func TestRestPredicates(t *testing.T) {
	for _, s := range []struct {
		name string
		in   string
		want []restPredicate
	}{{
		name: "one comparison",
		in:   "status=eq.active",
		want: []restPredicate{{Col: "status", Op: "eq", Vals: []string{"active"}}},
	}, {
		name: "several, in column order so one request is one expression",
		in:   "status=eq.active&age=gte.18",
		want: []restPredicate{
			{Col: "age", Op: "gte", Vals: []string{"18"}},
			{Col: "status", Op: "eq", Vals: []string{"active"}},
		},
	}, {
		name: "in carries every value",
		in:   "id=in.(a,b,c)",
		want: []restPredicate{{Col: "id", Op: "in", Vals: []string{"a", "b", "c"}}},
	}, {
		name: "the wildcard this wire spells with a star becomes SQL's",
		in:   "name=like.*ace*",
		want: []restPredicate{{Col: "name", Op: "like", Vals: []string{"%ace%"}}},
	}, {
		name: "select and paging are not predicates",
		in:   "select=id&limit=5&offset=0&order=id",
		want: []restPredicate{},
	}} {
		t.Run(s.name, func(t *testing.T) {
			in, _ := url.ParseQuery(s.in)
			got, err := restPredicates(in)
			if err != nil {
				t.Fatalf("restPredicates(%q) errored: %v", s.in, err)
			}
			if len(got) != len(s.want) {
				t.Fatalf("restPredicates(%q) = %+v, want %+v", s.in, got, s.want)
			}
			for i := range got {
				if got[i].Col != s.want[i].Col || got[i].Op != s.want[i].Op ||
					strings.Join(got[i].Vals, ",") != strings.Join(s.want[i].Vals, ",") {
					t.Errorf("predicate %d = %+v, want %+v", i, got[i], s.want[i])
				}
			}
		})
	}
}

// A window or an operator this store cannot honour is REFUSED. Rounding an
// unaligned offset would answer with real rows for the wrong range, which is
// indistinguishable from correct data at the call site.
func TestRestQueryRefusesWhatItCannotHonour(t *testing.T) {
	for _, s := range []struct{ name, in, wants string }{
		{"offset off a page boundary", "limit=25&offset=30", "page boundary"},
		{"offset with no limit", "offset=30", "needs a limit"},
		{"a negative limit", "limit=-1", "non-negative"},
		{"an unparseable limit", "limit=many", "non-negative"},
		{"an unknown operator", "status=matches.x", "unknown operator"},
		{"a predicate with no operator", "status=active", "needs an operator"},
		{"is with something that is not a literal", "x=is.7", "null, true or false"},
		{"an order direction this store cannot express", "order=a.desc.nullsfirst", "asc and desc"},
	} {
		t.Run(s.name, func(t *testing.T) {
			in, _ := url.ParseQuery(s.in)
			_, _, err := restQuery(in, false)
			if err == nil {
				t.Fatalf("restQuery(%q) was accepted; it should be refused", s.in)
			}
			if !strings.Contains(err.Error(), s.wants) {
				t.Errorf("restQuery(%q) said %q, which does not mention %q — the caller has to be able to act on it",
					s.in, err.Error(), s.wants)
			}
		})
	}
}

// The predicate order is stable for one request. url.Values iterates randomly,
// so without an explicit order the same query yields a different expression run
// to run, which cannot be reproduced from a log.
func TestRestPredicatesAreStableAcrossRuns(t *testing.T) {
	in, _ := url.ParseQuery("b=eq.2&a=eq.1&c=eq.3")
	first, err := restPredicates(in)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 50; i++ {
		got, err := restPredicates(in)
		if err != nil {
			t.Fatal(err)
		}
		for j := range got {
			if got[j].Col != first[j].Col {
				t.Fatalf("predicate order changed between runs: %v then %v", first, got)
			}
		}
	}
	if first[0].Col != "a" {
		t.Errorf("predicates do not lead with the first column by name: %+v", first)
	}
}

// A caller's value stays a VALUE. It is never rendered into an expression, which
// is what makes the door safe to expose — restWhere binds it as a parameter and
// the column is resolved to an identifier before it can reach SQL.
func TestRestPredicateCarriesTheValueVerbatim(t *testing.T) {
	hostile := `x' || 1=1 || '`
	in, _ := url.ParseQuery("name=eq." + url.QueryEscape(hostile))

	preds, err := restPredicates(in)
	if err != nil {
		t.Fatal(err)
	}
	if len(preds) != 1 {
		t.Fatalf("want one predicate, got %+v", preds)
	}
	if preds[0].Vals[0] != hostile {
		t.Fatalf("the value should travel verbatim as data, got %q", preds[0].Vals[0])
	}
	if strings.Contains(preds[0].Col, "1=1") {
		t.Fatalf("the value leaked into the column: %q", preds[0].Col)
	}
}
