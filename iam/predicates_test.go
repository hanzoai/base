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
// Three prefixes are resolved and the table states each once, so a kind added to
// one predicate and forgotten in another shows up as a row disagreeing with
// itself.
func TestKeyKindsAreClassifiedByPrefix(t *testing.T) {
	for _, tc := range []struct {
		token                       string
		publishable, secret, apiKey bool
	}{
		{"pk-live-abc", true, false, true},
		{"sk-live-abc", false, true, true},
		{"hk-abc", false, false, true},
		// Not keys at all: a JWT, an empty string, and near-misses.
		{"eyJhbGciOiJIUzI1NiJ9.e30.sig", false, false, false},
		{"", false, false, false},
		{"pk_live_abc", false, false, false},  // underscore, not dash
		{"PK-LIVE-ABC", false, false, false},  // prefixes are lower-case
		{"xpk-live-abc", false, false, false}, // the prefix must lead
		{"pk-", true, false, true},            // a bare prefix still classifies
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
		})
	}
}

// hz-, hi- and ha- are prefixes IAM mints and no door here resolves, so a caller
// bearing one reaches exactly what an anonymous caller reaches. That is the whole
// reason IsWidgetKey and IsAnalyticsKey could be deleted (30c91ac7) — and it now
// rests entirely on IsAPIKey answering false, with no predicate left naming those
// kinds. If one ever starts returning true here, a key that was public becomes a
// credential, silently. This is the guard on that.
func TestUnresolvedPrefixesAreNotAPIKeys(t *testing.T) {
	for _, token := range []string{"hz-abc", "hi-abc", "ha-abc"} {
		if iam.IsAPIKey(token) {
			t.Fatalf("IsAPIKey(%q) = true — the key middleware would resolve a prefix no door serves", token)
		}
		if iam.IsPublishable(token) || iam.IsSecret(token) {
			t.Fatalf("%q classified as a publishable or secret key", token)
		}
	}
	for _, token := range []string{"hk-abc", "pk-abc", "sk-abc"} {
		if !iam.IsAPIKey(token) {
			t.Fatalf("IsAPIKey(%q) = false — the key middleware would refuse a real key", token)
		}
	}
}

// No token is both publishable and secret. It matters for pk- specifically: the
// middleware refuses a publishable key by kind, and a token that were also
// something else would offer a second reading to gate on.
func TestNoTokenIsTwoKinds(t *testing.T) {
	for _, token := range []string{"pk-a", "sk-a", "hk-a", "hz-a", "hi-a", "ha-a"} {
		if iam.IsPublishable(token) && iam.IsSecret(token) {
			t.Fatalf("%q is both publishable and secret", token)
		}
	}
}
