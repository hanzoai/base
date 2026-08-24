// Copyright (C) 2020-2026, Hanzo AI Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package org

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// truncate clips an IAM response body into an error message (three call sites in
// iam.go), so whatever it returns is read by a person and logged. Clipping by
// BYTE cuts a multi-byte rune in half and emits invalid UTF-8 — a body that says
// "café" becomes one that says nothing a terminal can render, and the error that
// was meant to explain an IAM failure becomes its own puzzle.
func TestTruncateClipsRunesAndStaysValidUTF8(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   string
		n    int
		want string
	}{
		{"short ascii is untouched", "hello", 10, "hello"},
		{"exactly n is untouched", "hello", 5, "hello"},
		{"long ascii clips", "hello world", 5, "hello…"},
		{"multibyte clips on a rune boundary", "héllo wörld", 4, "héll…"},
		{"every rune multibyte", "日本語のテキスト", 3, "日本語…"},
		{"emoji is one rune", "🔑🔑🔑🔑", 2, "🔑🔑…"},
		{"empty stays empty", "", 5, ""},
		{"zero budget keeps nothing", "abc", 0, "…"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := truncate(tc.in, tc.n)
			if got != tc.want {
				t.Fatalf("truncate(%q, %d) = %q, want %q", tc.in, tc.n, got, tc.want)
			}
			if !utf8.ValidString(got) {
				t.Fatalf("truncate(%q, %d) returned invalid UTF-8: %q", tc.in, tc.n, got)
			}
			if n := utf8.RuneCountInString(strings.TrimSuffix(got, "…")); n > tc.n {
				t.Fatalf("truncate(%q, %d) kept %d runes, want at most %d", tc.in, tc.n, n, tc.n)
			}
		})
	}
}

// The candidates are the shapes IAM might have STORED for a phone the caller
// states one way. It expands downward only — strips "+", then a US "+1" — so a
// caller holding E.164 finds a row saved either way.
//
// It does not expand upward, and that asymmetry is the behaviour, not an
// oversight to read past: a caller passing a raw national number probes exactly
// that one shape and will not find the same person stored as "+1…". Adding the
// country code back is a guess about which country, which is why the function
// does not make it. Today's only production caller looks up by email
// (plugins/bootnode/api_team.go), so nothing depends on the upward direction.
func TestNormalizePhoneCandidates(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   string
		want []string
	}{
		{"empty yields nothing", "", nil},
		{"E.164 US expands to three", "+16125551234", []string{"+16125551234", "16125551234", "6125551234"}},
		{"E.164 non-US drops only the plus", "+442071838750", []string{"+442071838750", "442071838750"}},
		{"raw digits stay one shape", "6125551234", []string{"6125551234"}},
		{"a bare plus yields itself", "+", []string{"+"}},
		{"plus one yields itself and the one", "+1", []string{"+1", "1"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := normalizePhoneCandidates(tc.in)
			if len(got) != len(tc.want) {
				t.Fatalf("normalizePhoneCandidates(%q) = %v, want %v", tc.in, got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("normalizePhoneCandidates(%q)[%d] = %q, want %q", tc.in, i, got[i], tc.want[i])
				}
			}
			seen := map[string]bool{}
			for _, v := range got {
				if seen[v] {
					t.Fatalf("normalizePhoneCandidates(%q) repeated %q: %v", tc.in, v, got)
				}
				seen[v] = true
			}
		})
	}
}

// A collision on add-user is not an error the caller should surface — EnsureUser
// treats it as "already provisioned" and proceeds. IAM spells the message more
// than one way, so the match is on the phrase and is case-insensitive.
func TestIsAlreadyExistsMsg(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want bool
	}{
		{"", false},
		{"user already exists", true},
		{"Username already exists.", true},
		{"EMAIL ALREADY EXISTS", true},
		{"phone: already exists in organization hanzo", true},
		{"user not found", false},
		{"already", false},
		{"exists", false},
	} {
		if got := isAlreadyExistsMsg(tc.in); got != tc.want {
			t.Fatalf("isAlreadyExistsMsg(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}
