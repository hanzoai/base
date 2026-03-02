package apis

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/hanzoai/base/core"
	"github.com/hanzoai/base/tools/hook"
	"github.com/hanzoai/base/tools/list"
	"github.com/hanzoai/base/tools/router"
	"github.com/hanzoai/base/tools/routine"
	"github.com/hanzoai/base/tools/security"
	"github.com/spf13/cast"
)

// Common request event store keys used by the middlewares and api handlers.
const (
	RequestEventKeyLogMeta = "baseLogMeta" // extra data to store with the request activity log

	requestEventKeyExecStart              = "__execStart"                 // the value must be time.Time
	requestEventKeySkipSuccessActivityLog = "__skipSuccessActivityLogger" // the value must be bool
)

const (
	DefaultWWWRedirectMiddlewarePriority = -99999
	DefaultWWWRedirectMiddlewareId       = "baseWWWRedirect"

	DefaultActivityLoggerMiddlewarePriority   = DefaultRateLimitMiddlewarePriority - 40
	DefaultActivityLoggerMiddlewareId         = "baseActivityLogger"
	DefaultSkipSuccessActivityLogMiddlewareId = "baseSkipSuccessActivityLog"
	DefaultEnableAuthIdActivityLog            = "baseEnableAuthIdActivityLog"

	DefaultPanicRecoverMiddlewarePriority = DefaultRateLimitMiddlewarePriority - 30
	DefaultPanicRecoverMiddlewareId       = "basePanicRecover"

	DefaultLoadAuthTokenMiddlewarePriority = DefaultRateLimitMiddlewarePriority - 20
	DefaultLoadAuthTokenMiddlewareId       = "baseLoadAuthToken"

	DefaultSecurityHeadersMiddlewarePriority = DefaultRateLimitMiddlewarePriority - 10
	DefaultSecurityHeadersMiddlewareId       = "baseSecurityHeaders"

	DefaultRequireGuestOnlyMiddlewareId                 = "baseRequireGuestOnly"
	DefaultRequireAuthMiddlewareId                      = "baseRequireAuth"
	DefaultRequireSuperuserAuthMiddlewareId             = "baseRequireSuperuserAuth"
	DefaultRequireSuperuserOrOwnerAuthMiddlewareId      = "baseRequireSuperuserOrOwnerAuth"
	DefaultRequireSameCollectionContextAuthMiddlewareId = "baseRequireSameCollectionContextAuth"
)

// RequireGuestOnly middleware requires a request to NOT have a valid
// Authorization header.
//
// This middleware is the opposite of [apis.RequireAuth()].
func RequireGuestOnly() *hook.Handler[*core.RequestEvent] {
	return &hook.Handler[*core.RequestEvent]{
		Id: DefaultRequireGuestOnlyMiddlewareId,
		Func: func(e *core.RequestEvent) error {
			if e.Auth != nil {
				return router.NewBadRequestError("The request can be accessed only by guests.", nil)
			}

			return e.Next()
		},
	}
}

// RequireAuth middleware requires a request to have a valid record Authorization header.
//
// The auth record could be from any collection.
// You can further filter the allowed record auth collections by specifying their names.
//
// Example:
//
//	apis.RequireAuth()                      // any auth collection
//	apis.RequireAuth("_superusers", "users") // only the listed auth collections
func RequireAuth(optCollectionNames ...string) *hook.Handler[*core.RequestEvent] {
	return &hook.Handler[*core.RequestEvent]{
		Id:   DefaultRequireAuthMiddlewareId,
		Func: requireAuth(optCollectionNames...),
	}
}

func requireAuth(optCollectionNames ...string) func(*core.RequestEvent) error {
	return func(e *core.RequestEvent) error {
		if e.Auth == nil {
			return e.UnauthorizedError("The request requires valid record authorization token.", nil)
		}

		// check record collection name
		if len(optCollectionNames) > 0 && !slices.Contains(optCollectionNames, e.Auth.Collection().Name) {
			return e.ForbiddenError("The authorized record is not allowed to perform this action.", nil)
		}

		return e.Next()
	}
}

