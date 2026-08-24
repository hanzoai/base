// Copyright (C) 2020-2026, Hanzo AI Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package iam_test

import (
	"testing"

	"github.com/hanzoai/base/iam"
)

// The kind of a key decides what it reaches. plugins/org's key middleware admits
// a request only when IsAPIKey holds, and refuses a publishable key everything
// under /v1/bases by KIND rather than by method — so these predicates ARE the
// authorization, and this is the import path the estate is told to use.
//
// The table states every prefix once, so a new kind added to one predicate and
// forgotten in another shows up as a row that disagrees with itself.
func TestKeyKindsAreClassifiedByPrefix(t *testing.T) {
	for _, tc := range []struct {
		token                                          string
		publishable, secret, apiKey, analytics, widget bool
	}{
		{"pk-live-abc", true, false, true, false, false},
		{"sk-live-abc", false, true, true, false, false},
		{"hk-abc", false, false, true, false, false},
		{"hi-abc", false, false, false, true, false},
		{"ha-abc", false, false, false, true, false},
		{"hz-abc", false, false, false, false, true},
		// Not keys at all: a JWT, an empty string, and near-misses.
		{"eyJhbGciOiJIUzI1NiJ9.e30.sig", false, false, false, false, false},
		{"", false, false, false, false, false},
		{"pk_live_abc", false, false, false, false, false},  // underscore, not dash
		{"PK-LIVE-ABC", false, false, false, false, false},  // prefixes are lower-case
		{"xpk-live-abc", false, false, false, false, false}, // prefix must lead
		{"pk-", true, false, true, false, false},            // bare prefix still classifies
	} {
		t.Run(tc.token, func(t *testing.T) {
			if got := iam.IsPublishable(tc.token); got != tc.publishable {
				t.Errorf("IsPublishable(%q) = %v, want %v", tc.token, got, tc.publishable)
			}
			if got := iam.IsSecret(tc.token); got != tc.secret {
				t.Errorf("IsSecret(%q) = %v, want %v", tc.token, got, tc.secret)
			}
			if got := iam.IsAPIKey(tc.token); got != tc.apiKey {
				t.Errorf("IsAPIKey(%q) = %v, want %v", tc.token, got, tc.apiKey)
			}
			if got := iam.IsAnalytics(tc.token); got != tc.analytics {
				t.Errorf("IsAnalytics(%q) = %v, want %v", tc.token, got, tc.analytics)
			}
			if got := iam.IsWidget(tc.token); got != tc.widget {
				t.Errorf("IsWidget(%q) = %v, want %v", tc.token, got, tc.widget)
			}
		})
	}
}

// IsAPIKey is what the key middleware asks before it will resolve a credential
// at all (plugins/org/plugin.go). It admits hk-, pk- and sk- and NOTHING else —
// so an analytics or widget key is not an API key here, and a request bearing
// one is refused rather than resolved. Stated as its own test because the
// asymmetry reads like an omission and is not: those kinds are classified for a
// caller to route on, never admitted by this door.
func TestAnalyticsAndWidgetKeysAreNotAPIKeys(t *testing.T) {
	for _, token := range []string{"hi-abc", "ha-abc", "hz-abc"} {
		if iam.IsAPIKey(token) {
			t.Fatalf("IsAPIKey(%q) = true — the key middleware would admit it", token)
		}
	}
	for _, token := range []string{"hk-abc", "pk-abc", "sk-abc"} {
		if !iam.IsAPIKey(token) {
			t.Fatalf("IsAPIKey(%q) = false — the key middleware would refuse a real key", token)
		}
	}
}

// No token is two kinds at once. It matters for pk- specifically: the middleware
// refuses a publishable key by kind, and a token that were also something else
// would offer a second reading to gate on.
func TestNoTokenIsTwoKinds(t *testing.T) {
	for _, token := range []string{"pk-a", "sk-a", "hk-a", "hi-a", "ha-a", "hz-a"} {
		n := 0
		for _, is := range []bool{iam.IsPublishable(token), iam.IsSecret(token), iam.IsAnalytics(token), iam.IsWidget(token)} {
			if is {
				n++
			}
		}
		if n > 1 {
			t.Fatalf("%q matched %d kinds, want at most 1", token, n)
		}
	}
}

// hk- is the one kind with no predicate of its own: it is an API key and nothing
// narrower says so. That is a real hole in the surface rather than a rounding
// error — a caller routing on kind has no way to name a hosted key, and asking
// "is it an API key but none of the others" is a definition by subtraction that
// silently acquires every kind added later.
func TestHostedKeyIsAnAPIKeyWithNoKindOfItsOwn(t *testing.T) {
	const hosted = "hk-abc"
	if !iam.IsAPIKey(hosted) {
		t.Fatalf("IsAPIKey(%q) = false", hosted)
	}
	for name, is := range map[string]bool{
		"IsPublishable": iam.IsPublishable(hosted),
		"IsSecret":      iam.IsSecret(hosted),
		"IsAnalytics":   iam.IsAnalytics(hosted),
		"IsWidget":      iam.IsWidget(hosted),
	} {
		if is {
			t.Fatalf("%s(%q) = true — hk- has acquired a kind; give it a named predicate", name, hosted)
		}
	}
}
