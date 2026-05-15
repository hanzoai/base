package apis

import (
	"net/http"
	"strings"

	"github.com/hanzoai/base/core"
	"github.com/hanzoai/base/tools/hook"
	"github.com/hanzoai/base/tools/router"
)

// bindRecordAuthApi registers the read-only auth record endpoints.
//
// The mutating local-auth surface (auth-with-password, auth-with-otp,
// auth-with-oauth2, oauth2-redirect, request/confirm flows, impersonate)
// was removed in the IAM-native rip. Hanzo IAM is the only auth source;
// clients run the PKCE flow against /api/iam/oauth/authorize, which the
// platform plugin mounts as a transparent proxy to IAM_ENDPOINT.
//
// The two endpoints kept here are read-only or refresh existing
// sessions and do not issue new credentials:
//
//   - GET  /api/collections/{c}/auth-methods  — discovery metadata
//   - POST /api/collections/{c}/auth-refresh  — rotate an existing JWT
//
// Any client that hits a stale local-auth URL gets a generic 404 from
// the router, which is the correct signal for a permanently-removed
// endpoint that no longer has even a deprecated handler bound.
func bindRecordAuthApi(app core.App, rg *router.RouterGroup[*core.RequestEvent]) {
	sub := rg.Group("/collections/{collection}")

	sub.GET("/auth-methods", recordAuthMethods).Bind(
		collectionPathRateLimit("", "listAuthMethods"),
	)

	sub.POST("/auth-refresh", recordAuthRefresh).Bind(
		collectionPathRateLimit("", "authRefresh"),
		RequireSameCollectionContextAuth(""),
	)
}

func findAuthCollection(e *core.RequestEvent) (*core.Collection, error) {
	collection, err := e.App.FindCachedCollectionByNameOrId(e.Request.PathValue("collection"))

	if err != nil || !collection.IsAuth() {
		return nil, e.NotFoundError("Missing or invalid auth collection context.", err)
	}

	return collection, nil
}

// externalAuthGuard returns a middleware that returns 410 Gone for any
// retired Base auth surface when external-only mode is active
// (StoreKeyExternalAuthOnly == true — the only mode the platform plugin
// allows). Hanzo IAM is the only auth source; there is no local
// password / OTP / MFA path.
//
// No collection is exempt. The admin panel UI uses the IAM PKCE flow
// against /api/iam/oauth/authorize (transparent proxy mounted by the
// platform plugin), so _superusers does not need a built-in auth
// surface either.
//
// The handler is kept (rather than deleted alongside the route
// bindings) so callers attaching their own legacy-style routes via
// e.Router can still opt in to the 410-Gone-with-Location behavior
// without re-implementing the lookup. It is currently unbound.
//
// 410 Gone (not 403) is intentional — it signals the endpoint is
// permanently retired, not temporarily forbidden, and lets clients
// stop retrying. The Location header points at the IAM equivalent
// so a redirecting client lands on the right page.
func externalAuthGuard() *hook.Handler[*core.RequestEvent] {
	return &hook.Handler[*core.RequestEvent]{
		Id: "baseExternalAuthGuard",
		Func: func(e *core.RequestEvent) error {
			externalOnly, _ := e.App.Store().Get(StoreKeyExternalAuthOnly).(bool)
			if !externalOnly {
				return e.Next()
			}

			jwksURL, _ := e.App.Store().Get(StoreKeyJWKSURL).(string)
			if location := iamReplacementURL(e.Request.URL.Path, jwksURL); location != "" {
				e.Response.Header().Set("Location", location)
			}
			return e.Error(
				http.StatusGone,
				"This endpoint is retired — Hanzo Base auth is delegated to IAM. "+
					"See the Location header or the configured IAM_ENDPOINT.",
				nil,
			)
		},
	}
}

// iamReplacementURL maps a retired Base auth path to the IAM endpoint
// that replaces it. Returns "" when no public IAM equivalent exists
// (e.g. local OTP — IAM owns MFA internally, there is no public surface).
//
// jwksURL is the configured `${IAM_ENDPOINT}/.well-known/jwks` — we
// strip the suffix to recover the base URL.
func iamReplacementURL(reqPath, jwksURL string) string {
	if jwksURL == "" {
		return ""
	}
	const jwksSuffix = "/.well-known/jwks"
	base := strings.TrimSuffix(jwksURL, jwksSuffix)
	switch {
	case strings.HasSuffix(reqPath, "/request-password-reset"),
		strings.HasSuffix(reqPath, "/confirm-password-reset"):
		return base + "/forget"
	case strings.HasSuffix(reqPath, "/request-email-change"),
		strings.HasSuffix(reqPath, "/confirm-email-change"):
		return base + "/account"
	case strings.HasSuffix(reqPath, "/request-verification"),
		strings.HasSuffix(reqPath, "/confirm-verification"):
		// IAM auto-verifies on signup; no public confirm endpoint.
		return ""
	case strings.HasSuffix(reqPath, "/request-otp"),
		strings.HasSuffix(reqPath, "/auth-with-otp"):
		// IAM owns MFA; no public OTP request endpoint.
		return ""
	case strings.HasSuffix(reqPath, "/auth-with-password"),
		strings.HasSuffix(reqPath, "/auth-with-oauth2"):
		return base + "/oauth/authorize"
	}
	return ""
}
