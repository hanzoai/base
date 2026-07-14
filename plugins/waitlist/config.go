// Copyright (C) 2020-2026, Hanzo AI Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package waitlist registers a viral, points-based waiting-list plugin on a
// Base app.
//
// The mechanic is one number: an entry's POINTS. Position on the list is the
// entry's competition rank by points (ORDER BY points DESC, earlier joiners
// break ties). Points are earned from events — referrals, shares, invites,
// verified social follows/joins, running hanzod, admin/service boosts — and
// every award is an append-only row in `waitlist_events`, so a
// UNIQUE(entry, dedupKey) index is the anti-fraud spine (one follow = one
// award). There is exactly one place points change: award().
//
// Access is a separate, orthogonal gate: an entry has access when the list is
// Open, when it was granted access (sticky), or when its points-derived rank
// falls within AccessCapacity.
//
// Endpoints under /v1/waitlist:
//
//	POST /join          register an entry, credit a referrer atomically
//	GET  /status        one entry's rank, points, per-source breakdown, access
//	GET  /neighborhood  the rank +/- window around an entry (the scalable view)
//	GET  /list          leaderboard page (top-N; masked emails)
//	GET  /activity      recent event feed
//	POST /track-share   award share points (deduped per platform per day)
//	POST /invite        award invite points; credit conversions on join
//	POST /boost         service-authed points boost (superuser/AdminSecret,
//	                    e.g. hanzod), a caller-amount award through the seam
//	POST /award         server-to-server award for a VERIFIED event
//	                    (social/hanzod), gated by AwardSecret — the seam the
//	                    cloud automations connectors call after verifying
//	GET  /export        admin-only CSV
//
// Backing storage is Base SQLite collections auto-created on bootstrap. No
// Redis, no external store: SQL transactions provide atomicity, an index on
// points provides O(log n) neighborhood seeks.
package waitlist

import (
	"errors"
	"os"
	"strconv"
	"strings"
	"time"
)

// PointValues is the award schedule — the currency the business controls.
// Every value is overridable from the environment so a consumer can tune the
// economy without a redeploy of the plugin.
type PointValues struct {
	Referral        int // someone joins via your refCode
	Share           int // a share click (per platform, per day)
	InviteSent      int // per valid email you invite
	InviteConverted int // an invited email actually joins
	Social          int // a verified social follow/join (X, Discord, ...)
	Hanzod          int // running a hanzod node (verified)
	Signup          int // a plain signup (default 0)
}

func (v *PointValues) resolve() {
	v.Referral = envInt("POINTS_REFERRAL", firstNonZero(v.Referral, 10))
	v.Share = envInt("POINTS_SHARE", firstNonZero(v.Share, 2))
	v.InviteSent = envInt("POINTS_INVITE_SENT", firstNonZero(v.InviteSent, 1))
	v.InviteConverted = envInt("POINTS_INVITE_CONVERTED", firstNonZero(v.InviteConverted, 5))
	v.Social = envInt("POINTS_SOCIAL", firstNonZero(v.Social, 15))
	v.Hanzod = envInt("POINTS_HANZOD", firstNonZero(v.Hanzod, 25))
	v.Signup = envInt("POINTS_SIGNUP", v.Signup) // default 0
}

