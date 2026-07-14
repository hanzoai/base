// Copyright (C) 2020-2026, Hanzo AI Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package waitlist

import (
	"reflect"
	"testing"
	"time"

	"github.com/hanzoai/base/core"
	"github.com/hanzoai/base/tests"
	"github.com/hanzoai/base/tools/types"
	luxlog "github.com/luxfi/log"
)

func TestRefCodeUniqueness(t *testing.T) {
	seen := map[string]struct{}{}
	for i := 0; i < 5000; i++ {
		c := generateRefCode()
		if len(c) != 8 {
			t.Fatalf("ref code length = %d, want 8", len(c))
		}
		for _, r := range c {
			if !contains(refCodeAlphabet, byte(r)) {
				t.Fatalf("ref code %q contains illegal char %q", c, r)
			}
		}
		if _, dup := seen[c]; dup {
			t.Fatalf("collision after %d codes (alphabet too small or RNG broken)", i)
		}
		seen[c] = struct{}{}
	}
}

func TestEmailValidation(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"a@b.co", true},
		{"foo+bar@example.com", true},
		{"", false},
		{"no-at-sign", false},
		{"a@b", false},
		{"@b.co", false},
	}
	for _, c := range cases {
		if got := isValidEmail(c.in); got != c.want {
			t.Errorf("isValidEmail(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestEmailDomain(t *testing.T) {
	if got := emailDomain("Foo@Example.COM"); got != "example.com" {
		t.Errorf("emailDomain lowercase failed: %q", got)
	}
	if got := emailDomain("no-at"); got != "" {
		t.Errorf("emailDomain bad input: %q", got)
	}
}

func TestSlidingLimiter(t *testing.T) {
	now := time.Unix(1000, 0)
	l := newSlidingLimiter(3, time.Minute)
	l.now = func() time.Time { return now }

	for i := 0; i < 3; i++ {
		if !l.allow("ip-a") {
			t.Fatalf("hit %d should be allowed", i)
		}
	}
	if l.allow("ip-a") {
		t.Fatal("4th hit must be denied")
	}
	if !l.allow("ip-b") {
		t.Fatal("different key must be allowed")
	}

	// Advance past window — counter resets.
	now = now.Add(2 * time.Minute)
	if !l.allow("ip-a") {
		t.Fatal("after window expiry, should be allowed")
	}
}

func TestSlidingLimiterDisabled(t *testing.T) {
	l := newSlidingLimiter(0, time.Minute) // limit=0 disables
	for i := 0; i < 100; i++ {
		if !l.allow("x") {
			t.Fatalf("disabled limiter should always allow")
		}
	}
}

func TestConfigResolveDefaults(t *testing.T) {
	c := Config{Enabled: true}
	c.resolve()
	if c.JoinRateLimit != 5 {
		t.Errorf("default rate limit = %d, want 5", c.JoinRateLimit)
	}
	if c.JoinRateWindow != time.Hour {
		t.Errorf("default window = %v, want 1h", c.JoinRateWindow)
	}
	if c.waitlistsCollection() != "waitlists" {
		t.Errorf("default collection name = %q", c.waitlistsCollection())
	}
}

func TestConfigCollectionPrefix(t *testing.T) {
	c := Config{Enabled: true, CollectionPrefix: "tenant1"}
	if err := c.validate(); err != nil {
		t.Errorf("alphanumeric prefix should validate: %v", err)
	}
	if c.waitlistsCollection() != "tenant1_waitlists" {
		t.Errorf("prefixed collection name = %q", c.waitlistsCollection())
	}

	c = Config{Enabled: true, CollectionPrefix: "Bad-Prefix"}
	if err := c.validate(); err == nil {
		t.Error("prefix with uppercase/hyphen should be rejected")
	}
}

func TestConfigResolveOpen(t *testing.T) {
	cases := []struct {
		name string
		env  string
		want bool
	}{
		{"one", "1", true},
		{"true", "true", true},
		{"true_upper", "TRUE", true},
		{"yes_mixed", "Yes", true},
		{"empty", "", false},
		{"false", "false", false},
		{"no", "no", false},
		{"zero", "0", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Setenv("WAITLIST_OPEN", c.env)
			cfg := Config{Enabled: true}
			cfg.resolve()
			if cfg.Open != c.want {
				t.Errorf("WAITLIST_OPEN=%q -> Open=%v, want %v", c.env, cfg.Open, c.want)
			}
		})
	}
}

func TestConfigResolveAccessCapacity(t *testing.T) {
	cases := []struct {
		name string
		env  string
		want int
	}{
		{"number", "10", 10},
		{"zero", "0", 0},
		{"empty", "", 0},
		{"invalid", "abc", 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Setenv("WAITLIST_ACCESS_CAPACITY", c.env)
			cfg := Config{Enabled: true}
			cfg.resolve()
			if cfg.AccessCapacity != c.want {
				t.Errorf("WAITLIST_ACCESS_CAPACITY=%q -> %d, want %d", c.env, cfg.AccessCapacity, c.want)
			}
		})
	}
}

