// Copyright (C) 2020-2026, Hanzo AI Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package waitlist

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hanzoai/base/apis"
	"github.com/hanzoai/base/core"
	"github.com/hanzoai/base/tests"
	luxlog "github.com/luxfi/log"
)

const testAwardSecret = "award-secret-xyz"
const testAdminSecret = "admin-secret-abc"

func newPointsTestPlugin(t *testing.T) (*plugin, *tests.TestApp, func()) {
	t.Helper()
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("NewTestApp: %v", err)
	}
	cfg := Config{
		Enabled:       true,
		AwardSecret:   testAwardSecret,
		AdminSecret:   testAdminSecret,
		JoinRateLimit: 100000, // don't let the limiter flake the flow
	}
	cfg.resolve()
	p := &plugin{
		app:        app,
		config:     cfg,
		logger:     luxlog.New("component", "waitlist-test"),
		limiter:    newSlidingLimiter(cfg.JoinRateLimit, cfg.JoinRateWindow),
		turnstile:  newTurnstileVerifier(""),
		disposable: newDomainSet(defaultDisposableDomains),
	}
	if err := p.ensureSchema(); err != nil {
		app.Cleanup()
		t.Fatalf("ensureSchema: %v", err)
	}
	// Seed a waitlist "demo".
	wlCol, err := app.FindCollectionByNameOrId(p.config.waitlistsCollection())
	if err != nil {
		app.Cleanup()
		t.Fatalf("find waitlists: %v", err)
	}
	wl := core.NewRecord(wlCol)
	wl.Set("slug", "demo")
	wl.Set("name", "Demo")
	if err := app.Save(wl); err != nil {
		app.Cleanup()
		t.Fatalf("save waitlist: %v", err)
	}
	return p, app, app.Cleanup
}

func serve(t *testing.T, p *plugin, app *tests.TestApp) *httptest.Server {
	t.Helper()
	r, err := apis.NewRouter(app)
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}
	p.registerRoutes(r)
	mux, err := r.BuildMux()
	if err != nil {
		t.Fatalf("BuildMux: %v", err)
	}
	return httptest.NewServer(mux)
}

func do(t *testing.T, method, url, token string, body any) (*http.Response, map[string]any) {
	t.Helper()
	var reader *bytes.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		reader = bytes.NewReader(b)
	} else {
		reader = bytes.NewReader(nil)
	}
	req, err := http.NewRequest(method, url, reader)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, url, err)
	}
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	resp.Body.Close()
	return resp, out
}

func num(t *testing.T, m map[string]any, key string) int {
	t.Helper()
	v, ok := m[key]
	if !ok {
		t.Fatalf("missing key %q in %v", key, m)
	}
	f, ok := v.(float64)
	if !ok {
		t.Fatalf("key %q is %T not number", key, v)
	}
	return int(f)
}

func breakdown(t *testing.T, m map[string]any, cat string) int {
	t.Helper()
	bd, ok := m["pointBreakdown"].(map[string]any)
	if !ok {
		t.Fatalf("no pointBreakdown in %v", m)
	}
	f, _ := bd[cat].(float64)
	return int(f)
}

