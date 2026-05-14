package apis

import (
	"net/http"
	"strings"

	"github.com/hanzoai/base/core"
	"github.com/hanzoai/base/tools/hook"
	"github.com/hanzoai/base/tools/router"
)

// bindRecordAuthApi registers the auth record api endpoints and
// the corresponding handlers.
//
// When StoreKeyExternalAuthOnly is true (set by the platform plugin), all
// built-in auth endpoints except auth-methods, auth-refresh, impersonate, and
// oauth2-redirect are blocked for non-superuser collections. Users must
// authenticate via the configured identity provider instead.
func bindRecordAuthApi(app core.App, rg *router.RouterGroup[*core.RequestEvent]) {
	// global oauth2 subscription redirect handler (always allowed — needed for OAuth2 flow)
	rg.GET("/oauth2-redirect", oauth2SubscriptionRedirect).Bind(
		SkipSuccessActivityLog(), // skip success log as it could contain sensitive information in the url
	)
	// add again as POST in case of response_mode=form_post
	rg.POST("/oauth2-redirect", oauth2SubscriptionRedirect).Bind(
		SkipSuccessActivityLog(), // skip success log as it could contain sensitive information in the url
	)

	sub := rg.Group("/collections/{collection}")

	// These endpoints are always available (read-only or refresh existing sessions).
	sub.GET("/auth-methods", recordAuthMethods).Bind(
		collectionPathRateLimit("", "listAuthMethods"),
	)

	sub.POST("/auth-refresh", recordAuthRefresh).Bind(
		collectionPathRateLimit("", "authRefresh"),
		RequireSameCollectionContextAuth(""),
	)

	// Built-in auth endpoints — blocked in external-auth-only mode (except for _superusers).
	sub.POST("/auth-with-password", recordAuthWithPassword).Bind(
		externalAuthGuard(),
		collectionPathRateLimit("", "authWithPassword", "auth"),
	)

	sub.POST("/auth-with-oauth2", recordAuthWithOAuth2).Bind(
		externalAuthGuard(),
		collectionPathRateLimit("", "authWithOAuth2", "auth"),
	)

	// auth-with-otp is the OTP-completion endpoint (challenge already
	// issued elsewhere). Kept ONLY for the _superusers admin login
	// transition path; the guard 410s it for every other collection.
	sub.POST("/auth-with-otp", recordAuthWithOTP).Bind(
		externalAuthGuard(),
		collectionPathRateLimit("", "authWithOTP", "auth"),
	)

	// The legacy local-only request/confirm flows
	//   /request-otp /request-password-reset /confirm-password-reset
	//   /request-email-change /confirm-email-change
	//   /request-verification /confirm-verification
	//   /impersonate/{id}
	// were removed in the IAM-native rip. IAM owns password recovery,
	// email change, MFA/OTP issuance, and impersonation. Clients that
	// hit a stale URL get a generic 404 from the router — which is
	// the correct signal for a permanently-removed endpoint that no
	// longer has even a deprecated handler bound.
}

func findAuthCollection(e *core.RequestEvent) (*core.Collection, error) {
	collection, err := e.App.FindCachedCollectionByNameOrId(e.Request.PathValue("collection"))

	if err != nil || !collection.IsAuth() {
		return nil, e.NotFoundError("Missing or invalid auth collection context.", err)
	}

	return collection, nil
}

// externalAuthGuard returns a middleware that returns 410 Gone for the
// legacy built-in auth endpoints when external-only mode is active
// (StoreKeyExternalAuthOnly == true) — which is the only mode the
// platform plugin allows. There is no "local password / OTP / MFA"
// path anymore: Hanzo IAM is the only auth source.
//
// The _superusers collection is still exempt here because the admin
// panel UI hasn't been switched to the IAM-redirect login flow yet
// (tracked: admin-panel IAM login follow-up). Once that lands, this
// exemption + the whole legacy apis/record_auth_* group goes away.
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

			// Allow _superusers — admin panel login still uses built-in
			// OAuth2 redirects until we cut admin to IAM-only login.
			collectionName := e.Request.PathValue("collection")
			if collectionName == core.CollectionNameSuperusers {
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
	case strings.HasSuffix(reqPath, "/auth-with-password"):
		return base + "/oauth/authorize"
	}
	return ""
}
