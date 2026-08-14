package apis_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/hanzoai/base/apis"
	"github.com/hanzoai/base/core"
	_ "github.com/hanzoai/base/plugins/gojavm" // the "js" runtime a function runs on
	"github.com/hanzoai/base/tests"
	"github.com/hanzoai/base/tools/types"
)

// A function starts work somewhere else, and these are the statements about
// what that does and does not give it. The interesting half is the second one:
// a sandbox has a network and processes, so the thing worth proving is that
// asking for one leaves the function exactly where it was.

// recorder is a [apis.Sandboxes] that starts nothing and remembers what it was
// asked to start. It is the only place the org and the spec are visible at the
// same moment, which is what makes it the place to assert that they came from
// different sources.
type recorder struct {
	mu    sync.Mutex
	orgs  []string
	specs []apis.Sandbox
	fails error
}

func (r *recorder) Start(_ context.Context, org string, s apis.Sandbox) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.fails != nil {
		return "", r.fails
	}
	r.orgs = append(r.orgs, org)
	r.specs = append(r.specs, s)
	return fmt.Sprintf("sbx%d", len(r.specs)), nil
}

func (r *recorder) started() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.specs)
}

func (r *recorder) at(i int) (string, apis.Sandbox) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.orgs[i], r.specs[i]
}

// seedRuns builds a Base whose functions are the given sources and whose work
// runs into a recorder.
func seedRuns(t testing.TB, sources map[string]string) (*tests.TestApp, *recorder) {
	t.Helper()

	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatal(err)
	}

	functions, err := app.FindCollectionByNameOrId(core.CollectionNameFunctions)
	if err != nil {
		t.Fatal(err)
	}
	functions.ViewRule = types.Pointer("")
	if err := app.Save(functions); err != nil {
		t.Fatal(err)
	}

	for name, src := range sources {
		r := core.NewRecord(functions)
		r.Id = name
		r.Set(core.FieldNameSource, src)
		if err := app.Save(r); err != nil {
			t.Fatal(err)
		}
	}

	rec := &recorder{}
	app.Store().Set(apis.StoreKeySandboxes, apis.Sandboxes(rec))

	return app, rec
}

// invokeIn calls a function on a request that is acting in org.
//
// Naming the org on the request is what a resolved credential does before any
// handler runs, so standing in for it here is standing in for the door and not
// for the rule — the rule is what is under test.
func invokeIn(t *testing.T, app *tests.TestApp, name, token, body, org string, status int, want ...string) {
	t.Helper()

	if body == "" {
		body = "{}"
	}

	scenario := tests.ApiScenario{
		Name:    name + " in " + org,
		Method:  http.MethodPost,
		URL:     "/v1/functions/" + name,
		Body:    strings.NewReader(body),
		Headers: map[string]string{"Authorization": token, "Content-Type": "application/json"},
		BeforeTestFunc: func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
			e.Router.BindFunc(func(re *core.RequestEvent) error {
				re.Set(apis.RequestEventKeyOrg, org)
				return re.Next()
			})
		},
		TestAppFactory:        func(testing.TB) *tests.TestApp { return app },
		DisableTestAppCleanup: true,
		ExpectedStatus:        status,
		ExpectedContent:       want,

		// Whatever the host said to the function, it said to the function. What
		// a run was refused for is between the deployment and its own code, and
		// none of it is the answer to somebody's HTTP request — so every call
		// in this file asserts the wording never turns up in a body.
		NotExpectedContent: []string{
			"nowhere to run work",
			"the deployment's to write",
			"unknown field",
			"needs a snapshot",
			"needs an org",
			"sk-deployment-secret",
			"10.4.2.9",
		},
	}
	scenario.Test(t)
}

// What a function gets for starting work is a name, and it gets it straight
// away. The run itself is minutes long and the request is not, so the two are
// not the same call: nothing about the work's result comes back here, and the
// name is what the caller looks under once it does.
func TestFunctionStartsWorkAndIsAnsweredWithItsName(t *testing.T) {
	t.Parallel()

	app, rec := seedRuns(t, map[string]string{
		"research": `function handler(p, base){ return base.start({snapshot:"hanzo/research", env:{TOPIC:p.topic}}) }`,
	})
	defer app.Cleanup()

	token, err := tests.GetUserAuthToken(app, "users", tests.TestUserID1)
	if err != nil {
		t.Fatal(err)
	}

	invokeIn(t, app, "research", token, `{"topic":"kelp"}`, "acme", 200, `{"id":"sbx1"}`)

	if rec.started() != 1 {
		t.Fatalf("expected 1 run, got %d", rec.started())
	}

	org, spec := rec.at(0)
	if org != "acme" {
		t.Fatalf("expected the request's org, got %q", org)
	}
	if spec.Snapshot != "hanzo/research" {
		t.Fatalf("expected the asked-for snapshot, got %q", spec.Snapshot)
	}
	if spec.Env["TOPIC"] != "kelp" {
		t.Fatalf("expected the payload to reach the run, got %q", spec.Env["TOPIC"])
	}
}

