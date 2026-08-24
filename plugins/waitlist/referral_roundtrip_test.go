// Copyright (C) 2020-2026, Hanzo AI Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package waitlist

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/hanzoai/base/apis"
)

// entryWire decodes what entryView writes. The handler returns a map, so the
// shape is asserted here rather than shared with production code — a test that
// declares the fields it depends on fails when one is renamed, which is the
// point.
type entryWire struct {
	OK            bool   `json:"ok"`
	Email         string `json:"email"`
	RefCode       string `json:"refCode"`
	ShareURL      string `json:"shareUrl"`
	Rank          int    `json:"rank"`
	Points        int    `json:"points"`
	ReferralCount int    `json:"referralCount"`
	AlreadyJoined bool   `json:"alreadyJoined"`
}

// newWaitlistServer mounts the plugin's real routes on a real Base app and
// serves them over HTTP. The sibling helper in plugin_test.go stops at the
// plugin; the referral path is worth proving through the router, because
// attribution arrives as a request body and the rank comes back as JSON.
func newWaitlistServer(t *testing.T) *httptest.Server {
	t.Helper()
	cfg := Config{Enabled: true, DefaultSlugs: []string{"launch"}, JoinRateLimit: -1}
	cfg.resolve() // point values come from the defaults, as they do in a real boot
	return newWaitlistServerWith(t, cfg)
}

// newWaitlistServerWith is newWaitlistServer over an explicit config, for the
// tests that vary what a referral is worth.
func newWaitlistServerWith(t *testing.T, cfg Config) *httptest.Server {
	t.Helper()

	p, _ := newWaitlistTestPlugin(t, cfg)

	r, err := apis.NewRouter(p.app)
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}
	p.registerRoutes(r)
	mux, err := r.BuildMux()
	if err != nil {
		t.Fatalf("BuildMux: %v", err)
	}
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func join(t *testing.T, srv *httptest.Server, email, referrerCode string) entryWire {
	t.Helper()
	body, _ := json.Marshal(joinRequest{Waitlist: "launch", Email: email, ReferrerCode: referrerCode})
	resp, err := http.Post(srv.URL+"/v1/waitlist/join", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST join(%s): %v", email, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("join(%s): status %d, want 200", email, resp.StatusCode)
	}
	var out entryWire
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode join(%s): %v", email, err)
	}
	return out
}

func status(t *testing.T, srv *httptest.Server, email string) entryWire {
	t.Helper()
	resp, err := http.Get(srv.URL + "/v1/waitlist/status?waitlist=launch&email=" + url.QueryEscape(email))
	if err != nil {
		t.Fatalf("GET status(%s): %v", email, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status(%s): status %d, want 200", email, resp.StatusCode)
	}
	var out entryWire
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode status(%s): %v", email, err)
	}
	return out
}

// TestReferralRoundTrip proves the attribution loop end-to-end over HTTP:
// join -> refCode + shareUrl -> a second join carrying that code -> the
// referrer's referralCount rises, it is paid in points, and the points move
// the rank. Rank is computed from POINTS, so the last step is what makes a
// referral worth anything to the person who made it.
func TestReferralRoundTrip(t *testing.T) {
	srv := newWaitlistServer(t)

	// A joins first, with no referrer.
	a := join(t, srv, "alice@example.com", "")
	if !a.OK || a.RefCode == "" {
		t.Fatalf("A join: ok=%v refCode=%q — want a fresh entry with a code", a.OK, a.RefCode)
	}
	if a.ReferralCount != 0 {
		t.Fatalf("A initial referralCount = %d, want 0", a.ReferralCount)
	}
	if !strings.Contains(a.ShareURL, "ref="+a.RefCode) {
		t.Fatalf("A shareUrl %q must carry ref=%s", a.ShareURL, a.RefCode)
	}

	// C joins too, also unreferred, so it is the control: same points as A
	// until a referral separates them.
	c := join(t, srv, "carol@example.com", "")
	if !c.OK || c.RefCode == "" {
		t.Fatalf("C join failed: %+v", c)
	}

	aBefore := status(t, srv, "alice@example.com")
	cBefore := status(t, srv, "carol@example.com")
	if aBefore.Points != cBefore.Points {
		t.Fatalf("pre-referral: A points %d != C points %d — the control is not level",
			aBefore.Points, cBefore.Points)
	}

	// B joins WITH A's refCode.
	b := join(t, srv, "bob@example.com", a.RefCode)
	if !b.OK || b.RefCode == "" {
		t.Fatalf("B join with referrer failed: %+v", b)
	}
	if b.RefCode == a.RefCode {
		t.Fatalf("B must get its OWN refCode, not the referrer's (%q)", b.RefCode)
	}

	// PROOF 1 — A's referralCount incremented 0 -> 1.
	aAfter := status(t, srv, "alice@example.com")
	if aAfter.ReferralCount != 1 {
		t.Fatalf("after B referred by A: A referralCount = %d, want 1", aAfter.ReferralCount)
	}

	// PROOF 2 — the referral was PAID. referralCount alone ranks nobody.
	if aAfter.Points <= aBefore.Points {
		t.Fatalf("A points did not rise on a referral: before=%d after=%d", aBefore.Points, aAfter.Points)
	}

	// PROOF 3 — and the points moved the rank: A leads the level control.
	cAfter := status(t, srv, "carol@example.com")
	if aAfter.Rank >= cAfter.Rank {
		t.Fatalf("post-referral: A rank %d must lead C rank %d (A was paid a referral, C was not)",
			aAfter.Rank, cAfter.Rank)
	}
	if aAfter.Rank != 1 {
		t.Fatalf("A should be #1 as the only entry paid a referral; got rank %d", aAfter.Rank)
	}

	// PROOF 4 — re-joining is idempotent and pays nobody twice.
	aRejoin := join(t, srv, "alice@example.com", "")
	if !aRejoin.AlreadyJoined {
		t.Fatalf("re-join of A must set alreadyJoined=true")
	}
	if aRejoin.RefCode != a.RefCode {
		t.Fatalf("re-join of A returned a different refCode: %q != %q", aRejoin.RefCode, a.RefCode)
	}
	if aRejoin.ReferralCount != 1 {
		t.Fatalf("re-join of A must NOT change referralCount: got %d, want 1", aRejoin.ReferralCount)
	}
	if aRejoin.Points != aAfter.Points {
		t.Fatalf("re-join of A must NOT change points: got %d, want %d", aRejoin.Points, aAfter.Points)
	}

	// PROOF 5 — an unknown referrer code is ignored, not rejected.
	d := join(t, srv, "dave@example.com", "NOSUCHCODE")
	if !d.OK {
		t.Fatalf("join with an unknown referrer code must still succeed: %+v", d)
	}
}
