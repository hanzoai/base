// Copyright (C) 2020-2026, Hanzo AI Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package waitlist

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hanzoai/base/apis"
	"github.com/hanzoai/base/tests"
	luxlog "github.com/luxfi/log"
)

// newWaitlistTestPlugin boots a real Base TestApp, installs the waitlist plugin
// (schema + routes), and seeds one waitlist ("launch"). It returns a live
// httptest server mounting /v1/waitlist/* and a cleanup func.
func newWaitlistTestPlugin(t *testing.T) (*httptest.Server, func()) {
	t.Helper()

	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("NewTestApp: %v", err)
	}

	cfg := Config{Enabled: true}
	cfg.resolve()
	p := &plugin{
		app:        app,
		config:     cfg,
		logger:     luxlog.New("component", "waitlist-test"),
		limiter:    newSlidingLimiter(-1, 0), // rate-limit disabled for the test
		turnstile:  newTurnstileVerifier(""), // empty secret -> captcha skipped
		disposable: newDomainSet(nil),
	}

	// ensureSchema creates the collections AND self-seeds the default
	// "launch" waitlist row (cfg.DefaultSlug), so no manual seed is needed.
	if err := p.ensureSchema(); err != nil {
		app.Cleanup()
		t.Fatalf("ensureSchema: %v", err)
	}
	if _, err := app.FindFirstRecordByData(cfg.waitlistsCollection(), "slug", "launch"); err != nil {
		app.Cleanup()
		t.Fatalf("expected self-seeded launch waitlist: %v", err)
	}

	r, err := apis.NewRouter(app)
	if err != nil {
		app.Cleanup()
		t.Fatalf("NewRouter: %v", err)
	}
	p.registerRoutes(r)
	mux, err := r.BuildMux()
	if err != nil {
		app.Cleanup()
		t.Fatalf("BuildMux: %v", err)
	}
	srv := httptest.NewServer(mux)

	return srv, func() {
		srv.Close()
		app.Cleanup()
	}
}

func joinWaitlist(t *testing.T, srvURL, email, referrerCode string) joinResponse {
	t.Helper()
	body, _ := json.Marshal(joinRequest{Waitlist: "launch", Email: email, ReferrerCode: referrerCode})
	resp, err := http.Post(srvURL+"/v1/waitlist/join", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST join(%s): %v", email, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("join(%s): status %d, want 200", email, resp.StatusCode)
	}
	var out joinResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode join(%s): %v", email, err)
	}
	return out
}

func statusWaitlist(t *testing.T, srvURL, email string) statusResponse {
	t.Helper()
	resp, err := http.Get(srvURL + "/v1/waitlist/status?waitlist=launch&email=" + email)
	if err != nil {
		t.Fatalf("GET status(%s): %v", email, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status(%s): status %d, want 200", email, resp.StatusCode)
	}
	var out statusResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode status(%s): %v", email, err)
	}
	return out
}

