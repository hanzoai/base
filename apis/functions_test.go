package apis_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/hanzoai/base/core"
	_ "github.com/hanzoai/base/plugins/gojavm" // the "js" runtime a function runs on
	"github.com/hanzoai/base/tests"
	"github.com/hanzoai/base/tools/types"
)

// notes is a collection whose rows belong to whoever wrote them, which is the
// rule almost every real one has. Everything below is a statement about that
// rule seen from inside a function.
func seedFunctions(t testing.TB) *tests.TestApp {
	t.Helper()

	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatal(err)
	}

	own := "owner = @request.auth.id"
	notes := core.NewBaseCollection("notes")
	notes.ListRule = types.Pointer(own)
	notes.ViewRule = types.Pointer(own)
	notes.Fields.Add(
		&core.TextField{Name: "owner", Required: true},
		&core.TextField{Name: "title"},
		&core.TextField{Name: "secret", Hidden: true},
	)
	if err := app.Save(notes); err != nil {
		t.Fatal(err)
	}

	for _, row := range []struct{ id, owner, title string }{
		{"noteoneaaaaaaaa", tests.TestUserID1, "mine"},
		{"notetwoaaaaaaaa", tests.TestUserID2, "theirs"},
	} {
		r := core.NewRecord(notes)
		r.Id = row.id
		r.Set("owner", row.owner)
		r.Set("title", row.title)
		r.Set("secret", "shh")
		if err := app.Save(r); err != nil {
			t.Fatal(err)
		}
	}

	// A deployment that wants its functions callable by its users says so on the
	// collection, the same way it would for any other row. The source stays
	// unreadable either way — it is a hidden field, not a second rule.
	functions, err := app.FindCollectionByNameOrId(core.CollectionNameFunctions)
	if err != nil {
		t.Fatal(err)
	}
	functions.ViewRule = types.Pointer("")
	if err := app.Save(functions); err != nil {
		t.Fatal(err)
	}

	for name, src := range map[string]string{
		"titles":  `function handler(p, base){ return base.list({collection:"notes"}).map(function(r){ return r.title }) }`,
		"one":     `function handler(p, base){ return base.one({collection:"notes", id:p.id}) }`,
		"private": `function handler(p, base){ return base.list({collection:"demo1"}) }`,
		"spin":    `function handler(){ while (true) {} }`,
		"double":  `function handler(p){ return {doubled: p.n * 2} }`,
		"burrow":  `function handler(p, base){ return base.list({collection:"notes", filter:"@request.auth.id != ''"}) }`,
	} {
		r := core.NewRecord(functions)
		r.Id = name
		r.Set(core.FieldNameSource, src)
		if err := app.Save(r); err != nil {
			t.Fatal(err)
		}
	}

	return app
}

func invoke(t *testing.T, app *tests.TestApp, name, token, body string, status int, want ...string) {
	t.Helper()

	if body == "" {
		body = "{}"
	}

	scenario := tests.ApiScenario{
		Name:                  name + " as " + token[:min(8, len(token))],
		Method:                http.MethodPost,
		URL:                   "/v1/functions/" + name,
		Body:                  strings.NewReader(body),
		Headers:               map[string]string{"Authorization": token, "Content-Type": "application/json"},
		TestAppFactory:        func(testing.TB) *tests.TestApp { return app },
		DisableTestAppCleanup: true,
		ExpectedStatus:        status,
		ExpectedContent:       want,
	}
	scenario.Test(t)
}

// A function reads what its caller may read, and stops there. Same function,
// same collection, two callers: each one is answered with its own row, because
// the rule is evaluated against whoever made the request and not against
// whoever wrote the function.
func TestFunctionReadsAsItsCaller(t *testing.T) {
	t.Parallel()

	app := seedFunctions(t)
	defer app.Cleanup()

	one, err := tests.GetUserAuthToken(app, "users", tests.TestUserID1)
	if err != nil {
		t.Fatal(err)
	}
	two, err := tests.GetUserAuthToken(app, "users", tests.TestUserID2)
	if err != nil {
		t.Fatal(err)
	}

	invoke(t, app, "titles", one, "", 200, `["mine"]`)
	invoke(t, app, "titles", two, "", 200, `["theirs"]`)
}

// The refusal is the same shape the wire gives: a row outside the rule is not
// there. `one` fetches by id, so the caller names the other user's row directly
// and is answered with the null it would get for a row that was never written.
func TestFunctionIsRefusedWhatItsCallerMayNotRead(t *testing.T) {
	t.Parallel()

	app := seedFunctions(t)
	defer app.Cleanup()

	one, err := tests.GetUserAuthToken(app, "users", tests.TestUserID1)
	if err != nil {
		t.Fatal(err)
	}

	invoke(t, app, "one", one, `{"id":"noteoneaaaaaaaa"}`, 200, `"title":"mine"`)
	invoke(t, app, "one", one, `{"id":"notetwoaaaaaaaa"}`, 200, "null")

	// A collection with no list rule is superuser-only, and a function running
	// as a user is not a way to become one — demo1 is exactly that collection.
	invoke(t, app, "private", one, "", 500, `"status":500`)

	// A hidden field stays hidden. The function reads the row it owns and the
	// column it may not see is not in what it gets.
	invoke(t, app, "one", one, `{"id":"noteoneaaaaaaaa"}`, 200, `"title":"mine"`)
	assertAbsent(t, app, "one", one, `{"id":"noteoneaaaaaaaa"}`, "shh")

	// Nor may it filter on the rule vocabulary a client is refused. The same
	// guard, because it is the same guard.
	invoke(t, app, "burrow", one, "", 500, `"status":500`)
}

