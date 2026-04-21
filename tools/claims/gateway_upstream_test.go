package claims_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hanzoai/base/tools/claims"
)

func TestAssertGatewayUpstream_RefusesUnset(t *testing.T) {
	t.Setenv(claims.EnvGatewayUpstream, "")
	if err := claims.AssertGatewayUpstream(); err == nil {
		t.Fatal("expected error when HANZO_GATEWAY_UPSTREAM is unset")
	}
}

func TestAssertGatewayUpstream_RefusesFalsy(t *testing.T) {
	for _, v := range []string{"0", "false", "no", "yes", "on"} {
		t.Run(v, func(t *testing.T) {
			t.Setenv(claims.EnvGatewayUpstream, v)
			if err := claims.AssertGatewayUpstream(); err == nil {
				t.Fatalf("expected error for %q", v)
			}
		})
	}
}

func TestAssertGatewayUpstream_AcceptsTruthy(t *testing.T) {
	for _, v := range []string{"1", "true", "TRUE", "True"} {
		t.Run(v, func(t *testing.T) {
			t.Setenv(claims.EnvGatewayUpstream, v)
			if err := claims.AssertGatewayUpstream(); err != nil {
				t.Fatalf("expected nil for %q, got %v", v, err)
			}
		})
	}
}

// TestInject populates context with canonical Claims and exposes them via
// FromContext/OrgID/UserID/HasRole.
func TestInject_Populates(t *testing.T) {
	var seen claims.Claims
	var seenOrg, seenUser string
	var seenRole bool

	h := claims.Inject(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = claims.FromContext(r.Context())
		seenOrg = claims.OrgID(r.Context())
		seenUser = claims.UserID(r.Context())
		seenRole = claims.HasRole(r.Context(), "admin")
	}))

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set(claims.HeaderUserID, "u_alice")
	r.Header.Set(claims.HeaderOrgID, "hanzo")
	r.Header.Set(claims.HeaderRoles, "admin,viewer")

	h.ServeHTTP(httptest.NewRecorder(), r)

	if seen.UserID != "u_alice" || seen.OrgID != "hanzo" {
		t.Fatalf("FromContext = %+v", seen)
	}
	if seenUser != "u_alice" || seenOrg != "hanzo" {
		t.Fatalf("UserID/OrgID = %q/%q", seenUser, seenOrg)
	}
	if !seenRole {
		t.Fatal("HasRole(admin) should be true")
	}
}

// FromContext on a bare context returns zero claims (not a panic).
func TestFromContext_Empty(t *testing.T) {
	c := claims.FromContext(context.Background())
	if c.UserID != "" || c.OrgID != "" || len(c.Roles) != 0 {
		t.Fatalf("expected zero Claims, got %+v", c)
	}
}

// RequireGateway returns 503 — NOT 401 — when identity headers are missing.
// 401 would suggest the caller can recover by authenticating; the real
// problem is a gateway bypass (deployment topology bug).
func TestRequireGateway_503OnMissing(t *testing.T) {
	h := claims.Inject(claims.RequireGateway(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})))

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", rec.Code)
	}
}

// RequireGateway also 503s when only one of the two required headers is set.
func TestRequireGateway_503OnPartial(t *testing.T) {
	cases := []struct {
		name string
		kv   map[string]string
	}{
		{"only user", map[string]string{claims.HeaderUserID: "u"}},
		{"only org", map[string]string{claims.HeaderOrgID: "o"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := claims.Inject(claims.RequireGateway(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				t.Fatal("handler must not run")
			})))
			r := httptest.NewRequest(http.MethodGet, "/", nil)
			for k, v := range tc.kv {
				r.Header.Set(k, v)
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, r)
			if rec.Code != http.StatusServiceUnavailable {
				t.Fatalf("expected 503, got %d", rec.Code)
			}
		})
	}
}

// RequireGateway allows through when both required headers are present.
func TestRequireGateway_PassThrough(t *testing.T) {
	called := false
	h := claims.Inject(claims.RequireGateway(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})))

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set(claims.HeaderUserID, "u_alice")
	r.Header.Set(claims.HeaderOrgID, "hanzo")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)

	if !called {
		t.Fatal("handler should have run")
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

// RequireRole returns 404 (NOT 403) when the caller lacks the role. This is a
// deliberate existence-leak defense: a probe from a non-admin for an
// admin-only endpoint should be indistinguishable from a probe for a
// non-existent endpoint.
func TestRequireRole_404OnMissingRole(t *testing.T) {
	h := claims.Inject(claims.RequireRole("admin")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler must not run")
	})))

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set(claims.HeaderUserID, "u_alice")
	r.Header.Set(claims.HeaderOrgID, "hanzo")
	r.Header.Set(claims.HeaderRoles, "viewer")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

// RequireRole lets the caller through when the role is held.
func TestRequireRole_PassThrough(t *testing.T) {
	called := false
	h := claims.Inject(claims.RequireRole("admin")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})))
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set(claims.HeaderUserID, "u")
	r.Header.Set(claims.HeaderOrgID, "o")
	r.Header.Set(claims.HeaderRoles, "admin,viewer")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	if !called || rec.Code != http.StatusOK {
		t.Fatalf("expected passthrough 200, got called=%v code=%d", called, rec.Code)
	}
}

// Strip + Inject composes: a forged X-User-Id from the client is removed by
// Strip, leaving Inject to see an empty Claims; RequireGateway then 503s.
func TestStrip_ThenInject_DefeatsForgedHeaders(t *testing.T) {
	h := claims.Strip(claims.Inject(claims.RequireGateway(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler must not run for forged identity")
	}))))

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	// Attacker forges every identity header we know about.
	r.Header.Set(claims.HeaderUserID, "attacker")
	r.Header.Set(claims.HeaderOrgID, "victim")
	r.Header.Set(claims.HeaderRoles, "admin")
	r.Header.Set("X-Hanzo-User-Id", "attacker")
	r.Header.Set("X-IAM-User-Id", "attacker")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 after Strip removed forged headers, got %d", rec.Code)
	}
}
