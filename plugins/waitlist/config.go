// Copyright (C) 2020-2026, Hanzo AI Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package waitlist registers a viral waiting-list plugin on a Base app.
//
// It exposes three endpoints under /v1/waitlist:
//
//	POST /v1/waitlist/join     - register an entry, optionally crediting a referrer
//	GET  /v1/waitlist/status   - look up an entry's rank and share URL
//	GET  /v1/waitlist/export   - admin-only CSV export
//
// Backing storage is two Base collections (`waitlists`, `waitlist_entries`)
// that are auto-created on bootstrap. All state lives in the host Base
// SQLite shard — no Redis, no external store.
package waitlist

import (
	"errors"
	"os"
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

	// AdminSecret guards /v1/waitlist/export. Required header is
	// `Authorization: Bearer <AdminSecret>`. Resolved at boot from
	// WAITLIST_ADMIN_SECRET if empty. If still empty after resolution,
	// export is disabled (404).
	AdminSecret string

	// DisposableDomains, if non-nil, replaces the built-in disposable
	// e-mail blocklist. Pass an empty slice to disable blocking.
	DisposableDomains []string

	// ShareBaseURL, when set, is prepended to the ?ref=<code> share link so the
	// response carries an ABSOLUTE, shareable URL (required for share-to-X and a
	// copyable referral link). Resolved at boot from WAITLIST_SHARE_BASE_URL if
	// empty. Empty -> the share URL stays the relative "?ref=<code>" (the widget
	// resolves it against its own origin).
	ShareBaseURL string
}

// ConfigFromEnv builds a Config from the environment, mirroring the bootnode
// plugin's opt-in convention. The plugin is OFF unless WAITLIST_ENABLED=true.
//
//	WAITLIST_ENABLED         "true" to mount the plugin
//	TURNSTILE_SECRET_KEY     Cloudflare Turnstile secret (empty -> captcha off)
//	WAITLIST_ADMIN_SECRET    Bearer secret for /v1/waitlist/export (empty -> export off)
//	WAITLIST_SHARE_BASE_URL  absolute base for the share link (e.g. https://waitlist.hanzo.ai)
func ConfigFromEnv() Config {
	return Config{
		Enabled:      os.Getenv("WAITLIST_ENABLED") == "true",
		ShareBaseURL: strings.TrimRight(strings.TrimSpace(os.Getenv("WAITLIST_SHARE_BASE_URL")), "/"),
		// TurnstileSecret + AdminSecret are resolved from env in resolve().
	}
}

func (c *Config) resolve() {
	if c.TurnstileSecret == "" {
		c.TurnstileSecret = os.Getenv("TURNSTILE_SECRET_KEY")
	}
	if c.AdminSecret == "" {
		c.AdminSecret = os.Getenv("WAITLIST_ADMIN_SECRET")
	}
	if c.ShareBaseURL == "" {
		c.ShareBaseURL = strings.TrimRight(strings.TrimSpace(os.Getenv("WAITLIST_SHARE_BASE_URL")), "/")
	}
	if c.JoinRateLimit == 0 {
		c.JoinRateLimit = 5
	}
	if c.JoinRateWindow == 0 {
		c.JoinRateWindow = time.Hour
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

// shareURL builds the referral share link for a code. With ShareBaseURL set it
// is absolute (https://host?ref=CODE) — shareable to X and copyable; otherwise
// it stays the relative "?ref=CODE" the widget resolves against its own origin.
func (c *Config) shareURL(code string) string {
	if c.ShareBaseURL == "" {
		return "?ref=" + code
	}
	return c.ShareBaseURL + "?ref=" + code
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
