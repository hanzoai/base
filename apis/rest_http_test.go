package apis_test

import (
	"net/http"
	"strings"
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
			ExpectedContent: []string{`"code":"PGRST202"`, `"details"`, `"hint"`},
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
			ExpectedContent: []string{`"code":"PGRST301"`, `Only superusers`},
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
			ExpectedContent: []string{`"code":"PGRST100"`},
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

// The write verbs. PostgREST's PATCH and DELETE are FILTERED where Base's are
// by-id, so these prove the door resolves a filter to rows and then lets Base's
// own rules decide, rather than reimplementing them.
func TestRestApiWrites(t *testing.T) {
	t.Parallel()

	scenarios := []tests.ApiScenario{
		{
			// The catastrophic shape, refused. PostgREST allows an unfiltered
			// DELETE and postgrest-js sends it without comment, so the first time
			// anyone learns what it means is after the table is empty.
			Name:            "an unfiltered delete is refused",
			Method:          http.MethodDelete,
			URL:             "/rest/v1/demo2",
			ExpectedStatus:  400,
			ExpectedContent: []string{`"code":"PGRST100"`, `needs a filter`, `every row`},
			ExpectedEvents:  map[string]int{"*": 0},
		},
		{
			Name:            "an unfiltered update is refused the same way",
			Method:          http.MethodPatch,
			URL:             "/rest/v1/demo2",
			Body:            strings.NewReader(`{"title":"x"}`),
			ExpectedStatus:  400,
			ExpectedContent: []string{`"code":"PGRST100"`, `needs a filter`},
			ExpectedEvents:  map[string]int{"*": 0},
		},
		{
			// The error body is the shape clients branch on: message/details/
			// hint/code, never Base's {data,message,status}.
			Name:               "an error carries the fields a client reads",
			Method:             http.MethodDelete,
			URL:                "/rest/v1/demo2",
			ExpectedStatus:     400,
			ExpectedContent:    []string{`"message"`, `"details"`, `"hint"`, `"code"`},
			NotExpectedContent: []string{`"data":{}`, `"status":400`},
			ExpectedEvents:     map[string]int{"*": 0},
		},
		{
			// A filtered write still obeys the collection's own rules: demo1 has
			// no list rule, so an unauthenticated caller cannot even resolve the
			// rows, let alone change them.
			Name:            "a filtered delete on a guarded collection still refuses",
			Method:          http.MethodDelete,
			URL:             "/rest/v1/demo1?id=eq.imy661ixudk5izi",
			ExpectedStatus:  403,
			ExpectedContent: []string{`Only superusers`},
			ExpectedEvents:  map[string]int{"*": 0},
		},
		{
			Name:            "a bulk insert is refused rather than half-applied",
			Method:          http.MethodPost,
			URL:             "/rest/v1/demo2",
			Body:            strings.NewReader(`[{"title":"a"},{"title":"b"}]`),
			ExpectedStatus:  501,
			ExpectedContent: []string{`"code":"PGRST100"`, `one object per request`},
			ExpectedEvents:  map[string]int{"*": 0},
		},
	}

	for _, scenario := range scenarios {
		scenario.Test(t)
	}
}

// The writes that are supposed to WORK, executed end to end. Their absence is
// exactly what let a broken filter ship green, so the success path is the part
// that has to be measured rather than reasoned about.
func TestRestApiWritesThatSucceed(t *testing.T) {
	t.Parallel()

	scenarios := []tests.ApiScenario{
		{
			// PostgREST returns nothing unless asked, and postgrest-js reads an
			// empty body as exactly that. 201, no body.
			Name:           "insert returns 201 and no body by default",
			Method:         http.MethodPost,
			URL:            "/rest/v1/demo2",
			Body:           strings.NewReader(`{"title":"from-rest"}`),
			ExpectedStatus: 201,
			ExpectedEvents: map[string]int{"OnRecordCreateRequest": 1},
		},
		{
			Name:   "insert returns the row when asked",
			Method: http.MethodPost,
			URL:    "/rest/v1/demo2",
			Body:   strings.NewReader(`{"title":"from-rest"}`),
			Headers: map[string]string{
				"Prefer": "return=representation",
			},
			ExpectedStatus:  201,
			ExpectedContent: []string{`"title":"from-rest"`},
			ExpectedEvents:  map[string]int{"OnRecordCreateRequest": 1},
		},
		{
			// .single() on a write: a bare object, not a one-element array. A
			// server that answers `[{...}]` hands every .single() caller an array
			// where they wrote data.id.
			Name:   "insert answers a bare object for .single()",
			Method: http.MethodPost,
			URL:    "/rest/v1/demo2",
			Body:   strings.NewReader(`{"title":"single-rest"}`),
			Headers: map[string]string{
				"Prefer": "return=representation",
				"Accept": "application/vnd.pgrst.object+json",
			},
			ExpectedStatus:     201,
			ExpectedContent:    []string{`"title":"single-rest"`},
			NotExpectedContent: []string{`[{`},
			ExpectedEvents:     map[string]int{"OnRecordCreateRequest": 1},
		},
		{
			Name:           "a filtered update changes the rows the filter selects",
			Method:         http.MethodPatch,
			URL:            "/rest/v1/demo2?title=eq.test1",
			Body:           strings.NewReader(`{"title":"renamed"}`),
			ExpectedStatus: 204,
			ExpectedEvents: map[string]int{"OnRecordUpdateRequest": 1},
		},
		{
			Name:   "a filtered update returns the changed rows when asked",
			Method: http.MethodPatch,
			URL:    "/rest/v1/demo2?title=eq.test1",
			Body:   strings.NewReader(`{"title":"renamed"}`),
			Headers: map[string]string{
				"Prefer": "return=representation",
			},
			ExpectedStatus:     200,
			ExpectedContent:    []string{`"title":"renamed"`},
			NotExpectedContent: []string{`"title":"test1"`},
			ExpectedEvents:     map[string]int{"OnRecordUpdateRequest": 1},
		},
		{
			Name:           "a filtered delete removes them",
			Method:         http.MethodDelete,
			URL:            "/rest/v1/demo2?title=eq.test1",
			ExpectedStatus: 204,
			ExpectedEvents: map[string]int{"OnRecordDeleteRequest": 1},
		},
		{
			// A filter that selects nothing is a no-op, not an error — and the
			// count says so.
			Name:           "a filter that matches nothing changes nothing",
			Method:         http.MethodDelete,
			URL:            "/rest/v1/demo2?title=eq.nosuchtitle",
			ExpectedStatus: 204,
			ExpectedEvents: map[string]int{"*": 0},
		},
	}

	for _, scenario := range scenarios {
		scenario.Test(t)
	}
}

// A duplicate is ordinary application logic, not an exceptional case: clients
// branch on error.code === '23505' to show "that name is taken". demo2 carries
// idx_unique_demo2_title, so inserting a title it already holds is the real
// thing rather than a simulated one.
func TestRestApiDuplicateIsSQLState23505(t *testing.T) {
	t.Parallel()

	scenarios := []tests.ApiScenario{
		{
			Name:           "a unique violation answers 409 with the SQLSTATE",
			Method:         http.MethodPost,
			URL:            "/rest/v1/demo2",
			Body:           strings.NewReader(`{"title":"test1"}`),
			ExpectedStatus: 409,
			ExpectedContent: []string{
				`"code":"23505"`,
				`duplicate key value violates unique constraint`,
				`already exists`,
			},
			NotExpectedContent: []string{`"data":{`, `"status":409`},
			ExpectedEvents:     map[string]int{"OnRecordCreateRequest": 1},
		},
		{
			// The same write on Base's own door keeps Base's shape — the
			// PostgREST body belongs to the PostgREST wire, not to the store.
			Name:            "the collections door still answers its own shape",
			Method:          http.MethodPost,
			URL:             "/v1/collections/demo2/records",
			Body:            strings.NewReader(`{"title":"test1"}`),
			ExpectedStatus:  400,
			ExpectedContent: []string{`validation_not_unique`},
			ExpectedEvents:  map[string]int{"OnRecordCreateRequest": 1},
		},
	}

	for _, scenario := range scenarios {
		scenario.Test(t)
	}
}