func TestConfigResolveDefaultSlugs(t *testing.T) {
	cases := []struct {
		name string
		env  string
		want []string
	}{
		{"empty", "", nil},
		{"single", "hanzod", []string{"hanzod"}},
		{"multi_trim_skip_blanks", " hanzod , zoo ,, lux ", []string{"hanzod", "zoo", "lux"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Setenv("WAITLIST_DEFAULT_SLUGS", c.env)
			cfg := Config{Enabled: true}
			cfg.resolve()
			if !reflect.DeepEqual(cfg.DefaultSlugs, c.want) {
				t.Errorf("WAITLIST_DEFAULT_SLUGS=%q -> %v, want %v", c.env, cfg.DefaultSlugs, c.want)
			}
		})
	}
}

func TestGrantsAccess(t *testing.T) {
	cases := []struct {
		name     string
		open     bool
		granted  bool
		capacity int
		rank     int
		want     bool
	}{
		{"open always", true, false, 0, 999, true},
		{"granted sticky", false, true, 0, 999, true},
		{"within capacity", false, false, 5, 3, true},
		{"at capacity edge", false, false, 5, 5, true},
		{"beyond capacity", false, false, 5, 6, false},
		{"closed no grant no cap", false, false, 0, 1, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := grantsAccess(c.open, c.granted, c.capacity, c.rank); got != c.want {
				t.Errorf("grantsAccess(%v,%v,%d,%d) = %v, want %v",
					c.open, c.granted, c.capacity, c.rank, got, c.want)
			}
		})
	}
}

// --- DB-backed harness ---

func newWaitlistTestPlugin(t *testing.T, cfg Config) (*plugin, *tests.TestApp) {
	t.Helper()
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("NewTestApp: %v", err)
	}
	t.Cleanup(app.Cleanup)

	if cfg.JoinRateWindow == 0 {
		cfg.JoinRateWindow = time.Hour
	}
	p := &plugin{
		app:        app,
		config:     cfg,
		logger:     luxlog.New("component", "waitlist-test"),
		limiter:    newSlidingLimiter(cfg.JoinRateLimit, cfg.JoinRateWindow),
		turnstile:  newTurnstileVerifier(cfg.TurnstileSecret),
		disposable: newDomainSet(defaultDisposableDomains),
	}
	if err := p.ensureSchema(); err != nil {
		t.Fatalf("ensureSchema: %v", err)
	}
	return p, app
}

func seedWaitlist(t *testing.T, app *tests.TestApp, slug string) string {
	t.Helper()
	col, err := app.FindCollectionByNameOrId("waitlists")
	if err != nil {
		t.Fatalf("waitlists collection: %v", err)
	}
	rec := core.NewRecord(col)
	rec.Set("slug", slug)
	rec.Set("name", titleSlug(slug))
	if err := app.Save(rec); err != nil {
		t.Fatalf("save waitlist: %v", err)
	}
	return rec.Id
}

// seedEntry creates an entry with an explicit createdAt. SetRaw is used
// because AutodateField.FindSetter returns a noop for record.Set, and the
// create interceptor only stamps "now" when the value equals the zero
// last-known value — a pre-set non-zero value is preserved.
func seedEntry(t *testing.T, app *tests.TestApp, wlID, email, refCode string, referralCount, points float64, created time.Time) *core.Record {
	t.Helper()
	col, err := app.FindCollectionByNameOrId("waitlist_entries")
	if err != nil {
		t.Fatalf("entries collection: %v", err)
	}
	dt, err := types.ParseDateTime(created)
	if err != nil {
		t.Fatalf("parse createdAt: %v", err)
	}
	rec := core.NewRecord(col)
	rec.Set("waitlist", wlID)
	rec.Set("email", email)
	rec.Set("refCode", refCode)
	rec.Set("referralCount", referralCount)
	rec.Set("points", points)
	rec.Set("accessGranted", false)
	rec.SetRaw("createdAt", dt)
	if err := app.Save(rec); err != nil {
		t.Fatalf("save entry: %v", err)
	}
	return rec
}

func mustRank(t *testing.T, p *plugin, wlID string, entry *core.Record) (int, int) {
	t.Helper()
	rank, total, err := p.competitionRank(p.app, wlID, entry.GetFloat("points"))
	if err != nil {
		t.Fatalf("competitionRank: %v", err)
	}
	return rank, total
}

