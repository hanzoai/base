package claims_test

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/hanzoai/base/tools/claims"
)

func TestFromHeaders_CanonicalThree(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("X-User-Id", "u_alice")
	r.Header.Set("X-Org-Id", "hanzo")
	r.Header.Set("X-Roles", "admin, viewer ,,operator")

	c := claims.FromHeaders(r)

	if c.UserID != "u_alice" {
		t.Errorf("UserID = %q, want %q", c.UserID, "u_alice")
	}
	if c.OrgID != "hanzo" {
		t.Errorf("OrgID = %q, want %q", c.OrgID, "hanzo")
	}
	want := []string{"admin", "viewer", "operator"}
	if !reflect.DeepEqual(c.Roles, want) {
		t.Errorf("Roles = %v, want %v", c.Roles, want)
	}
}

func TestFromHeaders_EmptyRoles(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("X-User-Id", "u_bob")
	r.Header.Set("X-Org-Id", "acme")
	// no X-Roles header

	c := claims.FromHeaders(r)
	if len(c.Roles) != 0 {
		t.Errorf("Roles = %v, want empty", c.Roles)
	}
}

func TestFromHeaders_IgnoresLegacyVariants(t *testing.T) {
	// The helper must NEVER read any legacy variant. Even if a caller sets
	// X-Hanzo-*, X-IAM-*, or X-User-Role on the request, those values must
	// not influence the returned Claims.
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("X-Hanzo-User-Id", "forged")
	r.Header.Set("X-IAM-User-Id", "forged")
	r.Header.Set("X-User-Role", "admin")  // singular legacy
	r.Header.Set("X-User-Roles", "admin") // plural legacy
	r.Header.Set("X-Tenant-Id", "forged-org")
	// No canonical headers set.

	c := claims.FromHeaders(r)
	if c.UserID != "" || c.OrgID != "" || len(c.Roles) != 0 {
		t.Errorf("Claims from legacy headers leaked: %+v", c)
	}
}

func TestClaims_HasRole(t *testing.T) {
	c := claims.Claims{UserID: "u", OrgID: "o", Roles: []string{"admin", "trader"}}
	tests := []struct {
		wanted []string
		want   bool
	}{
		{[]string{"admin"}, true},
		{[]string{"trader"}, true},
		{[]string{"admin", "reader"}, true},
		{[]string{"reader"}, false},
		{[]string{""}, false},
		{[]string{}, false},
		{nil, false},
	}
	for _, tt := range tests {
		if got := c.HasRole(tt.wanted...); got != tt.want {
			t.Errorf("HasRole(%v) = %v, want %v", tt.wanted, got, tt.want)
		}
	}
}

func TestStripIdentityHeaders_RemovesCanonicalAndLegacy(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	forged := []string{
		// canonical 3
		"X-User-Id", "X-Org-Id", "X-Roles",
		// gateway auxiliaries
		"X-User-Email", "X-Phone-Number", "X-User-IsAdmin",
		// non-canonical legacy
		"X-User-Role", "X-User-Roles", "X-User-Name",
		"X-Tenant-Id", "X-Tenant-ID", "X-Org", "X-Is-Admin",
		// pre-validation hints
		"X-Gateway-Validated", "X-Gateway-User-Id",
		"X-Gateway-Org-Id", "X-Gateway-User-Email",
		// vendor-prefixed
		"X-Hanzo-User-Id", "X-Hanzo-User-Role", "X-Hanzo-Admin",
		"X-IAM-User-Id", "X-IAM-Roles", "X-IAM-Anything",
	}
	for _, h := range forged {
		r.Header.Set(h, "forged")
	}

	claims.StripIdentityHeaders(r.Header)

	for _, h := range forged {
		if v := r.Header.Get(h); v != "" {
			t.Errorf("header %q was NOT stripped (got %q)", h, v)
		}
	}
}

func TestStripIdentityHeaders_PreservesUnrelated(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("Authorization", "Bearer some-token")
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-Request-Id", "req-42")
	r.Header.Set("X-Forwarded-For", "1.2.3.4")

	claims.StripIdentityHeaders(r.Header)

	wanted := map[string]string{
		"Authorization":   "Bearer some-token",
		"Content-Type":    "application/json",
		"X-Request-Id":    "req-42",
		"X-Forwarded-For": "1.2.3.4",
	}
	for k, v := range wanted {
		if got := r.Header.Get(k); got != v {
			t.Errorf("header %q was modified: got %q, want %q", k, got, v)
		}
	}
}