// Starting one changes nothing about what the function itself can touch.
//
// A sandbox has a network, a filesystem and processes; a function has three
// names and no way to reach any of that, before or after asking for one. The
// two lists this returns are the whole surface: what is reachable in the
// language, and what the host bound.
func TestFunctionReachesNoNetworkAndNoProcess(t *testing.T) {
	t.Parallel()

	probe := `function handler(p, base){
	  var reachable = [];
	  ["fetch","XMLHttpRequest","WebSocket","process","Buffer","$os","$http","$app","$security"].forEach(function(n){
	    try { if (eval("typeof " + n) !== "undefined") { reachable.push(n) } } catch (e) {}
	  });
	  ["http","https","net","dgram","child_process","fs","os"].forEach(function(m){
	    try { require(m); reachable.push("require:" + m) } catch (e) {}
	  });
	  base.start({snapshot:"hanzo/research"});
	  ["fetch","process","$os"].forEach(function(n){
	    try { if (eval("typeof " + n) !== "undefined") { reachable.push("after:" + n) } } catch (e) {}
	  });
	  return {reachable: reachable, host: Object.keys(base).sort()};
	}`

	app, rec := seedRuns(t, map[string]string{"probe": probe})
	defer app.Cleanup()

	token, err := tests.GetUserAuthToken(app, "users", tests.TestUserID1)
	if err != nil {
		t.Fatal(err)
	}

	invokeIn(t, app, "probe", token, "", "acme", 200,
		`{"reachable":[],"host":["list","one","start"]}`)

	// The run was started, so the emptiness above is the state of a function
	// that has one rather than of a function that never asked.
	if rec.started() != 1 {
		t.Fatalf("expected 1 run, got %d", rec.started())
	}
}

// Starting is counted on its own, because it is not priced like a read.
//
// Two reads cost two queries against a file that is already open. Two runs are
// two machines, held for as long as the work takes, so the second one here is
// refused by the rule that says so — and refused as the limit it is, not as a
// deployment that broke.
func TestStartingCountsAgainstItsOwnLimit(t *testing.T) {
	t.Parallel()

	app, rec := seedRuns(t, map[string]string{
		"twice": `function handler(p, base){ return {a: base.start({snapshot:"x"}).id, b: base.start({snapshot:"x"}).id} }`,
	})
	defer app.Cleanup()

	app.Settings().RateLimits.Enabled = true
	app.Settings().RateLimits.Rules = []core.RateLimitRule{
		{Label: "_functions:start", MaxRequests: 1, Duration: 10},
	}

	token, err := tests.GetUserAuthToken(app, "users", tests.TestUserID1)
	if err != nil {
		t.Fatal(err)
	}

	invokeIn(t, app, "twice", token, "", "acme", 429, `"status":429`)

	if rec.started() != 1 {
		t.Fatalf("expected the rule to stop the second run, got %d started", rec.started())
	}
}

// One call starts the work one request implies, and stops there.
//
// The rule above is what an operator writes; this is what holds when nobody has
// written one, which is every Base out of the box — rate limits ship disabled,
// so without this a single invoke starts as many machines as it can name in
// five seconds. Measured before the cap existed: five thousand.
func TestOneCallStartsABoundedNumberOfRuns(t *testing.T) {
	t.Parallel()

	app, rec := seedRuns(t, map[string]string{
		"burst": `function handler(p, base){
		  var n = 0;
		  try { for (var i = 0; i < 5000; i++) { base.start({snapshot:"x"}); n++ } }
		  catch (e) { return {n: n, stopped: true} }
		  return {n: n, stopped: false};
		}`,
	})
	defer app.Cleanup()

	token, err := tests.GetUserAuthToken(app, "users", tests.TestUserID1)
	if err != nil {
		t.Fatal(err)
	}

	// Nothing is configured here: these are the settings a Base boots with.
	invokeIn(t, app, "burst", token, "", "acme", 200, `{"n":8,"stopped":true}`)

	if rec.started() != 8 {
		t.Fatalf("expected the cap to hold at 8, got %d", rec.started())
	}
}

