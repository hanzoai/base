package apis

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/hanzoai/base/core"
)

// A grant that was not spent in time is not spent at all.
//
// The deadline is the only thing standing between "a stream was opened once"
// and a string in someone's proxy log that still opens one.
func TestAGrantExpires(t *testing.T) {
	table := &grants{m: map[string]*grant{}}

	fresh := table.mint(&grant{deadline: time.Now().Add(grantTTL)})
	stale := table.mint(&grant{deadline: time.Now().Add(-time.Second)})

	if table.spend(stale) != nil {
		t.Error("a grant past its deadline was spent")
	}
	if table.spend(fresh) == nil {
		t.Error("a grant inside its deadline was refused")
	}
	if table.spend(fresh) != nil {
		t.Error("a grant was spent twice")
	}
}

// Minting sweeps what nobody came back for, so the table holds a half minute of
// grants rather than a process's worth.
func TestMintingDropsWhatExpired(t *testing.T) {
	table := &grants{m: map[string]*grant{}}

	table.mint(&grant{deadline: time.Now().Add(-time.Second)})
	table.mint(&grant{deadline: time.Now().Add(grantTTL)})

	if len(table.m) != 1 {
		t.Errorf("the table holds %d grants, want the one that is still good", len(table.m))
	}
}

// The grant a stream is opened with travels in the query, so the log has to
// render it as absent.
//
// A log row is kept for Logs.MaxDays, served by GET /v1/logs and copied into
// every backup. A grant is worth one stream for half a minute, which is the
// reason it is a grant and not the caller's own token — but half a minute is
// not none, and three places is not one.
func TestTheLogRendersNoGrant(t *testing.T) {
	u, err := url.Parse("/v1/realtime?token=3o6r0aynxnhwgcz2mdnprwl9xytkryun0z8lz4iw")
	if err != nil {
		t.Fatal(err)
	}

	got := redactQuery(u)

	if strings.Contains(got, "3o6r0aynxnhwgcz2mdnprwl9xytkryun0z8lz4iw") {
		t.Errorf("the log holds the grant: %s", got)
	}
	if !strings.Contains(got, "token=redacted") {
		t.Errorf("the log lost the shape of the call: %s", got)
	}
}

// A host routing a stream to its org's Base reads the org off the grant and
// leaves the grant for that Base to spend.
func TestAGrantNamesItsOrgWithoutBeingSpent(t *testing.T) {
	fresh := streamGrants.mint(&grant{org: "acme", deadline: time.Now().Add(grantTTL)})
	stale := streamGrants.mint(&grant{org: "acme", deadline: time.Now().Add(-time.Second)})
	anon := streamGrants.mint(&grant{deadline: time.Now().Add(grantTTL)})

	if org, ok := StreamGrantOrg(fresh); !ok || org != "acme" {
		t.Fatalf("a fresh grant answered %q, %v", org, ok)
	}
	if streamGrants.spend(fresh) == nil {
		t.Fatal("asking the org spent the grant")
	}
	if _, ok := StreamGrantOrg(fresh); ok {
		t.Error("a spent grant still names an org")
	}
	if _, ok := StreamGrantOrg(stale); ok {
		t.Error("an expired grant still names an org")
	}
	if _, ok := StreamGrantOrg(anon); ok {
		t.Error("a grant minted in no org named one")
	}
	if _, ok := StreamGrantOrg("never-minted"); ok {
		t.Error("an unknown grant named an org")
	}
}

// A stream the host already routed to its org's Base is served by that Base,
// the live one, and not by the instance the grant was minted on: a pool may
// close an org's Base between the mint and the stream and open it again.
func TestAGrantKeepsTheBaseTheHostRouted(t *testing.T) {
	minted := core.NewBaseApp(core.BaseAppConfig{DataDir: t.TempDir()})
	reopened := core.NewBaseApp(core.BaseAppConfig{DataDir: t.TempDir()})
	process := core.NewBaseApp(core.BaseAppConfig{DataDir: t.TempDir()})

	minted.Store().Set(StoreKeyTenant, "acme")
	reopened.Store().Set(StoreKeyTenant, "acme")

	g := &grant{app: minted, org: "acme", deadline: time.Now().Add(grantTTL)}

	routed := &core.RequestEvent{App: reopened}
	routed.Request = httptest.NewRequest(http.MethodGet, "/v1/realtime", nil)
	g.apply(routed)
	if routed.App != reopened {
		t.Fatal("the grant replaced the org's live Base with the one it was minted on")
	}

	unrouted := &core.RequestEvent{App: process}
	unrouted.Request = httptest.NewRequest(http.MethodGet, "/v1/realtime", nil)
	g.apply(unrouted)
	if unrouted.App != minted {
		t.Fatal("a stream nobody routed was not served by the Base that minted its grant")
	}
}