func assertAbsent(t *testing.T, app *tests.TestApp, name, token, body, absent string) {
	t.Helper()

	scenario := tests.ApiScenario{
		Name:                  name + " hides " + absent,
		Method:                http.MethodPost,
		URL:                   "/v1/functions/" + name,
		Body:                  strings.NewReader(body),
		Headers:               map[string]string{"Authorization": token, "Content-Type": "application/json"},
		TestAppFactory:        func(testing.TB) *tests.TestApp { return app },
		DisableTestAppCleanup: true,
		ExpectedStatus:        200,
		NotExpectedContent:    []string{absent},
	}
	scenario.Test(t)
}

// A function that never stops is stopped. The engine is interrupted rather than
// abandoned, so the VM it borrowed goes back to serving other callers.
func TestFunctionThatDoesNotFinishIsStopped(t *testing.T) {
	t.Parallel()

	app := seedFunctions(t)
	defer app.Cleanup()

	token, err := tests.GetUserAuthToken(app, "users", tests.TestUserID1)
	if err != nil {
		t.Fatal(err)
	}

	invoke(t, app, "spin", token, "", http.StatusGatewayTimeout, "did not finish")

	// And the process kept working: the next call is answered normally.
	invoke(t, app, "double", token, `{"n":21}`, 200, `"doubled":42`)
}

// The payload is the body, and the answer is whatever the function returned.
func TestFunctionCarriesItsPayload(t *testing.T) {
	t.Parallel()

	app := seedFunctions(t)
	defer app.Cleanup()

	token, err := tests.GetUserAuthToken(app, "users", tests.TestUserID1)
	if err != nil {
		t.Fatal(err)
	}

	invoke(t, app, "double", token, `{"n":4}`, 200, `{"doubled":8}`)
}

// A function nobody may see is a function that is not there. The collection's
// view rule is the whole answer — invoking is reaching the row.
func TestFunctionUnseenIsUnreachable(t *testing.T) {
	t.Parallel()

	app := seedFunctions(t)
	defer app.Cleanup()

	token, err := tests.GetUserAuthToken(app, "users", tests.TestUserID1)
	if err != nil {
		t.Fatal(err)
	}

	invoke(t, app, "nosuch", token, "", 404, `"status":404`)

	// Closing the rule closes the door, without a word said about invoking.
	functions, err := app.FindCollectionByNameOrId(core.CollectionNameFunctions)
	if err != nil {
		t.Fatal(err)
	}
	functions.ViewRule = nil
	if err := app.Save(functions); err != nil {
		t.Fatal(err)
	}

	invoke(t, app, "double", token, `{"n":1}`, 403, `"status":403`)
}

// Management is the collection's rules and nothing else. A user who may not
// write a function cannot write one at either address, and the source is not
// something a user reads back at either.
func TestFunctionManagementIsTheCollectionsRules(t *testing.T) {
	t.Parallel()

	app := seedFunctions(t)
	defer app.Cleanup()

	user, err := tests.GetUserAuthToken(app, "users", tests.TestUserID1)
	if err != nil {
		t.Fatal(err)
	}
	superuser, err := tests.GetSuperuserAuthToken(app, tests.TestSuperuserID1)
	if err != nil {
		t.Fatal(err)
	}

	write := func(name, token, url string, status int, expected ...string) {
		t.Helper()
		scenario := tests.ApiScenario{
			Name:                  name,
			Method:                http.MethodPost,
			URL:                   url,
			Body:                  strings.NewReader(`{"id":"fresh","source":"function handler(){return 1}"}`),
			Headers:               map[string]string{"Authorization": token, "Content-Type": "application/json"},
			TestAppFactory:        func(testing.TB) *tests.TestApp { return app },
			DisableTestAppCleanup: true,
			ExpectedStatus:        status,
			ExpectedContent:       expected,
		}
		scenario.Test(t)
	}

	// The create rule is nil, so only a superuser writes — and both doors say
	// so in the same words, because it is one rule on one collection.
	refused := `"message":"Only superusers can perform this action."`
	write("user cannot write at /v1/functions", user, "/v1/functions", 403, refused)
	write("user cannot write at the collection", user, "/v1/collections/_functions/records", 403, refused)
	write("superuser writes", superuser, "/v1/functions", 200, `"id":"fresh"`, `"collectionName":"_functions"`)

	// A user who may view a function still does not get its source back.
	read := tests.ApiScenario{
		Name:                  "a user reads a function without its source",
		Method:                http.MethodGet,
		URL:                   "/v1/functions/double",
		Headers:               map[string]string{"Authorization": user},
		TestAppFactory:        func(testing.TB) *tests.TestApp { return app },
		DisableTestAppCleanup: true,
		ExpectedStatus:        200,
		ExpectedContent:       []string{`"id":"double"`},
		NotExpectedContent:    []string{"doubled"},
	}
	read.Test(t)

	// The superuser who wrote it does.
	readAll := tests.ApiScenario{
		Name:                  "a superuser reads the source",
		Method:                http.MethodGet,
		URL:                   "/v1/functions/double",
		Headers:               map[string]string{"Authorization": superuser},
		TestAppFactory:        func(testing.TB) *tests.TestApp { return app },
		DisableTestAppCleanup: true,
		ExpectedStatus:        200,
		ExpectedContent:       []string{`"id":"double"`, "doubled"},
	}
	readAll.Test(t)
}
