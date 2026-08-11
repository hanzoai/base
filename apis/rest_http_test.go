package apis_test

import (
	"net/http"
	"testing"

	"github.com/hanzoai/base/tests"
)

// wantRange asserts the Content-Range a scenario should answer with. The count
// rides in the header on this wire, so a body assertion cannot see it.
func wantRange(want string) func(t testing.TB, app *tests.TestApp, res *http.Response) {
	return func(t testing.TB, app *tests.TestApp, res *http.Response) {
		if got := res.Header.Get("Content-Range"); got != want {
			t.Errorf("Content-Range = %q, want %q", got, want)
		}
	}
}

// The PostgREST door over real HTTP, against the same fixtures the collections
// door is tested on. The translation tests prove the query maps correctly; these
// prove the wire — the shape of the body, the count in the header, and above all
// that the door decides who may read a row exactly as the other one does.
func TestRestApiList(t *testing.T) {
	t.Parallel()

	scenarios := []tests.ApiScenario{
		{
			Name:            "a missing table is a 404, as on the other door",
			Method:          http.MethodGet,
			URL:             "/rest/v1/missing",
			ExpectedStatus:  404,
			ExpectedContent: []string{`"data":{}`},
			ExpectedEvents:  map[string]int{"*": 0},
		},
		{
			// THE property that makes this door safe to expose. It runs the same
			// handler, so the list rule that refuses an unauthenticated caller on
			// /v1/collections/demo1/records must refuse the same caller here. A
			// second door that answered 200 would be a read bypass, and it would
			// look like a feature.
			Name:            "a collection with no list rule still needs superuser auth",
			Method:          http.MethodGet,
			URL:             "/rest/v1/demo1",
			ExpectedStatus:  403,
			ExpectedContent: []string{`"data":{}`, `Only superusers`},
			ExpectedEvents:  map[string]int{"*": 0},
		},
		{
			// The wire difference: a BARE ARRAY, not {items,page,perPage,...}.
			// The leading `[{` and the absence of `"items"` are what a Supabase
			// client depends on.
			Name:           "a public table answers a bare array",
			Method:         http.MethodGet,
			URL:            "/rest/v1/demo2",
			ExpectedStatus: 200,
			ExpectedContent: []string{
				`[{`,
				`"id":"0yxhwia2amd8gec"`,
				`"id":"achvryl401bhse3"`,
				`"id":"llvuca81nly1qls"`,
			},
			NotExpectedContent: []string{
				`"items"`,
				`"totalItems"`,
				`"perPage"`,
			},
			ExpectedEvents: map[string]int{
				"*":                    0,
				"OnRecordsListRequest": 1,
				"OnRecordEnrich":       3,
			},
		},
		{
			Name:           "limit narrows the window",
			Method:         http.MethodGet,
			URL:            "/rest/v1/demo2?limit=1",
			ExpectedStatus: 200,
			ExpectedContent: []string{
				`[{`,
			},
			NotExpectedContent: []string{`"items"`},
			ExpectedEvents: map[string]int{
				"*":                    0,
				"OnRecordsListRequest": 1,
				"OnRecordEnrich":       1,
			},
		},
		{
			// An unsatisfiable window is refused rather than rounded, so a caller
			// never receives real rows for a range it did not ask for.
			Name:            "an offset off a page boundary is refused",
			Method:          http.MethodGet,
			URL:             "/rest/v1/demo2?limit=25&offset=30",
			ExpectedStatus:  400,
			ExpectedContent: []string{`page boundary`},
			ExpectedEvents:  map[string]int{"*": 0},
		},
		{
			// THE test whose absence hid a bug that made every filtered read a
			// 400: a filter that is supposed to SUCCEED, executed end to end.
			// demo2 holds test1/test2/test3, so eq on a title returns exactly one.
			Name:           "a filter that should match, matches",
			Method:         http.MethodGet,
			URL:            "/rest/v1/demo2?title=eq.test2",
			ExpectedStatus: 200,
			ExpectedContent: []string{
				`"id":"achvryl401bhse3"`,
				`"title":"test2"`,
			},
			NotExpectedContent: []string{`"id":"llvuca81nly1qls"`, `"items"`},
			ExpectedEvents: map[string]int{
				"*":                    0,
				"OnRecordsListRequest": 1,
				"OnRecordEnrich":       1,
			},
		},
		{
			Name:               "like matches on a wildcard",
			Method:             http.MethodGet,
			URL:                "/rest/v1/demo2?title=like.*est3*",
			ExpectedStatus:     200,
			ExpectedContent:    []string{`"id":"0yxhwia2amd8gec"`},
			NotExpectedContent: []string{`"id":"llvuca81nly1qls"`},
			ExpectedEvents: map[string]int{
				"*":                    0,
				"OnRecordsListRequest": 1,
				"OnRecordEnrich":       1,
			},
		},
		{
			Name:               "in matches several",
			Method:             http.MethodGet,
			URL:                "/rest/v1/demo2?title=in.(test1,test3)",
			ExpectedStatus:     200,
			ExpectedContent:    []string{`"id":"llvuca81nly1qls"`, `"id":"0yxhwia2amd8gec"`},
			NotExpectedContent: []string{`"id":"achvryl401bhse3"`},
			ExpectedEvents: map[string]int{
				"*":                    0,
				"OnRecordsListRequest": 1,
				"OnRecordEnrich":       2,
			},
		},
		{
			Name:               "a boolean column filters on is",
			Method:             http.MethodGet,
			URL:                "/rest/v1/demo2?active=is.false",
			ExpectedStatus:     200,
			ExpectedContent:    []string{`"id":"llvuca81nly1qls"`},
			NotExpectedContent: []string{`"id":"achvryl401bhse3"`},
			ExpectedEvents: map[string]int{
				"*":                    0,
				"OnRecordsListRequest": 1,
				"OnRecordEnrich":       1,
			},
		},
		{
			// A column that is not a field on this collection is refused by the
			// resolver, so a caller cannot name arbitrary SQL as a column.
			Name:            "an unknown column is refused",
			Method:          http.MethodGet,
			URL:             "/rest/v1/demo2?nosuchcolumn=eq.x",
			ExpectedStatus:  400,
			ExpectedContent: []string{`"data":{}`},
			ExpectedEvents:  map[string]int{"*": 0},
		},
		{
			Name:            "an unknown operator is refused",
			Method:          http.MethodGet,
			URL:             "/rest/v1/demo2?title=matches.x",
			ExpectedStatus:  400,
			ExpectedContent: []string{`unknown operator`},
			ExpectedEvents:  map[string]int{"*": 0},
		},
	}

	for _, scenario := range scenarios {
		scenario.Test(t)
	}
}

