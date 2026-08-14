package apis

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/hanzoai/authz"
	"github.com/hanzoai/base/core"
	"github.com/hanzoai/base/tools/claims"
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

	// RequestEventKeyOrgs carries the org slugs the verified token asserts, as
	// []string. Set by resolveJWKSToken; read by anything that answers a
	// per-org question.
	RequestEventKeyOrgs = "authOrgs"

	// RequestEventKeyOrg carries the one org the request acts in, as a string —
	// the org resolved from the verified token, and the org whose Base is
	// serving. Set by resolveJWKSToken.
	RequestEventKeyOrg = "authOrg"

	// RequestEventKeySub carries the subject the credential names, as a string.
	// Every door that resolves a credential sets it, which is what lets anything
	// downstream name the caller without knowing which door it came through — an
	// IAM key mints no auth record, so e.Auth answers nothing for one.
	RequestEventKeySub = "authSub"

	// RequestEventKeyOrgAdmin reports, as a bool, whether the credential carries
	// authority over the org it acts in rather than only over its own subject.
	// A member's token is not that; an org admin's token and an org's secret key
	// are. Set by whichever door resolved the credential.
	RequestEventKeyOrgAdmin = "authOrgAdmin"

	// requestEventKeyStatedOrg carries the org a request SAID it meant, read off
	// X-Org-Id before that header is deleted. It is a statement of intent and
	// never an identity — see loadAuthToken.
	requestEventKeyStatedOrg = "__statedOrg"

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
	// (e.g., "https://auth.example.com/v1/iam/.well-known/jwks").
	// When set, loadAuthToken validates bearer tokens against this endpoint.
	StoreKeyJWKSURL = "jwksURL"

	// StoreKeyAuthUsersCollection is the name of the auth collection to
	// find/create externally-authenticated user records in (default: "users").
	StoreKeyAuthUsersCollection = "authUsersCollection"

	// StoreKeyExternalAuthOnly controls whether the external identity
	// provider (OIDC/JWKS via Hanzo IAM) is the exclusive auth source.
	// In Hanzo Base this is always true once the platform plugin
	// registers — the legacy local-password / OTP / MFA / impersonate
	// surfaces have been removed (returning 404 from the router).
	// There is no exemption, _superusers included.
	StoreKeyExternalAuthOnly = "externalAuthOnly"

	// StoreKeyBases holds the [Bases] of the deployment. Set by the org plugin.
	StoreKeyBases = "bases"
)

// Bases answers which Base serves an org.
//
// One Base per org is the tenancy model whole, so this is the one lookup in it.
// A deployment that registers this has a Base per org; a deployment that does
// not has exactly one Base, the process's own, for everybody — a single-tenant
// Base with no other Base for a request to have missed.
type Bases func(org string) (core.App, error)

// refusal is what a VERIFIED token produces when Base will not serve it.
//
// The distinction from an ordinary validation error is the whole point. A token
// that does not verify is simply not authentication and the request carries on
// as a guest; a token that verifies but resolves to no Base must stop the
// request, because carrying on would serve it from whichever Base happened to
// be bound — the fallthrough this exists to remove.
type refusal struct{ err error }

func (r refusal) Error() string { return r.err.Error() }
func (r refusal) Unwrap() error { return r.err }

// iamJWKSPath is IAM's canonical JWKS endpoint (HIP-0111). It is the one
// suffix that turns StoreKeyJWKSURL back into the IAM origin, which is
// what the retired-endpoint pointers and the auth-methods authorize URL
// are built from.
const iamJWKSPath = "/v1/iam/.well-known/jwks"