func TestStrip_Middleware(t *testing.T) {
	var seen claims.Claims
	h := claims.Strip(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = claims.FromHeaders(r)
		w.WriteHeader(http.StatusOK)
	}))

	// Attacker forges canonical and legacy identity headers.
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-User-Id", "attacker")
	req.Header.Set("X-Org-Id", "victim-org")
	req.Header.Set("X-Roles", "admin")
	req.Header.Set("X-Hanzo-User-Id", "attacker")
	req.Header.Set("X-IAM-Roles", "admin")

	h.ServeHTTP(httptest.NewRecorder(), req)

	if seen.UserID != "" || seen.OrgID != "" || len(seen.Roles) != 0 {
		t.Errorf("Strip middleware leaked forged identity: %+v", seen)
	}
}

// ============================================================
// P7 — Red re-review coverage. One test per finding.
// ============================================================

// TestFromHeaders_RejectsControlBytesInSlug (P7-H3/H4/M2) — identity
// values containing CTL bytes (NUL, CR, LF, TAB, DEL) are dropped. The
// handler sees "" which causes RequireGateway to 503.
func TestFromHeaders_RejectsControlBytesInSlug(t *testing.T) {
	cases := []struct {
		name string
		val  string
	}{
		{"null", "alice\x00bob"},
		{"newline", "alice\nbob"},
		{"cr", "alice\rbob"},
		{"tab", "alice\tbob"},
		{"del", "alice\x7fbob"},
		{"vertical-tab", "alice\x0bbob"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/", nil)
			r.Header.Set("X-User-Id", tc.val)
			r.Header.Set("X-Org-Id", "acme")

			c := claims.FromHeaders(r)
			if c.UserID != "" {
				t.Errorf("UserID must be dropped for %q, got %q", tc.val, c.UserID)
			}
			if c.OrgID != "acme" {
				t.Errorf("unrelated OrgID must survive: got %q", c.OrgID)
			}
		})
	}
}

// TestFromHeaders_RejectsOverlongValue (P7-M1-adjacent) — values longer
// than MaxIdentityValueLen are dropped.
func TestFromHeaders_RejectsOverlongValue(t *testing.T) {
	long := make([]byte, claims.MaxIdentityValueLen+1)
	for i := range long {
		long[i] = 'a'
	}
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("X-User-Id", string(long))
	r.Header.Set("X-Org-Id", "acme")

	c := claims.FromHeaders(r)
	if c.UserID != "" {
		t.Errorf("overlong UserID must be dropped, got len=%d", len(c.UserID))
	}
}

// TestParseRoles_DropsCRLFRoles (P7-H4) — a role value that embeds CRLF
// (response-splitting primitive) is dropped from the returned slice.
func TestParseRoles_DropsCRLFRoles(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	// one clean role, one CRLF-poisoned role, one plain role.
	r.Header.Set("X-Roles", "admin,evil\r\nX-User-Id: attacker,viewer")
	r.Header.Set("X-User-Id", "u_alice")
	r.Header.Set("X-Org-Id", "acme")

	c := claims.FromHeaders(r)
	for _, role := range c.Roles {
		if strings.ContainsAny(role, "\r\n") {
			t.Errorf("parseRoles leaked CRLF role: %q", role)
		}
	}
	// The two clean roles should survive.
	got := strings.Join(c.Roles, ",")
	if got != "admin,viewer" {
		t.Errorf("roles = %q, want %q", got, "admin,viewer")
	}
}

// TestChain_EnforcesCanonicalOrder (P7-H3) — claims.Chain wires
// Strip→Inject→RequireGateway in the required order. A request with
// forged identity headers is stripped, then Inject sees empty values,
// then RequireGateway 503s — the forged identity never reaches the
// handler.
func TestChain_EnforcesCanonicalOrder(t *testing.T) {
	handlerRan := false
	h := claims.Chain(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handlerRan = true
		w.WriteHeader(http.StatusOK)
	}))

	// Forged headers — no real gateway upstream.
	req := httptest.NewRequest(http.MethodGet, "/v1/whatever", nil)
	req.Header.Set("X-User-Id", "attacker")
	req.Header.Set("X-Org-Id", "victim")
	req.Header.Set("X-Roles", "admin")

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if handlerRan {
		t.Fatal("forged identity reached handler through claims.Chain")
	}
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("Chain should 503 on forged-only request, got %d", rec.Code)
	}
}

// TestChain_DirectAccess_IsRejected — a request with no identity headers
// at all (direct pod access bypassing the gateway) is 503'd by Chain.
func TestChain_DirectAccess_IsRejected(t *testing.T) {
	handlerRan := false
	h := claims.Chain(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handlerRan = true
	}))

	req := httptest.NewRequest(http.MethodGet, "/v1/x", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if handlerRan {
		t.Fatal("handler ran without identity")
	}
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("want 503 on no-identity, got %d", rec.Code)
	}
}
