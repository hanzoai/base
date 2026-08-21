package apis_test

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/hanzoai/base/core"
	"github.com/hanzoai/base/tests"
	"github.com/hanzoai/dbx"
)

// The corpus these run against is tests/data, a SQLite file, so the engine here
// is always the embedded one. What the report says on a server is asserted in
// tests/engine, which builds its fixtures through the API and can therefore be
// pointed at one.

// databaseSuperuser is a signed auth token for _superusers/sywbhecnh46rhm0 in
// the test data.
const databaseSuperuser = "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJjb2xsZWN0aW9uSWQiOiJoYmNfMzE0MjYzNTgyMyIsImV4cCI6MjUyNDYwNDQ2MSwiaWQiOiJzeXdiaGVjbmg0NnJobTAiLCJyZWZyZXNoYWJsZSI6dHJ1ZSwidHlwZSI6ImF1dGgifQ.CXBf8BazmUeg2RnJW8OEs1UFYF41rbCMOa6YZa4wZio"

func TestDatabaseRead(t *testing.T) {
	t.Parallel()

	scenarios := []tests.ApiScenario{
		{
			Name:            "unauthorized",
			Method:          http.MethodGet,
			URL:             "/v1/database",
			ExpectedStatus:  401,
			ExpectedContent: []string{`"data":{}`},
			ExpectedEvents:  map[string]int{"*": 0},
		},
		{
			Name:            "authorized as regular user",
			Method:          http.MethodGet,
			URL:             "/v1/database",
			Headers:         map[string]string{"Authorization": userToken},
			ExpectedStatus:  403,
			ExpectedContent: []string{`"data":{}`},
			ExpectedEvents:  map[string]int{"*": 0},
		},
		{
			Name:           "authorized as superuser",
			Method:         http.MethodGet,
			URL:            "/v1/database",
			Headers:        map[string]string{"Authorization": databaseSuperuser},
			ExpectedStatus: 200,
			ExpectedContent: []string{
				`"engine":"sqlite"`,
				`"local":true`,
				`"path":"`,
				`data.db"`,
				`auxiliary.db"`,
				// a base collection, an auth one and a view all carry a count,
				// so the report covers every kind of collection there is
				`{"records":3,"id":"wsmn24bux7wo113","name":"demo1","type":"base","system":false}`,
				`{"records":4,"id":"hbc_3142635823","name":"_superusers","type":"auth","system":true}`,
				`{"records":3,"id":"v9gwnfh02gjq1q0","name":"view1","type":"view","system":false}`,
			},
			ExpectedEvents: map[string]int{"*": 0},
		},
	}

	for _, scenario := range scenarios {
		scenario.Test(t)
	}
}

func TestDatabaseReclaim(t *testing.T) {
	t.Parallel()

	// What each database occupied when the request was made, so the assertions
	// after it are about the change rather than about an absolute size. Both,
	// because a reclaim that had stopped rewriting one of them still frees a
	// few kilobytes off the other and would read as a success.
	var before map[string]int64

	scenarios := []tests.ApiScenario{
		{
			Name:            "unauthorized",
			Method:          http.MethodPost,
			URL:             "/v1/database/reclaim",
			ExpectedStatus:  401,
			ExpectedContent: []string{`"data":{}`},
			ExpectedEvents:  map[string]int{"*": 0},
		},
		{
			Name:            "authorized as regular user",
			Method:          http.MethodPost,
			URL:             "/v1/database/reclaim",
			Headers:         map[string]string{"Authorization": userToken},
			ExpectedStatus:  403,
			ExpectedContent: []string{`"data":{}`},
			ExpectedEvents:  map[string]int{"*": 0},
		},
		{
			Name:    "authorized as superuser",
			Method:  http.MethodPost,
			URL:     "/v1/database/reclaim",
			Headers: map[string]string{"Authorization": databaseSuperuser},
			BeforeTestFunc: func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
				databaseBloat(t, app)
				before = databaseSizes(t, app)
			},
			ExpectedStatus:  200,
			ExpectedContent: []string{`"before":`, `"after":`},
			ExpectedEvents:  map[string]int{"*": 0},
			AfterTestFunc: func(t testing.TB, app *tests.TestApp, res *http.Response) {
				for name, after := range databaseSizes(t, app) {
					if after >= before[name] {
						t.Fatalf("expected the reclaim to free the pages the dropped rows left behind in %s, still holding %d of %d bytes", name, after, before[name])
					}
				}
			},
		},
	}

	for _, scenario := range scenarios {
		scenario.Test(t)
	}
}

// databaseBloat leaves both databases physically large and mostly empty, which
// is the state a reclaim is for. Both engines spell the rows, so the same ones
// are written whichever is connected.
//
// The fold at the end is what puts the freed pages in the database rather than
// in its log, and is the difference between a case that measures the rewrite
// and one that only measures a later fold: without it the rewrite can be taken
// out of ReclaimDatabase entirely and this still passes.
func databaseBloat(t testing.TB, app *tests.TestApp) {
	t.Helper()

	for _, db := range []dbx.Builder{app.NonconcurrentDB(), app.AuxNonconcurrentDB()} {
		for _, query := range []string{
			"CREATE TABLE _bloat (v TEXT)",
			"INSERT INTO _bloat (v) WITH RECURSIVE c(x) AS (SELECT 1 UNION ALL SELECT x+1 FROM c WHERE x < 20000) " +
				"SELECT 'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa' FROM c",
			"DROP TABLE _bloat",
			app.Dialect().Checkpoint(),
		} {
			if query == "" {
				continue // an engine with no log to fold
			}
			if _, err := db.NewQuery(query).Execute(); err != nil {
				t.Fatal(err)
			}
		}
	}
}

// databaseSizes is each database measured from outside the app, so the
// assertions do not read their answer out of the code that produced it. A
// database's log counts as part of it: a rewrite lands there first, so the file
// on its own reads smaller than the database is.
func databaseSizes(t testing.TB, app *tests.TestApp) map[string]int64 {
	t.Helper()

	sizes := map[string]int64{}

	for _, name := range []string{"data.db", "auxiliary.db"} {
		for _, path := range []string{name, name + "-wal"} {
			info, err := os.Stat(filepath.Join(app.DataDir(), path))
			if err != nil {
				t.Fatal(err)
			}
			sizes[name] += info.Size()
		}
	}

	return sizes
}