// Whose work a run is comes from the request, so there is nothing for a
// function to name.
//
// The org is not a field of what gets asked for — it is passed beside it, off a
// credential resolved before any function ran. So a function that writes one is
// not overriding anything; it is naming a field that does not exist, and the
// same goes for the labels a deployment stamps a run's owner into.
func TestFunctionStartsWorkOnlyInItsOwnOrg(t *testing.T) {
	t.Parallel()

	app, rec := seedRuns(t, map[string]string{
		"named":   `function handler(p, base){ return base.start({snapshot:"x", org:"victim"}) }`,
		"labeled": `function handler(p, base){ return base.start({snapshot:"x", labels:{"hanzo.ai/org":"victim"}}) }`,
		"plain":   `function handler(p, base){ return base.start({snapshot:"x", labels:{mine:"yes"}}) }`,
	})
	defer app.Cleanup()

	token, err := tests.GetUserAuthToken(app, "users", tests.TestUserID1)
	if err != nil {
		t.Fatal(err)
	}

	// A function asking for something that is not on offer is the deployment's
	// own code being wrong, which is what it is answered as.
	invokeIn(t, app, "named", token, "", "acme", 500, `"status":500`)
	invokeIn(t, app, "labeled", token, "", "acme", 500, `"status":500`)

	if rec.started() != 0 {
		t.Fatalf("expected nothing to have been started, got %d", rec.started())
	}

	// The same function, asking for nothing it may not, runs in the org the
	// request is acting in — never in the one the source mentioned.
	invokeIn(t, app, "plain", token, "", "acme", 200, `{"id":"sbx1"}`)

	org, spec := rec.at(0)
	if org != "acme" {
		t.Fatalf("expected the request's org, got %q", org)
	}
	if spec.Labels["mine"] != "yes" {
		t.Fatalf("expected the caller's own labels to carry through, got %v", spec.Labels)
	}
}

// On a Base that tells orgs apart, a run belongs to one of them.
//
// A function is invokable by whoever the collection's rule says, and a rule of
// "" says everybody — so a request can reach here carrying no credential and
// therefore no org. On a deployment serving many that is not work belonging to
// nobody, it is work whose owner was never established, and it is refused. A
// Base serving one tenant names no org on any request and is unaffected: the
// question is asked of the deployment, not of the request.
func TestARunNeedsAnOrgWhereThereAreOrgs(t *testing.T) {
	t.Parallel()

	app, rec := seedRuns(t, map[string]string{
		"work": `function handler(p, base){ return base.start({snapshot:"x"}) }`,
	})
	defer app.Cleanup()

	token, err := tests.GetUserAuthToken(app, "users", tests.TestUserID1)
	if err != nil {
		t.Fatal(err)
	}

	// A Base serving one tenant: nobody to confuse the run with, so it runs.
	invokeIn(t, app, "work", token, "", "", 200, `{"id":"sbx1"}`)

	if org, _ := rec.at(0); org != "" {
		t.Fatalf("expected the one tenant's empty org, got %q", org)
	}

	// The same request on a deployment that serves many is refused, because
	// there the answer to "whose run is this" was never established.
	app.Store().Set(apis.StoreKeyBases, apis.Bases(func(string) (core.App, error) {
		return app, nil
	}))

	invokeIn(t, app, "work", token, "", "", 500, `"status":500`)

	if rec.started() != 1 {
		t.Fatalf("expected the unowned run to be refused, got %d started", rec.started())
	}
}

// What went wrong where the work runs is not the caller's to read.
//
// A function may catch what start raises, and a function is the tenant's while
// its caller may be anybody the rule admits — so the words start uses are the
// words that reach an end user. The deployment's own failure is logged whole
// and the function is told only that the run did not start, which is the rule
// functionInvoke already applies to a function that fails outright.
func TestStartTellsTheFunctionNothingAboutTheDeployment(t *testing.T) {
	t.Parallel()

	app, rec := seedRuns(t, map[string]string{
		"leaky": `function handler(p, base){ try { base.start({snapshot:"x"}) } catch (e) { return {said: e.message} } return {said:"no error"} }`,
	})
	defer app.Cleanup()

	rec.fails = errors.New(`dial tcp 10.4.2.9:8443: bad bearer sk-deployment-secret`)

	token, err := tests.GetUserAuthToken(app, "users", tests.TestUserID1)
	if err != nil {
		t.Fatal(err)
	}

	invokeIn(t, app, "leaky", token, "", "acme", 200, `{"said":"start: the run did not start"}`)
}

// A deployment with nowhere to run work still answers the name.
//
// The alternative is binding the name only where a runtime is configured, which
// makes the same source read as a missing function on one deployment and a
// refusal on another. One vocabulary everywhere; the answer is what differs.
func TestStartRefusesWhenTheDeploymentRunsNoWork(t *testing.T) {
	t.Parallel()

	app, _ := seedRuns(t, map[string]string{
		"work": `function handler(p, base){ return {names: Object.keys(base).sort(), started: base.start({snapshot:"x"})} }`,
	})
	defer app.Cleanup()

	app.Store().Remove(apis.StoreKeySandboxes)

	token, err := tests.GetUserAuthToken(app, "users", tests.TestUserID1)
	if err != nil {
		t.Fatal(err)
	}

	invokeIn(t, app, "work", token, "", "acme", 500, `"status":500`)
}