func TestPointsEndToEnd(t *testing.T) {
	p, app, cleanup := newPointsTestPlugin(t)
	defer cleanup()
	srv := serve(t, p, app)
	defer srv.Close()
	base := srv.URL + "/v1/waitlist"

	// alice joins (no referrer): rank 1, 0 points.
	_, alice := do(t, "POST", base+"/join", "", map[string]any{"waitlist": "demo", "email": "alice@example.com"})
	if !alice["ok"].(bool) {
		t.Fatalf("alice join failed: %v", alice)
	}
	if num(t, alice, "rank") != 1 || num(t, alice, "points") != 0 {
		t.Fatalf("alice rank/points = %d/%d, want 1/0", num(t, alice, "rank"), num(t, alice, "points"))
	}
	aliceRef, _ := alice["refCode"].(string)
	if aliceRef == "" {
		t.Fatal("alice has no refCode")
	}

	// bob joins via alice's code: alice earns REFERRAL(10).
	_, bob := do(t, "POST", base+"/join", "", map[string]any{"waitlist": "demo", "email": "bob@example.com", "referrerCode": aliceRef})
	bobRef, _ := bob["refCode"].(string)

	// alice status: 10 points from 1 referral, rank 1.
	_, aliceStatus := do(t, "GET", base+"/status?waitlist=demo&email=alice@example.com", "", nil)
	if got := num(t, aliceStatus, "points"); got != 10 {
		t.Fatalf("alice points after referral = %d, want 10", got)
	}
	if got := breakdown(t, aliceStatus, "referrals"); got != 10 {
		t.Fatalf("alice referrals breakdown = %d, want 10", got)
	}
	if num(t, aliceStatus, "referralCount") != 1 {
		t.Fatalf("alice referralCount = %d, want 1", num(t, aliceStatus, "referralCount"))
	}
	if num(t, aliceStatus, "rank") != 1 {
		t.Fatalf("alice rank = %d, want 1 (most points)", num(t, aliceStatus, "rank"))
	}

	// alice shares on x: +2. Same platform same day again: deduped (0).
	_, share1 := do(t, "POST", base+"/track-share", "", map[string]any{"waitlist": "demo", "refCode": aliceRef, "platform": "x"})
	if num(t, share1, "awarded") != 2 || share1["alreadyClaimed"].(bool) {
		t.Fatalf("share1 = %v, want awarded 2 not claimed", share1)
	}
	_, share2 := do(t, "POST", base+"/track-share", "", map[string]any{"waitlist": "demo", "refCode": aliceRef, "platform": "x"})
	if num(t, share2, "awarded") != 0 || !share2["alreadyClaimed"].(bool) {
		t.Fatalf("share2 = %v, want awarded 0 alreadyClaimed", share2)
	}
	if num(t, share2, "points") != 12 {
		t.Fatalf("alice points after share = %d, want 12", num(t, share2, "points"))
	}

	// alice invites carol: +1 invite_sent. Then carol joins via alice ->
	// alice earns REFERRAL(10) + INVITE_CONVERTED(5).
	_, inv := do(t, "POST", base+"/invite", "", map[string]any{"waitlist": "demo", "refCode": aliceRef, "emails": []string{"carol@example.com"}})
	if num(t, inv, "sent") != 1 || num(t, inv, "pointsAwarded") != 1 {
		t.Fatalf("invite = %v, want sent 1", inv)
	}
	do(t, "POST", base+"/join", "", map[string]any{"waitlist": "demo", "email": "carol@example.com", "referrerCode": aliceRef})
	_, aliceAfter := do(t, "GET", base+"/status?waitlist=demo&email=alice@example.com", "", nil)
	// 10 (bob referral) + 2 (share) + 1 (invite) + 10 (carol referral) + 5 (carol conversion) = 28
	if got := num(t, aliceAfter, "points"); got != 28 {
		t.Fatalf("alice total points = %d, want 28 (10+2+1+10+5); breakdown=%v", got, aliceAfter["pointBreakdown"])
	}
	if got := breakdown(t, aliceAfter, "invitesConverted"); got != 5 {
		t.Fatalf("alice invitesConverted = %d, want 5", got)
	}

	// --- award seam (server-to-server) ---
	// no bearer -> 401.
	resp, _ := do(t, "POST", base+"/award", "", map[string]any{"waitlist": "demo", "email": "bob@example.com", "source": "social:x:follow"})
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("award without secret = %d, want 401", resp.StatusCode)
	}
	// with bearer -> bob earns SOCIAL(15).
	_, aw1 := do(t, "POST", base+"/award", testAwardSecret, map[string]any{"waitlist": "demo", "email": "bob@example.com", "source": "social:x:follow"})
	if num(t, aw1, "awarded") != 15 || aw1["alreadyAwarded"].(bool) {
		t.Fatalf("award1 = %v, want awarded 15", aw1)
	}
	// replay same source -> deduped.
	_, aw2 := do(t, "POST", base+"/award", testAwardSecret, map[string]any{"waitlist": "demo", "email": "bob@example.com", "source": "social:x:follow"})
	if num(t, aw2, "awarded") != 0 || !aw2["alreadyAwarded"].(bool) {
		t.Fatalf("award2 = %v, want deduped", aw2)
	}
	// a different network is a distinct award.
	_, aw3 := do(t, "POST", base+"/award", testAwardSecret, map[string]any{"waitlist": "demo", "email": "bob@example.com", "source": "social:discord:join"})
	if num(t, aw3, "awarded") != 15 {
		t.Fatalf("award3 (discord) = %v, want awarded 15", aw3)
	}
	if got := num(t, aw3, "points"); got != 30 {
		t.Fatalf("bob points after 2 socials = %d, want 30", got)
	}
	_ = bobRef

	// --- neighborhood ---
	_, nb := do(t, "GET", base+"/neighborhood?waitlist=demo&email=bob@example.com&window=10", "", nil)
	entries, _ := nb["entries"].([]any)
	if len(entries) == 0 {
		t.Fatal("neighborhood empty")
	}
	foundMe := false
	for _, raw := range entries {
		row := raw.(map[string]any)
		if isMe, _ := row["isMe"].(bool); isMe {
			foundMe = true
			if row["email"] == "bob@example.com" {
				t.Fatal("neighborhood must mask emails")
			}
		}
	}
	if !foundMe {
		t.Fatal("neighborhood missing the pivot (isMe)")
	}

	// --- list (leaderboard) ordered by points desc, alice on top ---
	_, list := do(t, "GET", base+"/list?waitlist=demo&pageSize=10", "", nil)
	le, _ := list["entries"].([]any)
	if len(le) < 3 {
		t.Fatalf("list has %d entries, want >=3", len(le))
	}
	// bob overtook alice: 2 social awards (15+15=30) > alice's 28 — points ARE
	// position, so the leaderboard reorders automatically.
	top := le[0].(map[string]any)
	if num(t, top, "rank") != 1 || num(t, top, "points") != 30 {
		t.Fatalf("list top = rank %v points %v, want 1/30 (bob after 2 socials)", top["rank"], top["points"])
	}
	if top["refCode"] != nil {
		t.Fatal("non-admin list must not expose refCode")
	}

	// --- activity feed non-empty, has join+referral+share+social ---
	_, act := do(t, "GET", base+"/activity?waitlist=demo&limit=50", "", nil)
	ae, _ := act["entries"].([]any)
	if len(ae) == 0 {
		t.Fatal("activity feed empty")
	}
	types := map[string]bool{}
	for _, raw := range ae {
		types[raw.(map[string]any)["type"].(string)] = true
	}
	for _, want := range []string{"join", "referral", "share", "social"} {
		if !types[want] {
			t.Fatalf("activity feed missing type %q (have %v)", want, types)
		}
	}

	// --- export (admin) has points column ---
	req, _ := http.NewRequest("GET", base+"/export?waitlist=demo", nil)
	req.Header.Set("Authorization", "Bearer "+testAdminSecret)
	xresp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	buf := new(bytes.Buffer)
	_, _ = buf.ReadFrom(xresp.Body)
	xresp.Body.Close()
	if xresp.StatusCode != http.StatusOK {
		t.Fatalf("export status = %d", xresp.StatusCode)
	}
	if !bytes.Contains(buf.Bytes(), []byte("points")) || !bytes.Contains(buf.Bytes(), []byte("alice@example.com")) {
		t.Fatalf("export missing points header or alice row:\n%s", buf.String())
	}
}

