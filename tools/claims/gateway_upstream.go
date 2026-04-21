// Gateway-upstream assertion and request-context helpers.
//
// Every Base-derived service MUST sit behind hanzoai/gateway. The gateway is
// the sole JWT verifier in the stack. Services refuse to serve tenant-scoped
// routes unless this invariant is explicitly acknowledged at boot.
//
// Usage in a service:
//
//	if err := claims.AssertGatewayUpstream(); err != nil {
//	    log.Fatal(err) // boot refuses
//	}
//	mux.Use(claims.Strip)           // defense-in-depth: drop forged headers
//	mux.Use(claims.RequireGateway)  // 503 if headers are missing on tenant routes
//	mux.Use(claims.Inject)          // pull Claims into ctx
package claims

import (
	"context"
	"errors"
	"net/http"
	"os"
)

// EnvGatewayUpstream is the environment variable that acknowledges the service
// is reachable only through hanzoai/gateway. Any value other than "1" / "true"
// triggers a boot refusal.
const EnvGatewayUpstream = "HANZO_GATEWAY_UPSTREAM"

// ErrGatewayBypass is returned / surfaced when a tenant-scoped request reaches
// a handler without the canonical identity headers. It maps to HTTP 503; 401
// would suggest the client can recover by authenticating, which is wrong —
// the deployment topology is broken, not the caller.
var ErrGatewayBypass = errors.New("claims: gateway bypass detected — canonical identity headers missing")

// ErrGatewayNotAsserted is returned at boot when HANZO_GATEWAY_UPSTREAM is
// unset or falsy.
var ErrGatewayNotAsserted = errors.New("claims: HANZO_GATEWAY_UPSTREAM must be set to 1 — apps never re-verify JWT")

// AssertGatewayUpstream returns an error if the gateway-upstream acknowledgement
// is missing. Call exactly once at service boot, before listening.
func AssertGatewayUpstream() error {
	switch os.Getenv(EnvGatewayUpstream) {
	case "1", "true", "TRUE", "True":
		return nil
	default:
		return ErrGatewayNotAsserted
	}
}

// ctxKey is an unexported key type to prevent foreign packages from injecting
// Claims into the context by accident. The Claims context is produced only by
// claims.Inject.
type ctxKey struct{}

// Inject is a middleware that parses the canonical 3 identity headers and
// attaches the resulting Claims to the request context. It does NOT validate
// presence — pair it with RequireGateway for tenant-scoped routes.
func Inject(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c := FromHeaders(r)
		ctx := context.WithValue(r.Context(), ctxKey{}, c)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// FromContext returns the verified Claims attached by Inject. Returns the
// zero Claims{} if none were attached (e.g. public route).
func FromContext(ctx context.Context) Claims {
	c, _ := ctx.Value(ctxKey{}).(Claims)
	return c
}

// OrgID is a thin convenience that returns the caller's org slug.
func OrgID(ctx context.Context) string { return FromContext(ctx).OrgID }

// UserID is a thin convenience that returns the caller's user id.
func UserID(ctx context.Context) string { return FromContext(ctx).UserID }

// HasRole reports whether the caller holds any of the requested roles.
func HasRole(ctx context.Context, role ...string) bool {
	return FromContext(ctx).HasRole(role...)
}

// RequireGateway is a middleware for tenant-scoped routes. If either X-User-Id
// or X-Org-Id is missing after Strip + Inject have run, the gateway was
// bypassed (misconfigured ingress, direct pod access, etc.) and the handler
// MUST NOT serve. Returns 503 with a neutral body (no hint about which header
// was missing — that is an attacker oracle).
func RequireGateway(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c := FromContext(r.Context())
		if c.UserID == "" || c.OrgID == "" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusServiceUnavailable)
			// Intentionally vague. Operators get the detail in logs, not wire.
			_, _ = w.Write([]byte(`{"error":"service unavailable"}`))
			return
		}
		next.ServeHTTP(w, r)
	})
}

// Chain is the canonical tenant-route middleware chain: Strip → Inject →
// RequireGateway → next. This is the ONLY approved way to mount a
// tenant-scoped handler. Services MUST NOT compose Strip, Inject, and
// RequireGateway by hand — the PHILOSOPHY.md "one and only one way"
// principle applies, and a misordered hand-wired chain silently defeats
// the forged-header defense (verified by Red probe P7-H3).
//
// Public routes (/healthz, /readyz, /metrics) MUST be mounted on a
// separate mux that does not pass through Chain; the chain would 503
// every probe when gateway headers are absent.
func Chain(next http.Handler) http.Handler {
	return Strip(Inject(RequireGateway(next)))
}

// RequireRole returns a middleware that enforces the caller holds at least one
// of the requested roles. On failure, the response is a 404 — not 403 — so
// probing for authorized endpoints does not leak their existence. If you want
// an explicit 403 for a user that IS in the tenant but lacks a role, check
// HasRole inside the handler instead.
func RequireRole(role ...string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !HasRole(r.Context(), role...) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write([]byte(`{"error":"not found"}`))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
