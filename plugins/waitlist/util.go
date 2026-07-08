// Copyright (C) 2020-2026, Hanzo AI Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package waitlist

import (
	"crypto/rand"
	"math/big"
	"regexp"
	"strings"
)

// refCodeAlphabet excludes confusing chars (0/O/1/l/I/5/S/2/Z/9/g/v/V).
const refCodeAlphabet = "6789BCDFGHJKLMNPQRTWbcdfghjkmnpqrtwz"

// generateRefCode returns a cryptographically random 8-char referral code.
// 36^8 ≈ 2.8e12 — collision rate is negligible at any realistic list size.
func generateRefCode() string {
	const n = 8
	max := big.NewInt(int64(len(refCodeAlphabet)))
	out := make([]byte, n)
	for i := 0; i < n; i++ {
		v, err := rand.Int(rand.Reader, max)
		if err != nil {
			// crypto/rand should never fail; fall back rather than panic so
			// a transient entropy hiccup doesn't 500 the join endpoint.
			return strings.Repeat(string(refCodeAlphabet[0]), n)
		}
		out[i] = refCodeAlphabet[v.Int64()]
	}
	return string(out)
}

var emailRe = regexp.MustCompile(`^[^\s@]+@[^\s@]+\.[^\s@]+$`)

func isValidEmail(s string) bool {
	if len(s) > 254 {
		return false
	}
	return emailRe.MatchString(s)
}

// defaultDisposableDomains is the built-in blocklist used when Config
// does not override it. Kept short on purpose — operators who need a
// big list pass their own.
var defaultDisposableDomains = []string{
	"tempmail.com",
	"guerrillamail.com",
	"10minutemail.com",
	"mailinator.com",
	"trashmail.com",
	"getairmail.com",
	"yopmail.com",
	"maildrop.cc",
	"throwaway.email",
	"fakeinbox.com",
}

func newDomainSet(list []string) map[string]struct{} {
	out := make(map[string]struct{}, len(list))
	for _, d := range list {
		out[strings.ToLower(strings.TrimSpace(d))] = struct{}{}
	}
	return out
}

func emailDomain(email string) string {
	i := strings.LastIndexByte(email, '@')
	if i < 0 || i == len(email)-1 {
		return ""
	}
	return strings.ToLower(email[i+1:])
}

func normalizeEmail(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

// titleSlug returns the slug with its first letter upper-cased, used as the
// human-readable name of a seeded default waitlist (e.g. "hanzod" -> "Hanzod").
func titleSlug(slug string) string {
	if slug == "" {
		return slug
	}
	return strings.ToUpper(slug[:1]) + slug[1:]
}