func TestJoinIdempotent(t *testing.T) {
	p, app, cleanup := newPointsTestPlugin(t)
	defer cleanup()
	srv := serve(t, p, app)
	defer srv.Close()
	base := srv.URL + "/v1/waitlist"

	_, a1 := do(t, "POST", base+"/join", "", map[string]any{"waitlist": "demo", "email": "dup@example.com"})
	_, a2 := do(t, "POST", base+"/join", "", map[string]any{"waitlist": "demo", "email": "dup@example.com"})
	if a2["alreadyJoined"] != true {
		t.Fatalf("second join should be alreadyJoined, got %v", a2)
	}
	if a1["refCode"] != a2["refCode"] {
		t.Fatal("idempotent join must return the same refCode")
	}
}

func TestGrantAndGuards(t *testing.T) {
	p, app, cleanup := newPointsTestPlugin(t)
	defer cleanup()
	srv := serve(t, p, app)
	defer srv.Close()
	base := srv.URL + "/v1/waitlist"

	do(t, "POST", base+"/join", "", map[string]any{"waitlist": "demo", "email": "vip@example.com"})

	// grant honors caller amount (the business lever).
	_, g := do(t, "POST", base+"/award", testAwardSecret, map[string]any{
		"waitlist": "demo", "email": "vip@example.com", "source": "grant", "points": 500, "dedupKey": "vip-boost-1",
	})
	if num(t, g, "awarded") != 500 || num(t, g, "points") != 500 {
		t.Fatalf("grant = %v, want 500", g)
	}
	// unknown source rejected.
	resp, _ := do(t, "POST", base+"/award", testAwardSecret, map[string]any{"waitlist": "demo", "email": "vip@example.com", "source": "bogus"})
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("bogus source = %d, want 400", resp.StatusCode)
	}
	// disposable email blocked on join.
	resp2, _ := do(t, "POST", base+"/join", "", map[string]any{"waitlist": "demo", "email": "x@mailinator.com"})
	if resp2.StatusCode != http.StatusBadRequest {
		t.Fatalf("disposable join = %d, want 400", resp2.StatusCode)
	}
}