// The count rides in Content-Range, never in the body — and it is computed only
// when asked for, so an unasked count reports `*` rather than a number nobody
// paid for.
func TestRestApiCount(t *testing.T) {
	t.Parallel()

	scenarios := []tests.ApiScenario{
		{
			Name:               "no Prefer means no count, and the header says so",
			Method:             http.MethodGet,
			URL:                "/rest/v1/demo2",
			ExpectedStatus:     200,
			AfterTestFunc:      wantRange("0-2/*"),
			ExpectedContent:    []string{`[{`, `"id":"llvuca81nly1qls"`},
			NotExpectedContent: []string{`"totalItems"`},
			ExpectedEvents: map[string]int{
				"*":                    0,
				"OnRecordsListRequest": 1,
				"OnRecordEnrich":       3,
			},
		},
		{
			Name:   "Prefer: count=exact reports the total",
			Method: http.MethodGet,
			URL:    "/rest/v1/demo2",
			Headers: map[string]string{
				"Prefer": "count=exact",
			},
			ExpectedStatus:     200,
			AfterTestFunc:      wantRange("0-2/3"),
			ExpectedContent:    []string{`[{`, `"id":"llvuca81nly1qls"`},
			NotExpectedContent: []string{`"totalItems"`},
			ExpectedEvents: map[string]int{
				"*":                    0,
				"OnRecordsListRequest": 1,
				"OnRecordEnrich":       3,
			},
		},
		{
			// The range is derived from the window that actually ran, so the
			// second page reports where it really starts rather than 0.
			Name:   "the second page reports its own offset",
			Method: http.MethodGet,
			URL:    "/rest/v1/demo2?limit=2&offset=2",
			Headers: map[string]string{
				"Prefer": "count=exact",
			},
			ExpectedStatus:     200,
			AfterTestFunc:      wantRange("2-2/3"),
			ExpectedContent:    []string{`"id":"0yxhwia2amd8gec"`},
			NotExpectedContent: []string{`"id":"llvuca81nly1qls"`},
			ExpectedEvents: map[string]int{
				"*":                    0,
				"OnRecordsListRequest": 1,
				"OnRecordEnrich":       1,
			},
		},
	}

	for _, scenario := range scenarios {
		scenario.Test(t)
	}
}