// RequireSuperuserAuth middleware requires a request to have
// a valid superuser Authorization header.
func RequireSuperuserAuth() *hook.Handler[*core.RequestEvent] {
	return &hook.Handler[*core.RequestEvent]{
		Id:   DefaultRequireSuperuserAuthMiddlewareId,
		Func: requireAuth(core.CollectionNameSuperusers),
	}
}

// RequireSuperuserOrOwnerAuth middleware requires a request to have
// a valid superuser or regular record owner Authorization header set.
//
// This middleware is similar to [apis.RequireAuth()] but
// for the auth record token expects to have the same id as the path
// parameter ownerIdPathParam (default to "id" if empty).
func RequireSuperuserOrOwnerAuth(ownerIdPathParam string) *hook.Handler[*core.RequestEvent] {
	return &hook.Handler[*core.RequestEvent]{
		Id: DefaultRequireSuperuserOrOwnerAuthMiddlewareId,
		Func: func(e *core.RequestEvent) error {
			if e.Auth == nil {
				return e.UnauthorizedError("The request requires superuser or record authorization token.", nil)
			}

			if e.Auth.IsSuperuser() {
				return e.Next()
			}

			if ownerIdPathParam == "" {
				ownerIdPathParam = "id"
			}
			ownerId := e.Request.PathValue(ownerIdPathParam)

			// note: it is considered "safe" to compare only the record id
			// since the auth record ids are treated as unique across all auth collections
			if e.Auth.Id != ownerId {
				return e.ForbiddenError("You are not allowed to perform this request.", nil)
			}

			return e.Next()
		},
	}
}

// RequireSameCollectionContextAuth middleware requires a request to have
// a valid record Authorization header and the auth record's collection to
// match the one from the route path parameter (default to "collection" if collectionParam is empty).
func RequireSameCollectionContextAuth(collectionPathParam string) *hook.Handler[*core.RequestEvent] {
	return &hook.Handler[*core.RequestEvent]{
		Id: DefaultRequireSameCollectionContextAuthMiddlewareId,
		Func: func(e *core.RequestEvent) error {
			if e.Auth == nil {
				return e.UnauthorizedError("The request requires valid record authorization token.", nil)
			}

			if collectionPathParam == "" {
				collectionPathParam = "collection"
			}

			collection, _ := e.App.FindCachedCollectionByNameOrId(e.Request.PathValue(collectionPathParam))
			if collection == nil || e.Auth.Collection().Id != collection.Id {
				return e.ForbiddenError(fmt.Sprintf("The request requires auth record from %s collection.", e.Auth.Collection().Name), nil)
			}

			return e.Next()
		},
	}
}

// Store keys for OIDC/JWKS-based external auth provider integration.
// Set these via app.Store() from the platform plugin or manually.
const (
	// StoreKeyJWKSURL is the JWKS endpoint URL for the identity provider
	// (e.g., "https://auth.example.com/.well-known/jwks").
	// When set, loadAuthToken validates bearer tokens against this endpoint.
	StoreKeyJWKSURL = "jwksURL"

	// StoreKeyAuthUsersCollection is the name of the auth collection to
	// find/create externally-authenticated user records in (default: "users").
	StoreKeyAuthUsersCollection = "authUsersCollection"

	// StoreKeyExternalAuthOnly controls whether the external identity provider
	// (OIDC/JWKS) is the exclusive authentication source. When true:
	//   - loadAuthToken tries JWKS first; local tokens are only accepted for
	//     the _superusers collection (admin panel).
	//   - Built-in auth endpoints (password, OTP, email-change, password-reset,
	//     verification) are disabled for non-superuser collections.
	//
	// Set automatically by the platform plugin when an auth endpoint is configured.
	StoreKeyExternalAuthOnly = "externalAuthOnly"

	// Deprecated: use StoreKeyJWKSURL. Kept for backward compatibility.
	StoreKeyIAMJWKSURL         = StoreKeyJWKSURL
	StoreKeyIAMUsersCollection = StoreKeyAuthUsersCollection
	StoreKeyIAMOnly            = StoreKeyExternalAuthOnly
)