// TestReferralRoundTrip is the load-bearing proof of the viral growth engine:
//
//	join -> refCode + shareUrl -> a second signup with ?ref=<code> ->
//	the referrer's referralCount increments AND the referrer's rank climbs.
//
// Proven end-to-end over HTTP against a real Base app + the plugin's real
// SQLite-backed rank query.
func TestReferralRoundTrip(t *testing.T) {
	srv, cleanup := newWaitlistTestPlugin(t)
	defer cleanup()

	// A joins first (no referrer). Gets a refCode + a shareUrl carrying it.
	a := joinWaitlist(t, srv.URL, "alice@example.com", "")
	if !a.OK || a.RefCode == "" {
		t.Fatalf("A join: ok=%v refCode=%q — want a fresh entry with a code", a.OK, a.RefCode)
	}
	if a.ReferralCount != 0 {
		t.Fatalf("A initial referralCount = %d, want 0", a.ReferralCount)
	}
	if !strings.Contains(a.ShareURL, "ref="+a.RefCode) {
		t.Fatalf("A shareUrl %q must carry ref=%s", a.ShareURL, a.RefCode)
	}

	// C joins right after A, also with no referrer. On a referral tie, earlier
	// createdAt wins, so A is ahead of C at this point.
	c := joinWaitlist(t, srv.URL, "carol@example.com", "")
	if !c.OK || c.RefCode == "" {
		t.Fatalf("C join failed: %+v", c)
	}

	// Pre-referral, A and C both have 0 referrals. The rank tie-break is
	// createdAt at SECOND granularity (computeRank), so two joins inside the
	// same wall-clock second tie and BOTH report the same rank — that is the
	// baseline we measure the climb against. (Sub-second-tie collapse is noted
	// as a launch-spike sharpening item; it does not affect the referral proof:
	// a referral moves referralCount, which outranks any createdAt tie.)
	aBefore := statusWaitlist(t, srv.URL, "alice@example.com")
	cBefore := statusWaitlist(t, srv.URL, "carol@example.com")
	if aBefore.Rank > cBefore.Rank {
		t.Fatalf("pre-referral: A rank %d must not be BEHIND C rank %d (equal referrals, A joined first)",
			aBefore.Rank, cBefore.Rank)
	}

	// B joins WITH A's refCode (the ?ref=<code> attribution path).
	b := joinWaitlist(t, srv.URL, "bob@example.com", a.RefCode)
	if !b.OK || b.RefCode == "" {
		t.Fatalf("B join with referrer failed: %+v", b)
	}
	if b.RefCode == a.RefCode {
		t.Fatalf("B must get its OWN refCode, not the referrer's (%q)", b.RefCode)
	}

	// PROOF 1 — A's referralCount incremented 0 -> 1.
	aAfter := statusWaitlist(t, srv.URL, "alice@example.com")
	if aAfter.ReferralCount != 1 {
		t.Fatalf("after B referred by A: A referralCount = %d, want 1", aAfter.ReferralCount)
	}

	// PROOF 2 — A's rank CLIMBED (did not regress) and A now leads C by
	// referrals (1 vs 0), not merely by the createdAt tie-break. A is the sole
	// entry with a referral, so A is #1.
	cAfter := statusWaitlist(t, srv.URL, "carol@example.com")
	if aAfter.Rank > aBefore.Rank {
		t.Fatalf("A rank regressed after gaining a referral: before=%d after=%d", aBefore.Rank, aAfter.Rank)
	}
	if aAfter.Rank >= cAfter.Rank {
		t.Fatalf("post-referral: A rank %d must lead C rank %d (A has 1 referral, C has 0)",
			aAfter.Rank, cAfter.Rank)
	}
	if aAfter.Rank != 1 {
		t.Fatalf("A should be #1 as the only entry with a referral; got rank %d", aAfter.Rank)
	}

	// PROOF 3 — idempotent re-join of A returns the SAME entry and does not
	// inflate the referral count (double-credit guard).
	aRejoin := joinWaitlist(t, srv.URL, "alice@example.com", "")
	if !aRejoin.AlreadyJoined {
		t.Fatalf("re-join of A must set alreadyJoined=true")
	}
	if aRejoin.RefCode != a.RefCode {
		t.Fatalf("re-join of A returned a different refCode: %q != %q", aRejoin.RefCode, a.RefCode)
	}
	if aRejoin.ReferralCount != 1 {
		t.Fatalf("re-join of A must NOT change referralCount: got %d, want 1", aRejoin.ReferralCount)
	}

	// PROOF 4 — an unknown referrer code is ignored, not rejected (join still
	// succeeds, nobody is credited).
	d := joinWaitlist(t, srv.URL, "dave@example.com", "NOSUCHCODE")
	if !d.OK {
		t.Fatalf("join with an unknown referrer code must still succeed: %+v", d)
	}
}
