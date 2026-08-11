package apis

import (
	"net/url"
	"strings"
	"testing"
)

// A PostgREST query becomes Base's own, exactly. These are the shapes the Studio
// grid actually sends, so a regression here is a broken table view rather than a
// style nit.
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
		name: "the first page needs no offset",
		in:   "limit=10",
		want: map[string]string{"perPage": "10", "page": "1", "skipTotal": "1"},
	}, {
		name:    "asking for a count drops skipTotal",
		in:      "limit=10",
		counted: true,
		want:    map[string]string{"perPage": "10", "page": "1"},
	}, {
		name: "a column predicate crosses as a placeholder, never as text",
		in:   "status=eq.active",
		want: map[string]string{"filter": "status={:p0}", "p0": "active", "skipTotal": "1"},
	}, {
		name: "several predicates conjoin",
		in:   "status=eq.active&age=gte.18",
		want: map[string]string{"filter": "age>={:p0} && status={:p1}", "p0": "18", "p1": "active", "skipTotal": "1"},
	}, {
		name: "is.null is a literal, because a quoted null matches nothing",
		in:   "deleted=is.null",
		want: map[string]string{"filter": "deleted=null", "skipTotal": "1"},
	}, {
		name: "in expands to a disjunction, one placeholder per value",
		in:   "id=in.(a,b)",
		want: map[string]string{"filter": "(id={:p0} || id={:p1})", "p0": "a", "p1": "b", "skipTotal": "1"},
	}, {
		name: "like translates the wildcard PostgREST spells with a star",
		in:   "name=like.*ace*",
		want: map[string]string{"filter": "name~{:p0}", "p0": "%ace%", "skipTotal": "1"},
	}} {
		t.Run(s.name, func(t *testing.T) {
			in, err := url.ParseQuery(s.in)
			if err != nil {
				t.Fatalf("bad test input: %v", err)
			}
			got, err := restQuery(in, s.counted)
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

// A window this store cannot express is REFUSED. Rounding an unaligned offset to
// the nearest page would answer with real rows for the wrong range, which is
// indistinguishable from correct data at the call site.
func TestRestQueryRefusesAWindowItCannotHonour(t *testing.T) {
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
			_, err := restQuery(in, false)
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

// The filter string must be stable for one request. url.Values iterates randomly,
// so without an explicit order the same query yields different expressions run to
// run — which defeats the parsed-filter cache and makes a failure unreproducible.
func TestRestFilterIsStableAcrossRuns(t *testing.T) {
	in, _ := url.ParseQuery("b=eq.2&a=eq.1&c=eq.3")
	first, params, err := restFilter(in)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 50; i++ {
		got, gotParams, err := restFilter(in)
		if err != nil {
			t.Fatal(err)
		}
		if got != first {
			t.Fatalf("filter changed between runs: %q then %q", first, got)
		}
		if len(gotParams) != len(params) {
			t.Fatalf("params changed between runs: %v then %v", params, gotParams)
		}
	}
	// and the column order is the sorted one, so the expression is predictable
	if !strings.HasPrefix(first, "a=") {
		t.Errorf("filter %q does not lead with the first column by name", first)
	}
}

// A caller's value never reaches the expression text. The filter DSL is an
// expression language, so a value spliced into it is injection into the WHERE
// clause — this is the property that makes the door safe to expose.
func TestRestFilterNeverSplicesACallersValue(t *testing.T) {
	hostile := `x' || 1=1 || '`
	in, _ := url.ParseQuery("name=eq." + url.QueryEscape(hostile))

	filter, params, err := restFilter(in)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(filter, "1=1") || strings.Contains(filter, hostile) {
		t.Fatalf("the value reached the expression: %q", filter)
	}
	if filter != "name={:p0}" {
		t.Fatalf("filter = %q, want a bare placeholder", filter)
	}
	if params["p0"] != hostile {
		t.Fatalf("the value should be carried as a param verbatim, got %q", params["p0"])
	}
}