// shared JWKS cache for external token validation (10 minute TTL on keys).
var jwksCache = security.NewJWKSCache(10 * time.Minute)

// loadAuthToken attempts to load the auth context based on the "Authorization: TOKEN" header value.
//
// This middleware does nothing in case of:
//   - missing, invalid or expired token
//   - e.Auth is already loaded by another middleware
//
// This middleware is registered by default for all routes.
//
// When app.Store() contains a "jwksURL" value, the middleware validates bearer
// tokens against the external identity provider's JWKS endpoint. If the
// validation succeeds, a corresponding user record is found or auto-created
// in the auth collection (configurable via "authUsersCollection" store key,
// default: "users").
//
// When StoreKeyExternalAuthOnly is true (set by the platform plugin), JWKS is
// the primary auth mechanism. Local Base tokens are only accepted for the
// _superusers collection (admin panel sessions). All other local tokens are
// rejected — users must authenticate via the configured identity provider.
//
// Note: We don't throw an error on invalid or expired token to allow
// users to extend with their own custom handling in external middleware(s).
func loadAuthToken() *hook.Handler[*core.RequestEvent] {
	return &hook.Handler[*core.RequestEvent]{
		Id:       DefaultLoadAuthTokenMiddlewareId,
		Priority: DefaultLoadAuthTokenMiddlewarePriority,
		Func: func(e *core.RequestEvent) error {
			// already loaded by another middleware
			if e.Auth != nil {
				return e.Next()
			}

			token := getAuthTokenFromRequest(e)
			if token == "" {
				return e.Next()
			}

			externalOnly, _ := e.App.Store().Get(StoreKeyExternalAuthOnly).(bool)
			jwksURL, _ := e.App.Store().Get(StoreKeyJWKSURL).(string)

			if externalOnly && jwksURL != "" {
				// External-only mode: IAM (JWKS) is the ONLY auth mechanism.
				// No local tokens, no superuser fallback. All auth goes through IAM.
				record, jwksErr := resolveJWKSToken(e, token, jwksURL)
				if jwksErr == nil && record != nil {
					e.Auth = record
					return e.Next()
				}

				// JWKS failed — continue without auth (no fallback).
				if jwksErr != nil {
					e.App.Logger().Debug("loadAuthToken: IAM JWKS validation failed",
						"error", jwksErr,
					)
				}
				return e.Next()
			}

			// Standard mode: try local first, fall back to JWKS.
			record, err := e.App.FindAuthRecordByToken(token, core.TokenTypeAuth)
			if err == nil && record != nil {
				e.Auth = record
				return e.Next()
			}

			// Local validation failed — try JWKS if configured.
			if jwksURL == "" {
				if err != nil {
					e.App.Logger().Debug("loadAuthToken: local token validation failed", "error", err)
				}
				return e.Next()
			}

			jwksRecord, jwksErr := resolveJWKSToken(e, token, jwksURL)
			if jwksErr != nil {
				e.App.Logger().Warn("loadAuthToken: JWKS validation failed",
					"localError", err,
					"jwksError", jwksErr,
				)
			} else if jwksRecord != nil {
				e.Auth = jwksRecord
			}

			return e.Next()
		},
	}
}

