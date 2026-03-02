package apis

import (
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

	sub.POST("/request-otp", recordRequestOTP).Bind(
		externalAuthGuard(),
		collectionPathRateLimit("", "requestOTP"),
	)
	sub.POST("/auth-with-otp", recordAuthWithOTP).Bind(
		externalAuthGuard(),
		collectionPathRateLimit("", "authWithOTP", "auth"),
	)

	sub.POST("/request-password-reset", recordRequestPasswordReset).Bind(
		externalAuthGuard(),
		collectionPathRateLimit("", "requestPasswordReset"),
	)
	sub.POST("/confirm-password-reset", recordConfirmPasswordReset).Bind(
		externalAuthGuard(),
		collectionPathRateLimit("", "confirmPasswordReset"),
	)

	sub.POST("/request-verification", recordRequestVerification).Bind(
		externalAuthGuard(),
		collectionPathRateLimit("", "requestVerification"),
	)
	sub.POST("/confirm-verification", recordConfirmVerification).Bind(
		externalAuthGuard(),
		collectionPathRateLimit("", "confirmVerification"),
	)

	sub.POST("/request-email-change", recordRequestEmailChange).Bind(
		externalAuthGuard(),
		collectionPathRateLimit("", "requestEmailChange"),
		RequireSameCollectionContextAuth(""),
	)
	sub.POST("/confirm-email-change", recordConfirmEmailChange).Bind(
		externalAuthGuard(),
		collectionPathRateLimit("", "confirmEmailChange"),
	)

	sub.POST("/impersonate/{id}", recordAuthImpersonate).Bind(RequireSuperuserAuth())
}

func findAuthCollection(e *core.RequestEvent) (*core.Collection, error) {
	collection, err := e.App.FindCachedCollectionByNameOrId(e.Request.PathValue("collection"))

	if err != nil || !collection.IsAuth() {
		return nil, e.NotFoundError("Missing or invalid auth collection context.", err)
	}

	return collection, nil
}

// externalAuthGuard returns a middleware that blocks built-in auth endpoints
// when external-only mode is active (StoreKeyExternalAuthOnly == true).
//
// The _superusers collection is exempt because the admin panel uses Base's
// built-in OAuth2 flow (which redirects to the identity provider) to
// establish admin sessions.
//
// When blocked, returns 403 directing users to the configured identity provider.
func externalAuthGuard() *hook.Handler[*core.RequestEvent] {
	return &hook.Handler[*core.RequestEvent]{
		Id: "baseExternalAuthGuard",
		Func: func(e *core.RequestEvent) error {
			externalOnly, _ := e.App.Store().Get(StoreKeyExternalAuthOnly).(bool)
			if !externalOnly {
				return e.Next()
			}

			// Allow _superusers — admin panel login uses built-in OAuth2.
			collectionName := e.Request.PathValue("collection")
			if collectionName == core.CollectionNameSuperusers {
				return e.Next()
			}

			return e.ForbiddenError(
				"Direct authentication is disabled. Use the configured identity provider.",
				nil,
			)
		},
	}
}
