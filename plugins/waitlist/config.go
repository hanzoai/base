// Copyright (C) 2020-2026, Hanzo AI Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package waitlist registers a viral waiting-list plugin on a Base app.
//
// It exposes four endpoints under /v1/waitlist:
//
//	POST /v1/waitlist/join     - register an entry, optionally crediting a referrer
//	GET  /v1/waitlist/status   - look up an entry's rank, score and access
//	POST /v1/waitlist/boost    - service-authed position boost (e.g. hanzod)
//	GET  /v1/waitlist/export   - admin-only CSV export
//
// Backing storage is two Base collections (`waitlists`, `waitlist_entries`)
// that are auto-created on bootstrap. All state lives in the host Base
// SQLite shard — no Redis, no external store.
package waitlist

import (
	"errors"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config controls plugin registration.
type Config struct {
	// Enabled toggles the whole plugin. Default: true.
	Enabled bool

	// CollectionPrefix lets multiple waitlist plugins coexist on one Base
	// (rare). Default empty -> collections named `waitlists` and `waitlist_entries`.
	CollectionPrefix string

	// TurnstileSecret enables Cloudflare Turnstile token verification on
	// /v1/waitlist/join. Leave empty in dev to skip verification.
	// Resolved at boot from TURNSTILE_SECRET_KEY if empty.
	TurnstileSecret string

	// JoinRateLimit caps /v1/waitlist/join by source IP. Zero -> default
	// (5 per hour). Set negative to disable.
	JoinRateLimit int

	// JoinRateWindow is the sliding window for JoinRateLimit. Zero -> 1h.
	JoinRateWindow time.Duration

	// AdminSecret guards /v1/waitlist/export and /v1/waitlist/boost. Required
	// header is `Authorization: Bearer <AdminSecret>`. Resolved at boot from
	// WAITLIST_ADMIN_SECRET if empty. If still empty after resolution, those
	// service endpoints are disabled (404) unless the caller is a superuser.
	AdminSecret string

	// DisposableDomains, if non-nil, replaces the built-in disposable
	// e-mail blocklist. Pass an empty slice to disable blocking.
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
	if c.JoinRateLimit == 0 {
		c.JoinRateLimit = 5
	}
	if c.JoinRateWindow == 0 {
		c.JoinRateWindow = time.Hour
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
	if c.CollectionPrefix == "" {
		return "waitlists"
	}
	return c.CollectionPrefix + "_waitlists"
}

func (c *Config) entriesCollection() string {
	if c.CollectionPrefix == "" {
		return "waitlist_entries"
	}
	return c.CollectionPrefix + "_waitlist_entries"
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