func TestPointsRankOrdering(t *testing.T) {
	p, app := newWaitlistTestPlugin(t, Config{})
	wlID := seedWaitlist(t, app, "alpha")

	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	a := seedEntry(t, app, wlID, "a@x.com", "AAAA", 1, 0, base)
	b := seedEntry(t, app, wlID, "b@x.com", "BBBB", 1, 0, base.Add(time.Hour))

	// Equal points: competition rank is shared — nobody has strictly more, so
	// both sit at rank 1 (standard leaderboard semantics).
	if rankA, _ := mustRank(t, p, wlID, a); rankA != 1 {
		t.Fatalf("A rank = %d, want 1", rankA)
	}
	if rankB, total := mustRank(t, p, wlID, b); rankB != 1 || total != 2 {
		t.Fatalf("B rank = %d total = %d, want rank 1 total 2", rankB, total)
	}

	// Award B one point: points ARE position, so B (1) now outranks A (0) — the
	// single number decides the order, no separate boost store.
	b.Set("points", 1)
	if err := app.Save(b); err != nil {
		t.Fatalf("award B: %v", err)
	}
	if rankB, _ := mustRank(t, p, wlID, b); rankB != 1 {
		t.Fatalf("B rank after point = %d, want 1", rankB)
	}
	if rankA, total := mustRank(t, p, wlID, a); rankA != 2 || total != 2 {
		t.Fatalf("A rank = %d total = %d, want rank 2 total 2", rankA, total)
	}
}

func TestHasAccessCapacityStickyPersist(t *testing.T) {
	p, app := newWaitlistTestPlugin(t, Config{AccessCapacity: 1})
	wlID := seedWaitlist(t, app, "alpha")
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	e := seedEntry(t, app, wlID, "a@x.com", "AAAA", 0, 0, base)

	// rank 1 <= capacity 1: access granted AND persisted (sticky).
	if !p.hasAccess(e, 1) {
		t.Fatal("rank within capacity must have access")
	}
	if !e.GetBool("accessGranted") {
		t.Fatal("capacity grant must set accessGranted in-memory")
	}
	reloaded, err := app.FindRecordById("waitlist_entries", e.Id)
	if err != nil {
		t.Fatalf("reload entry: %v", err)
	}
	if !reloaded.GetBool("accessGranted") {
		t.Fatal("capacity grant must persist accessGranted (sticky)")
	}

	// Sticky: even far outside the capacity window, access stays true.
	if !p.hasAccess(reloaded, 999) {
		t.Fatal("granted access must stick past the capacity window")
	}
}

func TestHasAccessOpenAndClosed(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	// Open: any rank has access, and the open switch does not sticky-persist.
	pOpen, appOpen := newWaitlistTestPlugin(t, Config{Open: true})
	wlOpen := seedWaitlist(t, appOpen, "alpha")
	eOpen := seedEntry(t, appOpen, wlOpen, "a@x.com", "AAAA", 0, 0, base)
	if !pOpen.hasAccess(eOpen, 999) {
		t.Fatal("open waitlist must grant access at any rank")
	}
	if eOpen.GetBool("accessGranted") {
		t.Fatal("open switch must not sticky-persist a per-entry grant")
	}

	// Closed, no capacity, not granted: denied.
	pClosed, appClosed := newWaitlistTestPlugin(t, Config{})
	wlClosed := seedWaitlist(t, appClosed, "alpha")
	eClosed := seedEntry(t, appClosed, wlClosed, "a@x.com", "AAAA", 0, 0, base)
	if pClosed.hasAccess(eClosed, 1) {
		t.Fatal("closed waitlist with zero capacity must deny access")
	}
}

func TestEnsureDefaultWaitlists(t *testing.T) {
	p, app := newWaitlistTestPlugin(t, Config{DefaultSlugs: []string{"hanzod", "zoo"}})
	if err := p.ensureDefaultWaitlists(); err != nil {
		t.Fatalf("ensureDefaultWaitlists: %v", err)
	}
	// Idempotent: a second call must not error or duplicate.
	if err := p.ensureDefaultWaitlists(); err != nil {
		t.Fatalf("ensureDefaultWaitlists (2nd): %v", err)
	}
	hanzod, err := app.FindFirstRecordByData("waitlists", "slug", "hanzod")
	if err != nil {
		t.Fatalf("seeded slug hanzod missing: %v", err)
	}
	if hanzod.GetString("name") != "Hanzod" {
		t.Fatalf("seeded name = %q, want Hanzod", hanzod.GetString("name"))
	}
	count, err := app.CountRecords("waitlists")
	if err != nil {
		t.Fatalf("count waitlists: %v", err)
	}
	if count != 2 {
		t.Fatalf("waitlists count = %d, want 2 (idempotent seed)", count)
	}
}

func contains(s string, b byte) bool {
	for i := 0; i < len(s); i++ {
		if s[i] == b {
			return true
		}
	}
	return false
}
