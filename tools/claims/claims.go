// Package claims provides the canonical 3-header identity contract for every
// Base-derived service. There is exactly ONE way to read the authenticated
// caller's identity: [FromHeaders].
//
// The Hanzo Gateway validates the IAM JWT upstream and re-emits exactly three
// headers on the forwarded request. Services MUST NOT read any other variant
// (no X-Hanzo-*, no X-IAM-*, no singular X-User-Role, no X-Tenant-Id alias):
//
//	X-User-Id <- JWT "sub"
//	X-Org-Id  <- JWT "owner"
//	X-Roles   <- JWT "roles" (comma-joined if array)
//
// Services MUST call [StripIdentityHeaders] on every inbound request before
// the JWT middleware re-injects trusted values. A client that sets any of
// these headers directly is rejected at the gateway; services defend in depth
// by stripping again locally in case a sidecar/mesh bypasses the gateway.
package claims

import (
	"net/http"
	"strings"
)

// The canonical 3 identity headers. These are the ONLY headers a handler may
// read to determine the authenticated principal.
const (
	HeaderUserID = "X-User-Id"
	HeaderOrgID  = "X-Org-Id"
	HeaderRoles  = "X-Roles"
)

// MaxIdentityValueLen caps every header value consumed by this package.
// Chosen at 256 bytes: generous for ULIDs/UUIDs, IAM usernames, and the
// longest realistic comma-joined role list (~20 role names @ 12 chars).
// A value longer than this is either a bug or an exhaustion attempt and
// is discarded — the caller observes "no identity" and RequireGateway 503s.
const MaxIdentityValueLen = 256

// Claims is the verified identity of the current request as asserted by the
// upstream gateway's JWT validation. All three fields may be empty strings /
// empty slices when the request is unauthenticated (public endpoints).
type Claims struct {
	UserID string
	OrgID  string
	Roles  []string
}

// FromHeaders returns the canonical Claims for the request. It reads ONLY the
// three canonical headers; any legacy variant set by a client is ignored by
// design (and should have been stripped upstream).
//
// Header values are sanitized: any value that contains a control character
// (byte < 0x20 or byte == 0x7f) or exceeds [MaxIdentityValueLen] bytes is
// discarded and the corresponding field becomes empty. This makes log /
// path / response-splitting injection unreachable through this parser, and
// makes [RequireGateway] fail closed (503) on a poisoned identity instead
// of forwarding hostile bytes into handlers.
//
// Roles are decoded from a comma-separated list; empty roles, roles that
// exceed the length cap individually, and roles that contain control
// characters are each dropped.
func FromHeaders(r *http.Request) Claims {
	return Claims{
		UserID: sanitizeIdentity(r.Header.Get(HeaderUserID)),
		OrgID:  sanitizeIdentity(r.Header.Get(HeaderOrgID)),
		Roles:  parseRoles(r.Header.Get(HeaderRoles)),
	}
}

// sanitizeIdentity returns s unchanged when it meets the identity-value
// contract: non-empty, ≤ MaxIdentityValueLen bytes, no control characters.
// Any violation returns "" — i.e. the header is treated as absent. We
// deliberately do NOT fall back to a "best-effort" trimmed value because
// the identity contract is binary: either the gateway produced a clean
// header or the request is hostile.
func sanitizeIdentity(s string) string {
	if s == "" {
		return ""
	}
	if len(s) > MaxIdentityValueLen {
		return ""
	}
	if hasControlByte(s) {
		return ""
	}
	return s
}

// hasControlByte reports whether s contains any byte below 0x20 (all C0
// controls including NUL, TAB, CR, LF) or the DEL byte 0x7f. These bytes
// are never part of a legitimate IAM slug, email, or role name and are
// the universal primitives for header / log / path injection.
func hasControlByte(s string) bool {
	for i := 0; i < len(s); i++ {
		b := s[i]
		if b < 0x20 || b == 0x7f {
			return true
		}
	}
	return false
}

// parseRoles splits a comma-joined roles header into a slice, trimming
// whitespace and dropping empty entries. Any role value that contains a
// control byte or exceeds the length cap is silently dropped — the parser
// refuses to propagate bytes that could smuggle CRLF or NUL into
// downstream audit / log / response code.
func parseRoles(raw string) []string {
	if raw == "" {
		return nil
	}
	if len(raw) > MaxIdentityValueLen {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		v := strings.TrimSpace(p)
		if v == "" {
			continue
		}
		if hasControlByte(v) {
			continue
		}
		if len(v) > MaxIdentityValueLen {
			continue
		}
		out = append(out, v)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// HasRole reports whether the caller holds any of the requested roles.
// Role names are matched exactly (case-sensitive); empty inputs return false.
func (c Claims) HasRole(wanted ...string) bool {
	for _, want := range wanted {
		if want == "" {
			continue
		}
		for _, got := range c.Roles {
			if got == want {
				return true
			}
		}
	}
	return false
}

// legacyIdentityHeaders is the full list of non-canonical identity-bearing
// headers that MUST be stripped from every inbound request. Include both the
// canonical 3 (so a client-supplied value cannot survive until a handler
// re-reads it before the gateway/JWT middleware has re-set them) and every
// historical variant the ecosystem has shipped.
var legacyIdentityHeaders = []string{
	// Canonical 3 — stripped on ingress, re-injected after JWT validation.
	HeaderUserID,
	HeaderOrgID,
	HeaderRoles,
	// Gateway-emitted auxiliaries (derivatives of the JWT).
	"X-User-Email",
	"X-Phone-Number",
	"X-User-IsAdmin",
	// Non-canonical legacy identity headers.
	"X-User-Role",  // singular
	"X-User-Roles", // plural — renamed to X-Roles
	"X-User-Name",
	"X-Tenant-Id",
	"X-Tenant-ID",
	"X-Org",
	"X-Is-Admin",
	// Pre-validation hints from older gateway flows.
	"X-Gateway-Validated",
	"X-Gateway-User-Id",
	"X-Gateway-Org-Id",
	"X-Gateway-User-Email",
}

// StripIdentityHeaders removes every inbound identity-bearing header from h.
// Call this before JWT validation re-injects the canonical values. It also
// unconditionally drops every header whose name starts with "X-Hanzo-" or
// "X-IAM-" (case-insensitive), closing the "clever-new-prefix" attack vector.
func StripIdentityHeaders(h http.Header) {
	for _, name := range legacyIdentityHeaders {
		h.Del(name)
	}
	for key := range h {
		upper := strings.ToUpper(key)
		if strings.HasPrefix(upper, "X-IAM-") || strings.HasPrefix(upper, "X-HANZO-") {
			h.Del(key)
		}
	}
}

// Strip is a net/http middleware that calls [StripIdentityHeaders] on every
// inbound request before delegating to next. Use at the outermost layer of a
// service, before any JWT middleware that populates the canonical 3 headers.
func Strip(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		StripIdentityHeaders(r.Header)
		next.ServeHTTP(w, r)
	})
}