// iamOrigin recovers the IAM origin from the configured JWKS URL.
func iamOrigin(jwksURL string) string {
	return strings.TrimSuffix(jwksURL, iamJWKSPath)
}

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
// When StoreKeyExternalAuthOnly is true (set by the platform plugin), IAM is the
// ONLY auth mechanism. No local Base token is accepted for any collection,
// _superusers included: a second way to reach the widest authority in the
// process is a second thing to get wrong.
//
// It also decides WHICH BASE serves the request, because that follows from the
// same verified token and deciding it anywhere else means deciding it twice.
// A request carrying no credential at all reaches no org and is served by the
// process's own Base: with no identity there is no tenant, and what it can read
// there is what an anonymous caller's rules allow. A credential that verifies
// but resolves to no org is refused rather than served from there — see orgOf.
//
// Note: We don't throw an error on invalid or expired token to allow
// users to extend with their own custom handling in external middleware(s).
func loadAuthToken() *hook.Handler[*core.RequestEvent] {
	return &hook.Handler[*core.RequestEvent]{
		Id:       DefaultLoadAuthTokenMiddlewareId,
		Priority: DefaultLoadAuthTokenMiddlewarePriority,
		Func: func(e *core.RequestEvent) error {
			// The identity headers are the gateway's to write from a token it
			// validated. Arriving from a client they are a client saying who it
			// is, so none of them is believed: X-Org-Id is read once, as the org
			// a request SAYS it means, and every one of them is then deleted so
			// that nothing downstream can mistake an assertion for an identity.
			//
			// The stated org buys the caller nothing on its own — orgOf admits
			// it only where the token already carries it. It is read at all so
			// that naming someone else's org can be answered with a refusal
			// instead of silently with the caller's own data.
			e.Set(requestEventKeyStatedOrg, e.Request.Header.Get(claims.HeaderOrgID))
			claims.StripIdentityHeaders(e.Request.Header)

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

			if externalOnly {
				// IAM IS THE ONLY AUTH. There is no second way in, and that is
				// the whole of this branch. _superusers is not an exception:
				// it reaches schema, settings, backups and logs, and one
				// process serves many orgs' Bases, so two independently-keyed
				// doors to it would be one more than can be reasoned about.
				//
				// A token is what IAM says it is. If IAM cannot say, the
				// request is unauthenticated and the rules decide what an
				// anonymous caller may do.
				if jwksURL != "" {
					record, jwksErr := resolveJWKSToken(e, token, jwksURL)
					if jwksErr == nil && record != nil {
						e.Auth = record
						return e.Next()
					}
					var stop refusal
					if errors.As(jwksErr, &stop) {
						return stop.err
					}
					if jwksErr != nil {
						e.App.Logger().Debug("loadAuthToken: IAM JWKS validation failed",
							"error", jwksErr,
						)
					}
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
			var stop refusal
			if errors.As(jwksErr, &stop) {
				return stop.err
			}
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

	raw, err := security.ParseJWTWithJWKS(ctx, token, jwksURL, jwksCache)
	if err != nil {
		return nil, fmt.Errorf("jwks validation: %w", err)
	}

	// Extract standard OIDC claims.
	// "sub" is the primary identifier (RFC 7519). Fall back to
	// "preferred_username" then "name" for compatibility.
	sub, _ := raw["sub"].(string)
	if sub == "" {
		sub, _ = raw["preferred_username"].(string)
	}
	if sub == "" {
		sub, _ = raw["name"].(string)
	}
	if sub == "" {
		return nil, errors.New("token missing sub/preferred_username/name claim")
	}

	email, _ := raw["email"].(string)
	name, _ := raw["name"].(string)
	displayName, _ := raw["displayName"].(string)
	if name == "" {
		name = displayName
	}

	verified := decode(raw)

	// Which collection mirrors the token is the PLATFORM's decision — the
	// reserved admin org is a property of the process, not of any tenant — so it
	// is read off the platform's store here, before the request moves onto a
	// tenant's Base and that store becomes the tenant's.
	collectionName := core.CollectionNameSuperusers
	if !verified.PlatformSudo() {
		collectionName = "users"
		if v, _ := e.App.Store().Get(StoreKeyAuthUsersCollection).(string); v != "" {
			collectionName = v
		}
	}
	bases, _ := e.App.Store().Get(StoreKeyBases).(Bases)

	stated, _ := e.Get(requestEventKeyStatedOrg).(string)
	org, err := orgOf(verified, stated)
	if err != nil {
		return nil, refusal{err}
	}

	// Acting anywhere but home is either an explicit selection the membership
	// set admits or a platform operator reaching a tenant, which is the one
	// cross-tenant scope in the estate and the one worth a line in the log.
	if org != verified.Home() {
		e.App.Logger().Info("base: request acts outside its home org",
			"sub", sub, "home", verified.Home(), "org", org)
	}

	if bases != nil {
		b, err := bases(org)
		if err != nil {
			return nil, refusal{router.NewInternalServerError("Failed to open the Base for this organization.", err)}
		}
		e.App = b
	}

	// Publish the identity: on the request, for Base's own middleware, and back
	// onto the headers, for the proxies and shard routers that read them. They
	// were deleted on the way in, so what is here now came from the token.
	e.Set(RequestEventKeySub, sub)
	e.Set("authEmail", email)
	e.Set("authName", name)
	e.Set(RequestEventKeyOrgs, orgsOf(verified))
	e.Set(RequestEventKeyOrg, org)
	e.Set(RequestEventKeyOrgAdmin, verified.OrgAdmin(org))
	e.Request.Header.Set(claims.HeaderUserID, sub)
	e.Request.Header.Set(claims.HeaderOrgID, org)
	e.Request.Header.Set(headerUserEmail, email)

	// When IAM is active, Base does NOT store user records. IAM is the user store.
	// We create an ephemeral (unsaved) auth record from JWT claims so Base's
	// rule engine can evaluate it. No writes to _superusers or users collections.
	//
	// It is minted from the Base that is about to serve, so the collection the
	// rule engine resolves @request.auth against is the one it will query.
	//
	// A Base with no auth collection to mirror into refuses the request rather
	// than carrying on without one. The request is already pointed at that Base,
	// and continuing would serve a caller who holds a valid token as though they
	// held none — an anonymous read of a Base they are in fact a member of,
	// which is a strange enough answer to be worth an error instead.
	collection, err := e.App.FindCachedCollectionByNameOrId(collectionName)
	if err != nil {
		return nil, refusal{router.NewInternalServerError(
			"The Base for this organization has no auth collection.",
			fmt.Errorf("auth collection %q not found: %w", collectionName, err))}
	}

	// Ephemeral record — NOT persisted. IAM is the user store, not Base.
	// The record exists only for this request so Base's rule engine can evaluate it.
	record := core.NewRecord(collection)
	record.Id = subToRecordID(sub)
	record.Set("email", email)
	if name != "" {
		record.Set("name", name)
	}
	if collection.Fields.GetByName("org_id") != nil {
		record.Set("org_id", org)
	}
	record.SetVerified(true)

	return record, nil
}

// headerUserEmail travels beside the canonical three but is not one of them —
// tools/claims strips it and does not name it, because nothing authorizes on an
// address.
const headerUserEmail = "X-User-Email"

// StatedOrg is the org a request SAID it meant, read off X-Org-Id before that
// header was deleted.
//
// It is intent, not identity. Honor it only where the credential already
// carries the org, and refuse the request otherwise — a caller that names
// someone else's org and is handed its own reads the answer as that org's, and
// acts on it.
func StatedOrg(e *core.RequestEvent) string {
	v, _ := e.Get(requestEventKeyStatedOrg).(string)
	return v
}

// orgOf answers which org a verified token acts in.
//
// The org is the SUBJECT's own membership, home first, which is the only thing
// on a token that carries authority. Not the `owner` claim: IAM stamps that with
// the org of the APPLICATION a token was minted through, so reading it named the
// app's org for every caller — a token carrying alpha opened a Base for hanzo,
// and every tenant signing in through one application shared one file.
//
// stated is what the request said it meant, read off X-Org-Id. It selects among
// the orgs the token already carries and can add none, so on its own it grants
// nothing. It is honored at all so that naming an org the token does not carry
// can be answered with a refusal: served the caller's own org instead, an empty
// list reads as "this Base is empty", which is a different statement and a
// false one.
//
// A token with no membership set at all is a machine, and a machine is refused
// rather than handed the process's own Base. Its org could only come from the
// `owner` claim, and reading that claim — conditionally, on a predicate one
// reader will eventually drop — is the exact shape of the bug above. A machine
// that needs a Base reaches it with an IAM key, which carries a real membership.
func orgOf(c *authz.Claims, stated string) (string, error) {
	org, _ := c.EffectiveOrg(stated)

	if stated != "" && stated != org {
		return "", router.NewForbiddenError("The token does not carry the requested organization.", nil)
	}
	if org == "" {
		return "", router.NewForbiddenError("The token carries no organization.", nil)
	}

	return org, nil
}

// platformSudo reports whether verified claims carry platform authority — the
// one cross-tenant scope, and the only thing that mirrors onto _superusers.
//
// The question is asked, not restated: authz.Claims.PlatformSudo is the estate's
// published predicate, the same one the gateway mints X-User-IsAdmin from and
// cloud reads. Base holding a second definition is how platform authority comes
// to mean two things in two places, so it holds none — it decodes the verified
// claims through the type the issuer signs and lets that answer.
//
// Notably NOT the `owner` claim. IAM stamps `owner` with the organization of the
// APPLICATION a token was minted through, not the subject's own, so reading it
// makes platform authority a property of the app: everyone who signed in through
// an admin-org application would arrive holding it. The authority is membership
// of the reserved admin org, which only an existing platform admin can grant.
//
// An `admin` role on an ordinary org is a different, org-scoped authority and
// answers nothing here. _superusers is unscoped inside the process — schema,
// settings, backups and logs are process-wide, and one process serves many orgs'
// Bases — so honoring an org's own admin would hand one tenant the rest.
//
// Claims that do not decode carry no authority.
func decode(raw jwt.MapClaims) *authz.Claims {
	encoded, err := json.Marshal(raw)
	if err != nil {
		return &authz.Claims{}
	}

	var c authz.Claims
	if err := json.Unmarshal(encoded, &c); err != nil {
		return &authz.Claims{}
	}

	return &c
}

// orgsOf is the membership set the token asserts, as plain slugs.
//
// It travels on the request because the ephemeral auth record cannot carry it:
// a record has fields, and membership is not one of Base's. Without it, anything
// downstream that asks "which orgs is this caller in?" saw an empty set and
// refused every one of them.
func orgsOf(c *authz.Claims) []string {
	orgs := make([]string, 0, len(c.Orgs))
	for _, m := range c.Orgs {
		if m.Org != "" {
			orgs = append(orgs, m.Org)
		}
	}
	return orgs
}

// subToRecordID converts an OIDC sub claim (which may be a UUID, slug, or
// other identifier) into a valid Base record ID (exactly 15 lowercase
// alphanumeric chars).
//
// Short subs (< 15 chars) are padded with underscores (kept for backward compat).
// Long or non-alphanumeric subs are SHA-256 hashed and truncated to 24 hex chars
// (96 bits of entropy) to reduce collision risk.
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
	// SHA-256 the original sub and take the first 24 hex chars (96 bits).
	h := sha256.Sum256([]byte(sub))
	return hex.EncodeToString(h[:])[:24]
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
//
// Who may frame Base is NOT stated here. It is one sentence, and it lives in
// the admin's `frame-ancestors` (serve.go, BASE_FRAME_ANCESTORS): the admin is
// the only surface a frame can be pointed at to any effect, and X-Frame-Options
// has no allow-list form, so saying it here too would be a second, weaker copy
// of a policy that has to name specific hosts. Two statements of one rule drift.
func securityHeaders() *hook.Handler[*core.RequestEvent] {
	return &hook.Handler[*core.RequestEvent]{
		Id:       DefaultSecurityHeadersMiddlewareId,
		Priority: DefaultSecurityHeadersMiddlewarePriority,
		Func: func(e *core.RequestEvent) error {
			e.Response.Header().Set("X-XSS-Protection", "1; mode=block")
			e.Response.Header().Set("X-Content-Type-Options", "nosniff")

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

// logRequest writes one activity row for a served request.
//
// It writes to the Base the process serves from, never to the one the request
// landed on. Both the retention setting and the logger used to be read off
// e.App, which a credential naming an org has already moved to that tenant's
// Base — so an authenticated request logged itself into the tenant's file and
// the operator's log held only the traffic that never authenticated. The
// operator cannot read a tenant's file to reassemble it, and a tenant should
// not be holding the estate's audit trail either way.
func logRequest(event *core.RequestEvent, err error) {
	app := event.Deployment()

	// no logs retention
	if app.Settings().Logs.MaxDays == 0 {
		return
	}

	// the non-error route has explicitly disabled the activity logger
	if err == nil && event.Get(requestEventKeySkipSuccessActivityLog) != nil {
		return
	}

	attrs := make([]any, 0, 15)

	attrs = append(attrs, "type", "request")

	started := cast.ToTime(event.Get(requestEventKeyExecStart))
	if !started.IsZero() {
		attrs = append(attrs, "execTime", float64(time.Since(started))/float64(time.Millisecond))
	}

	if meta := event.Get(RequestEventKeyLogMeta); meta != nil {
		attrs = append(attrs, "meta", meta)
	}

	status := event.Status()
	method := cutStr(strings.ToUpper(event.Request.Method), 50)
	requestUri := cutStr(redactQuery(event.Request.URL), 3000)

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
				"error", errMsg,
				"details", apiErr.RawData(),
			)
		} else {
			attrs = append(attrs, "error", err.Error())
		}
	}

	attrs = append(
		attrs,
		"url", requestUri,
		"method", method,
		"status", status,
		"referer", cutStr(event.Request.Referer(), 2000),
		"userAgent", cutStr(event.Request.UserAgent(), 2000),
	)

	// Who acted. A JWT mints an auth record and the collection it lands in says
	// what kind of principal it is; an IAM key mints none, so a keyed request
	// used to be attributed to nobody — every action a service key took read as
	// anonymous, which is the opposite of what an audit trail is for. The
	// subject is the one thing both doors publish, so a subject with no record
	// is exactly a key.
	switch sub, _ := event.Get(RequestEventKeySub).(string); {
	case event.Auth != nil:
		attrs = append(attrs, "auth", event.Auth.Collection().Name)
		if app.Settings().Logs.LogAuthId {
			attrs = append(attrs, "authId", event.Auth.Id)
		}
	case sub != "":
		attrs = append(attrs, "auth", "key")
		if app.Settings().Logs.LogAuthId {
			attrs = append(attrs, "authId", sub)
		}
	default:
		attrs = append(attrs, "auth", "")
	}

	if app.Settings().Logs.LogIP {
		attrs = append(
			attrs,
			"userIP", event.RealIP(),
			"remoteIP", event.RemoteIP(),
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
			app.Logger().Error(message, attrs...)
		} else {
			app.Logger().Info(message, attrs...)
		}
	})
}

// credentialParams are the query parameters this deployment reads a credential
// out of: `key` is where an IAM key arrives on a request that carries no
// Authorization header, and `token` is the file and backup grant.
var credentialParams = []string{"key", "token"}

// redactQuery renders a request address for the log with any credential in the
// query replaced.
//
// A log row is kept for Logs.MaxDays, served by GET /v1/logs and copied into
// every backup, so a live secret key written into one is a live secret key in
// all three. The parameter NAMES stay — they are the shape of the call and the
// reason anyone reads the log — and only the values that are credentials go.
func redactQuery(u *url.URL) string {
	q := u.Query()

	redacted := false
	for _, name := range credentialParams {
		if q.Has(name) {
			q.Set(name, "redacted")
			redacted = true
		}
	}
	if !redacted {
		return u.RequestURI()
	}

	out := *u
	out.RawQuery = q.Encode()
	return out.RequestURI()
}

func cutStr(str string, max int) string {
	if len(str) > max {
		return str[:max] + "..."
	}
	return str
}