// Config controls plugin registration.
type Config struct {
	// Enabled toggles the whole plugin. A zero-value Config is disabled;
	// callers must set Enabled:true explicitly to opt in.
	Enabled bool

	// CollectionPrefix lets multiple waitlist plugins coexist on one Base.
	// Default empty -> `waitlists`, `waitlist_entries`, `waitlist_events`.
	CollectionPrefix string

	// TurnstileSecret enables Cloudflare Turnstile verification on /join.
	// Resolved at boot from TURNSTILE_SECRET_KEY if empty.
	TurnstileSecret string

	// JoinRateLimit caps /join (and other public writes) by source IP. Zero ->
	// default 5/window. Negative disables.
	JoinRateLimit int

	// JoinRateWindow is the sliding window for JoinRateLimit. Zero -> 1h.
	JoinRateWindow time.Duration

	// AdminSecret guards /export and the service-authed /boost. Resolved from
	// WAITLIST_ADMIN_SECRET if empty. If still empty, those endpoints require a
	// superuser session (else 404).
	AdminSecret string

	// AwardSecret guards the server-to-server POST /award. Resolved from
	// WAITLIST_AWARD_SECRET if empty. If still empty, /award is disabled (404)
	// — a verified-event award can never be forged by a public client.
	AwardSecret string

	// Points is the award schedule. Zero values resolve to sane defaults /
	// the POINTS_* environment.
	Points PointValues

	// InviteMaxBatch caps emails per /invite call. Zero -> 50.
	InviteMaxBatch int

	// DisposableDomains, if non-nil, replaces the built-in disposable
	// e-mail blocklist. Empty slice disables blocking.
	DisposableDomains []string

	// DefaultSlugs are waitlist slugs seeded on bootstrap. Each becomes a
	// waitlist row (name = slug with its first letter upper-cased) if absent.
	// Resolved at boot from WAITLIST_DEFAULT_SLUGS (comma-separated). Empty
	// seeds nothing.
	DefaultSlugs []string

	// AccessCapacity auto-grants product access to the top-N entries by rank.
	// Resolved at boot from WAITLIST_ACCESS_CAPACITY. Zero (default) grants
	// none automatically.
	AccessCapacity int

	// Open is the master switch that ends the waitlist: when true EVERYONE
	// has access. Resolved at boot from WAITLIST_OPEN (1/true/yes,
	// case-insensitive). Default false.
	Open bool
}

func (c *Config) resolve() {
	if c.TurnstileSecret == "" {
		c.TurnstileSecret = os.Getenv("TURNSTILE_SECRET_KEY")
	}
	if c.AdminSecret == "" {
		c.AdminSecret = os.Getenv("WAITLIST_ADMIN_SECRET")
	}
	if c.AwardSecret == "" {
		c.AwardSecret = os.Getenv("WAITLIST_AWARD_SECRET")
	}
	if c.JoinRateLimit == 0 {
		c.JoinRateLimit = 5
	}
	if c.JoinRateWindow == 0 {
		c.JoinRateWindow = time.Hour
	}
	if c.InviteMaxBatch <= 0 {
		c.InviteMaxBatch = 50
	}
	if len(c.DefaultSlugs) == 0 {
		c.DefaultSlugs = splitCSV(os.Getenv("WAITLIST_DEFAULT_SLUGS"))
	}
	if c.AccessCapacity == 0 {
		if n, err := strconv.Atoi(strings.TrimSpace(os.Getenv("WAITLIST_ACCESS_CAPACITY"))); err == nil {
			c.AccessCapacity = n
		}
	}
	if !c.Open {
		c.Open = parseBool(os.Getenv("WAITLIST_OPEN"))
	}
	c.Points.resolve()
	c.CollectionPrefix = strings.TrimSpace(c.CollectionPrefix)
}

func (c *Config) validate() error {
	if c.CollectionPrefix != "" {
		for _, r := range c.CollectionPrefix {
			if !(r == '_' || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')) {
				return errors.New("waitlist: CollectionPrefix must be lowercase alphanumeric or _")
			}
		}
	}
	return nil
}

func (c *Config) waitlistsCollection() string {
	return c.prefixed("waitlists")
}

func (c *Config) entriesCollection() string {
	return c.prefixed("waitlist_entries")
}

func (c *Config) eventsCollection() string {
	return c.prefixed("waitlist_events")
}

func (c *Config) prefixed(name string) string {
	if c.CollectionPrefix == "" {
		return name
	}
	return c.CollectionPrefix + "_" + name
}

// envInt reads an int from the environment, falling back to def on unset /
// unparseable.
func envInt(key string, def int) int {
	if s := strings.TrimSpace(os.Getenv(key)); s != "" {
		if n, err := strconv.Atoi(s); err == nil {
			return n
		}
	}
	return def
}

func firstNonZero(v, def int) int {
	if v != 0 {
		return v
	}
	return def
}

// splitCSV splits a comma-separated env value into trimmed, non-empty parts.
// An empty or all-whitespace input yields nil.
func splitCSV(s string) []string {
	var out []string
	for _, part := range strings.Split(s, ",") {
		if v := strings.TrimSpace(part); v != "" {
			out = append(out, v)
		}
	}
	return out
}

// parseBool reports whether s is an affirmative flag: 1/true/yes
// (case-insensitive). Everything else — including empty — is false.
func parseBool(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "1", "true", "yes":
		return true
	default:
		return false
	}
}
