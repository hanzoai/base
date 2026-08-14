package apis

import (
	"net/url"
	"strings"
	"testing"
	"time"
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