// resolveJWKSToken validates a JWT against the configured JWKS endpoint and
// returns the corresponding Base user record (creating one if it doesn't exist).
//
// Standard OIDC claims extracted: sub, email, name, preferred_username, owner.
func resolveJWKSToken(e *core.RequestEvent, token, jwksURL string) (*core.Record, error) {
	ctx, cancel := context.WithTimeout(e.Request.Context(), 10*time.Second)
	defer cancel()

	claims, err := security.ParseJWTWithJWKS(ctx, token, jwksURL, jwksCache)
	if err != nil {
		return nil, fmt.Errorf("jwks validation: %w", err)
	}

	// Extract standard OIDC claims.
	// "sub" is the primary identifier (RFC 7519). Fall back to
	// "preferred_username" then "name" for compatibility.
	sub, _ := claims["sub"].(string)
	if sub == "" {
		sub, _ = claims["preferred_username"].(string)
	}
	if sub == "" {
		sub, _ = claims["name"].(string)
	}
	if sub == "" {
		return nil, errors.New("token missing sub/preferred_username/name claim")
	}

	owner, _ := claims["owner"].(string)
	email, _ := claims["email"].(string)
	name, _ := claims["name"].(string)
	displayName, _ := claims["displayName"].(string)
	if name == "" {
		name = displayName
	}

	// Store resolved claims on the request for downstream middleware.
	e.Set("authSub", sub)
	e.Set("authOwner", owner)
	e.Set("authEmail", email)
	e.Set("authName", name)

	// Determine which collection to use.
	collectionName := "users"
	if v, _ := e.App.Store().Get(StoreKeyAuthUsersCollection).(string); v != "" {
		collectionName = v
	}

	collection, err := e.App.FindCachedCollectionByNameOrId(collectionName)
	if err != nil {
		return nil, fmt.Errorf("auth users collection %q not found: %w", collectionName, err)
	}

	if !collection.IsAuth() {
		return nil, fmt.Errorf("collection %q is not an auth collection", collectionName)
	}

	// Base record IDs must be exactly 15 lowercase alphanumeric characters.
	// External subs can be UUIDs (36 chars) or short slugs — normalize to 15.
	recordID := subToRecordID(sub)

	// Try to find existing user by sub (mapped to Base record ID).
	record, err := e.App.FindRecordById(collection, recordID)
	if err == nil {
		return record, nil
	}

	// Try by email as fallback.
	if email != "" {
		record, err = e.App.FindAuthRecordByEmail(collection, email)
		if err == nil {
			return record, nil
		}
	}

	// Auto-create a new Base user record for this external identity.
	record = core.NewRecord(collection)
	record.Id = recordID
	record.Set("email", email)
	if name != "" {
		record.Set("name", name)
	}

	// Set org_id if the collection has that field and the claim is present.
	if owner != "" && collection.Fields.GetByName("org_id") != nil {
		record.Set("org_id", owner)
	}

	// Set defaults for common fields if they exist on the collection.
	if f := collection.Fields.GetByName("role"); f != nil {
		record.Set("role", "user")
	}
	if f := collection.Fields.GetByName("status"); f != nil {
		record.Set("status", "active")
	}
	if f := collection.Fields.GetByName("kyc_status"); f != nil {
		record.Set("kyc_status", "none")
	}

	// External users authenticate via OIDC tokens, not local passwords.
	record.SetRandomPassword()

	// Mark email as verified — the identity provider already verified it.
	record.SetVerified(true)

	if err := e.App.Save(record); err != nil {
		// Save failed (e.g. collection rules block creation).
		// In IAM-only mode, the record still serves as a valid auth context
		// with all claims populated — just not persisted to the local DB.
		// This is intentional: IAM is the user store, not Base.
		e.App.Logger().Debug("IAM user not persisted locally (IAM is source of truth)",
			slog.String("sub", sub),
			slog.String("email", email),
			slog.String("error", err.Error()),
		)
		return record, nil
	}

	e.App.Logger().Info("auto-created user from external token",
		slog.String("sub", sub),
		slog.String("email", email),
		slog.String("collection", collectionName),
	)

	return record, nil
}

// subToRecordID converts an OIDC sub claim (which may be a UUID, slug, or
// other identifier) into a valid Base record ID (exactly 15 lowercase
// alphanumeric chars).
//
// Short subs (< 15 chars) are padded with underscores (kept for backward compat).
// Long or non-alphanumeric subs are SHA-256 hashed and truncated to 15 hex chars.
func subToRecordID(sub string) string {
	// Fast path: if already a valid 15-char lowercase alphanumeric ID, use as-is.
	if len(sub) == 15 && isLowerAlphanumeric(sub) {
		return sub
	}

	// For short subs that are alphanumeric, pad to 15 (backward compat).
	if len(sub) < 15 && isLowerAlphanumeric(sub) {
		for len(sub) < 15 {
			sub += "_"
		}
		return sub
	}

	// For UUIDs, long strings, or non-alphanumeric subs: deterministic hash.
	// SHA-256 the original sub and take the first 15 hex chars.
	h := sha256.Sum256([]byte(sub))
	return hex.EncodeToString(h[:])[:15]
}

