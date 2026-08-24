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
