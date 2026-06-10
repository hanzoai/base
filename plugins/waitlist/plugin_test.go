// Copyright (C) 2020-2026, Hanzo AI Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package waitlist

import (
	"testing"
	"time"
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

func contains(s string, b byte) bool {
	for i := 0; i < len(s); i++ {
		if s[i] == b {
			return true
		}
	}
	return false
}