// isLowerAlphanumeric returns true if s contains only [a-z0-9_].
func isLowerAlphanumeric(s string) bool {
	for _, c := range s {
		if !((c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '_') {
			return false
		}
	}
	return true
}

func getAuthTokenFromRequest(e *core.RequestEvent) string {
	token := e.Request.Header.Get("Authorization")

	// Fall back to X-Authorization (alias when Authorization is consumed by a proxy/CDN).
	if token == "" {
		token = e.Request.Header.Get("X-Authorization")
	}

	// Fall back to legacy X-Auth-Token header.
	if token == "" {
		token = e.Request.Header.Get("X-Auth-Token")
	}

	// Strip optional "Bearer " prefix for compatibility with standard HTTP clients.
	if len(token) > 7 && strings.EqualFold(token[:7], "Bearer ") {
		return token[7:]
	}

	return token
}

// wwwRedirect performs www->non-www redirect(s) if the request host
// matches with one of the values in redirectHosts.
//
// This middleware is registered by default on Serve for all routes.
func wwwRedirect(redirectHosts []string) *hook.Handler[*core.RequestEvent] {
	return &hook.Handler[*core.RequestEvent]{
		Id:       DefaultWWWRedirectMiddlewareId,
		Priority: DefaultWWWRedirectMiddlewarePriority,
		Func: func(e *core.RequestEvent) error {
			host := e.Request.Host

			if strings.HasPrefix(host, "www.") && list.ExistInSlice(host, redirectHosts) {
				// note: e.Request.URL.Scheme would be empty
				schema := "http://"
				if e.IsTLS() {
					schema = "https://"
				}

				return e.Redirect(
					http.StatusTemporaryRedirect,
					(schema + host[4:] + e.Request.RequestURI),
				)
			}

			return e.Next()
		},
	}
}

// panicRecover returns a default panic-recover handler.
func panicRecover() *hook.Handler[*core.RequestEvent] {
	return &hook.Handler[*core.RequestEvent]{
		Id:       DefaultPanicRecoverMiddlewareId,
		Priority: DefaultPanicRecoverMiddlewarePriority,
		Func: func(e *core.RequestEvent) (err error) {
			// panic-recover
			defer func() {
				recoverResult := recover()
				if recoverResult == nil {
					return
				}

				recoverErr, ok := recoverResult.(error)
				if !ok {
					recoverErr = fmt.Errorf("%v", recoverResult)
				} else if errors.Is(recoverErr, http.ErrAbortHandler) {
					// don't recover ErrAbortHandler so the response to the client can be aborted
					panic(recoverResult)
				}

				stack := make([]byte, 2<<10) // 2 KB
				length := runtime.Stack(stack, true)
				err = e.InternalServerError("", fmt.Errorf("[PANIC RECOVER] %w %s", recoverErr, stack[:length]))
			}()

			err = e.Next()

			return err
		},
	}
}

// securityHeaders middleware adds common security headers to the response.
//
// This middleware is registered by default for all routes.
func securityHeaders() *hook.Handler[*core.RequestEvent] {
	return &hook.Handler[*core.RequestEvent]{
		Id:       DefaultSecurityHeadersMiddlewareId,
		Priority: DefaultSecurityHeadersMiddlewarePriority,
		Func: func(e *core.RequestEvent) error {
			e.Response.Header().Set("X-XSS-Protection", "1; mode=block")
			e.Response.Header().Set("X-Content-Type-Options", "nosniff")
			e.Response.Header().Set("X-Frame-Options", "SAMEORIGIN")

			// @todo consider a default HSTS?
			// (see also https://webkit.org/blog/8146/protecting-against-hsts-abuse/)

			return e.Next()
		},
	}
}

// SkipSuccessActivityLog is a helper middleware that instructs the global
// activity logger to log only requests that have failed/returned an error.
func SkipSuccessActivityLog() *hook.Handler[*core.RequestEvent] {
	return &hook.Handler[*core.RequestEvent]{
		Id: DefaultSkipSuccessActivityLogMiddlewareId,
		Func: func(e *core.RequestEvent) error {
			e.Set(requestEventKeySkipSuccessActivityLog, true)
			return e.Next()
		},
	}
}

// activityLogger middleware takes care to save the request information
// into the logs database.
//
// This middleware is registered by default for all routes.
//
// The middleware does nothing if the app logs retention period is zero
// (aka. app.Settings().Logs.MaxDays = 0).
//
// Users can attach the [apis.SkipSuccessActivityLog()] middleware if
// you want to log only the failed requests.
func activityLogger() *hook.Handler[*core.RequestEvent] {
	return &hook.Handler[*core.RequestEvent]{
		Id:       DefaultActivityLoggerMiddlewareId,
		Priority: DefaultActivityLoggerMiddlewarePriority,
		Func: func(e *core.RequestEvent) error {
			e.Set(requestEventKeyExecStart, time.Now())

			err := e.Next()

			logRequest(e, err)

			return err
		},
	}
}

func logRequest(event *core.RequestEvent, err error) {
	// no logs retention
	if event.App.Settings().Logs.MaxDays == 0 {
		return
	}

	// the non-error route has explicitly disabled the activity logger
	if err == nil && event.Get(requestEventKeySkipSuccessActivityLog) != nil {
		return
	}

	attrs := make([]any, 0, 15)

	attrs = append(attrs, slog.String("type", "request"))

	started := cast.ToTime(event.Get(requestEventKeyExecStart))
	if !started.IsZero() {
		attrs = append(attrs, slog.Float64("execTime", float64(time.Since(started))/float64(time.Millisecond)))
	}

	if meta := event.Get(RequestEventKeyLogMeta); meta != nil {
		attrs = append(attrs, slog.Any("meta", meta))
	}

	status := event.Status()
	method := cutStr(strings.ToUpper(event.Request.Method), 50)
	requestUri := cutStr(event.Request.URL.RequestURI(), 3000)

	// parse the request error
	if err != nil {
		apiErr, isPlainApiError := err.(*router.ApiError)
		if isPlainApiError || errors.As(err, &apiErr) {
			// the status header wasn't written yet
			if status == 0 {
				status = apiErr.Status
			}

			var errMsg string
			if isPlainApiError {
				errMsg = apiErr.Message
			} else {
				// wrapped ApiError -> add the full serialized version
				// of the original error since it could contain more information
				errMsg = err.Error()
			}

			attrs = append(
				attrs,
				slog.String("error", errMsg),
				slog.Any("details", apiErr.RawData()),
			)
		} else {
			attrs = append(attrs, slog.String("error", err.Error()))
		}
	}

	attrs = append(
		attrs,
		slog.String("url", requestUri),
		slog.String("method", method),
		slog.Int("status", status),
		slog.String("referer", cutStr(event.Request.Referer(), 2000)),
		slog.String("userAgent", cutStr(event.Request.UserAgent(), 2000)),
	)

	if event.Auth != nil {
		attrs = append(attrs, slog.String("auth", event.Auth.Collection().Name))

		if event.App.Settings().Logs.LogAuthId {
			attrs = append(attrs, slog.String("authId", event.Auth.Id))
		}
	} else {
		attrs = append(attrs, slog.String("auth", ""))
	}

	if event.App.Settings().Logs.LogIP {
		attrs = append(
			attrs,
			slog.String("userIP", event.RealIP()),
			slog.String("remoteIP", event.RemoteIP()),
		)
	}

	// don't block on logs write
	routine.FireAndForget(func() {
		message := method + " "

		if escaped, unescapeErr := url.PathUnescape(requestUri); unescapeErr == nil {
			message += escaped
		} else {
			message += requestUri
		}

		if err != nil {
			event.App.Logger().Error(message, attrs...)
		} else {
			event.App.Logger().Info(message, attrs...)
		}
	})
}

func cutStr(str string, max int) string {
	if len(str) > max {
		return str[:max] + "..."
	}
	return str
}
