package platform

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/hashicorp/golang-lru/v2/expirable"
	"golang.org/x/sync/singleflight"
)

// IAMUser represents an authenticated user from Hanzo IAM.
type IAMUser struct {
	ID     string   `json:"id"`
	Email  string   `json:"email"`
	Name   string   `json:"name"`
	OrgIDs []string `json:"orgIds"`
}

// Cache parameters. The 5-minute TTL matches the original map+TTL design;
// 10K is a safe default for the per-tenant fleet sizes Hanzo runs (each
// service has its own IAMClient instance and a single token is shared
// across many requests, so the working set stays well under cap).
const (
	tokenCacheTTL     = 5 * time.Minute
	defaultCacheSize  = 10_000
)

// ValidateIAMToken validates a bearer token against the IAM userinfo endpoint
// at config.IAMEndpoint/v1/iam/oauth/userinfo.
//
// This is a convenience function that creates a one-off HTTP request. For
// production use with caching, use the IAMClient returned by NewIAMClient.
func ValidateIAMToken(token string, config PlatformConfig) (*IAMUser, error) {
	endpoint := config.IAMEndpoint
	if endpoint == "" {
		endpoint = "https://hanzo.id"
	}

	req, err := http.NewRequest("GET", endpoint+"/v1/iam/oauth/userinfo", nil)
	if err != nil {
		return nil, fmt.Errorf("iam: create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("iam: request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("iam: userinfo returned %d: %s", resp.StatusCode, string(body))
	}

	var user IAMUser
	if err := json.NewDecoder(resp.Body).Decode(&user); err != nil {
		return nil, fmt.Errorf("iam: decode userinfo: %w", err)
	}
	if user.ID == "" {
		return nil, fmt.Errorf("iam: userinfo response missing user id")
	}

	return &user, nil
}

// ExchangeOAuth2Token exchanges an authorization code for tokens using the
// IAM OAuth2 token endpoint.
func ExchangeOAuth2Token(code, redirectURI string, config PlatformConfig) (accessToken, refreshToken string, err error) {
	endpoint := config.IAMEndpoint
	if endpoint == "" {
		endpoint = "https://hanzo.id"
	}

	data := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {redirectURI},
		"client_id":     {config.IAMClientID},
		"client_secret": {config.IAMClientSecret},
	}

	resp, err := http.PostForm(endpoint+"/v1/iam/oauth/token", data)
	if err != nil {
		return "", "", fmt.Errorf("iam: token exchange request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return "", "", fmt.Errorf("iam: token exchange returned %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", "", fmt.Errorf("iam: decode token response: %w", err)
	}

	return result.AccessToken, result.RefreshToken, nil
}

// --------------------------------------------------------------------------
// IAMClient with caching
// --------------------------------------------------------------------------

// IAMClient handles authentication against Hanzo IAM with token caching.
//
// Cache: TTL-expirable LRU (hashicorp/golang-lru/v2) shared between
// ValidateToken (JWT bearer tokens) and ResolveAPIKey (pk-/sk-/hk- API
// keys). Entries expire after tokenCacheTTL; LRU evicts the oldest entry
// when the cache is full. No O(n) eviction scans.
//
// Singleflight: golang.org/x/sync/singleflight coalesces concurrent
// validation requests for the same token into a single upstream IAM
// call. Under load (N goroutines validating the same JWT simultaneously),
// only one HTTP request hits IAM; the remaining N-1 wait and reuse the
// result.
type IAMClient struct {
	baseURL    string
	httpClient *http.Client

	cache *expirable.LRU[string, *IAMUser]
	sf    singleflight.Group

	mu    sync.RWMutex
	admin AdminCreds
}

// NewIAMClient creates a new IAM client pointed at the given base URL with
// the default cache capacity (10,000 entries).
func NewIAMClient(baseURL string) *IAMClient {
	return NewIAMClientWithCache(baseURL, defaultCacheSize)
}

// NewIAMClientWithCache creates a new IAM client with a custom cache capacity.
// cacheSize must be > 0; values <= 0 fall back to defaultCacheSize.
func NewIAMClientWithCache(baseURL string, cacheSize int) *IAMClient {
	if baseURL == "" {
		baseURL = "https://hanzo.id"
	}
	// Trim trailing slash.
	baseURL = strings.TrimRight(baseURL, "/")
	if cacheSize <= 0 {
		cacheSize = defaultCacheSize
	}

	return &IAMClient{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
		// Single shared cache for JWTs and API keys. Cache key namespacing
		// is unnecessary: JWT bearer tokens and pk-/sk-/hk- prefixed API
		// keys live in disjoint string spaces, so collisions are impossible.
		cache: expirable.NewLRU[string, *IAMUser](cacheSize, nil, tokenCacheTTL),
	}
}

// ValidateToken validates a Bearer token against IAM userinfo. Results are
// cached for tokenCacheTTL (5 minutes). Concurrent validations of the same
// token are coalesced into a single upstream call via singleflight.
func (c *IAMClient) ValidateToken(token string) (*IAMUser, error) {
	if user, ok := c.cache.Get(token); ok {
		return user, nil
	}
	// Singleflight key namespace: prefix "v:" so a token cannot collide with
	// the ResolveAPIKey namespace ("k:"). Without prefixes, a token whose
	// literal value happened to match an access key would share the same
	// inflight slot and one method's result could be returned by the other.
	v, err, _ := c.sf.Do("v:"+token, func() (any, error) {
		// Re-check cache after acquiring the singleflight slot: a concurrent
		// caller may have populated it while we were waiting.
		if user, ok := c.cache.Get(token); ok {
			return user, nil
		}
		user, err := c.fetchUserInfo(token)
		if err != nil {
			return nil, err
		}
		c.cache.Add(token, user)
		return user, nil
	})
	if err != nil {
		// Ensure no stale entry persists for this token after a failed fetch.
		c.cache.Remove(token)
		return nil, err
	}
	return v.(*IAMUser), nil
}

func (c *IAMClient) fetchUserInfo(token string) (*IAMUser, error) {
	req, err := http.NewRequest("GET", c.baseURL+"/v1/iam/oauth/userinfo", nil)
	if err != nil {
		return nil, fmt.Errorf("iam: create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("iam: request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("iam: userinfo returned %d: %s", resp.StatusCode, string(body))
	}

	var user IAMUser
	if err := json.NewDecoder(resp.Body).Decode(&user); err != nil {
		return nil, fmt.Errorf("iam: decode userinfo: %w", err)
	}
	if user.ID == "" {
		return nil, fmt.Errorf("iam: userinfo response missing user id")
	}

	return &user, nil
}

// InvalidateToken removes a token (or API key) from the cache. Safe to call
// for either JWT bearer tokens or pk-/sk-/hk- API keys — the cache is shared.
func (c *IAMClient) InvalidateToken(token string) {
	c.cache.Remove(token)
}

// ── API Key Resolution (pk-/sk-/hk- keys managed by IAM) ────────────────

// IAMKey represents an API key from IAM's Key table.
type IAMKey struct {
	Owner       string `json:"owner"`
	Name        string `json:"name"`
	Type        string `json:"type"`        // Organization, Application, User
	Org         string `json:"organization"`
	Application string `json:"application"`
	User        string `json:"user"`
	AccessKey   string `json:"accessKey"`
	State       string `json:"state"`
}

// Hanzo key prefix standard (always hyphen, never underscore):
//
//   pk-  publishable key  (frontend-safe, read-only API access)
//   sk-  secret key       (backend-only, full API access)
//   hk-  hanzo key        (IAM user API key, legacy)
//   hi-  hanzo insights   (analytics event ingestion)
//   ha-  hanzo analytics  (lightweight web analytics)
//   hz-  hanzo widget     (restricted chat/embed key)
//
// All managed by IAM. One key store. One prefix convention.

// IsPublishableKey returns true if the token has a publishable key prefix.
func IsPublishableKey(token string) bool {
	return strings.HasPrefix(token, "pk-")
}

// IsSecretKey returns true if the token has a secret key prefix.
func IsSecretKey(token string) bool {
	return strings.HasPrefix(token, "sk-")
}

// IsAPIKey returns true if the token is any type of IAM API key.
func IsAPIKey(token string) bool {
	return strings.HasPrefix(token, "hk-") ||
		strings.HasPrefix(token, "pk-") ||
		strings.HasPrefix(token, "sk-")
}

// IsAnalyticsKey returns true if the token is an insights or analytics key.
func IsAnalyticsKey(token string) bool {
	return strings.HasPrefix(token, "hi-") ||
		strings.HasPrefix(token, "ha-")
}

// IsWidgetKey returns true if the token is a widget embed key.
func IsWidgetKey(token string) bool {
	return strings.HasPrefix(token, "hz-")
}

// ResolveAPIKey resolves an IAM API key to user + org context via IAM's
// GET /v1/iam/resolve-user?accessKey= endpoint. Results are cached for
// tokenCacheTTL; concurrent resolves of the same key are coalesced via
// singleflight.
//
// That door resolves a SECRET key. A publishable pk- names an organization and
// never a person — which is what makes it safe to ship in a browser — so it
// resolves to nobody here and IAM answers it at its own door.
func (c *IAMClient) ResolveAPIKey(accessKey string) (*IAMUser, error) {
	if user, ok := c.cache.Get(accessKey); ok {
		return user, nil
	}
	v, err, _ := c.sf.Do("k:"+accessKey, func() (any, error) {
		// Re-check cache after acquiring the singleflight slot.
		if user, ok := c.cache.Get(accessKey); ok {
			return user, nil
		}
		user, err := c.fetchUserByKey(accessKey)
		if err != nil {
			return nil, err
		}
		c.cache.Add(accessKey, user)
		return user, nil
	})
	if err != nil {
		c.cache.Remove(accessKey)
		return nil, err
	}
	return v.(*IAMUser), nil
}

func (c *IAMClient) fetchUserByKey(accessKey string) (*IAMUser, error) {
	q := url.Values{}
	q.Set("accessKey", accessKey)

	req, err := c.s2sRequest(context.Background(), "GET", "/v1/iam/resolve-user", q, nil)
	if err != nil {
		return nil, fmt.Errorf("iam: resolve-user: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("iam: key request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("iam: resolve-user returned %d: %s", resp.StatusCode, string(body))
	}

	// A key resolver is told exactly what it needs to attribute the request and
	// nothing more: who the key belongs to, and which organization pays. There is
	// no opaque subject in this projection — deliberately, so resolving one
	// credential can never disclose the holder's other credentials.
	var result struct {
		Data struct {
			Owner string `json:"owner"`
			Name  string `json:"name"`
			Email string `json:"email"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("iam: decode key user: %w", err)
	}

	if result.Data.Name == "" {
		return nil, fmt.Errorf("iam: key resolved to empty user")
	}

	return &IAMUser{
		Name:   result.Data.Name,
		Email:  result.Data.Email,
		OrgIDs: []string{result.Data.Owner},
	}, nil
}

// ── Server-to-Server User Operations ────────────────────────────────────
//
// These methods authenticate using clientId + clientSecret (IAM application
// credentials) and bypass session auth. They're for service-to-service flows
// where a downstream service needs to look up or provision IAM users
// (onboarding, KYC reconciliation, deduplication).
//
// The clientId/clientSecret used here are the *service*'s own IAM application
// credentials (e.g., the BD service's IAM client). They authorize reads against
// the configured org. They do NOT grant superuser scope.

// AdminCreds holds the service's IAM application credentials. Pass these to
// the client via SetAdminCreds before invoking server-to-server methods.
type AdminCreds struct {
	ClientID     string
	ClientSecret string
	Owner        string // default org for lookups when caller doesn't specify
}

// SetAdminCreds installs the service-level credentials used by LookupByAttribute,
// EnsureUser, and other server-to-server methods. Safe to call once at startup.
func (c *IAMClient) SetAdminCreds(creds AdminCreds) {
	c.mu.Lock()
	c.admin = creds
	c.mu.Unlock()
}

// EnsureUserSpec describes a user to provision idempotently via EnsureUser.
type EnsureUserSpec struct {
	Owner       string // org slug (defaults to client's admin Owner if empty)
	Email       string // primary lookup key for existing users
	Name        string // username; auto-generated by IAM if empty
	DisplayName string
	Phone       string
	Type        string // IAM user type, e.g. "normal-user"
}

// adminCreds returns a snapshot of the configured admin credentials.
func (c *IAMClient) adminCreds() AdminCreds {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.admin
}

// s2sRequest builds a server-to-server request carrying this service's own IAM
// application credentials as client_secret_basic (RFC 6749 §2.3.1) — the one
// client-credential transport IAM reads. The credentials ride in the header, so
// a secret never reaches a URL, an access log or a referrer.
func (c *IAMClient) s2sRequest(ctx context.Context, method, path string, q url.Values, body []byte) (*http.Request, error) {
	creds := c.adminCreds()
	if creds.ClientID == "" || creds.ClientSecret == "" {
		return nil, fmt.Errorf("admin credentials not configured (call SetAdminCreds)")
	}
	u := c.baseURL + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	var payload io.Reader
	if body != nil {
		payload = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, u, payload)
	if err != nil {
		return nil, err
	}
	req.SetBasicAuth(creds.ClientID, creds.ClientSecret)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return req, nil
}

// decodeUser reads an IAM user record off the wire. The user routes answer with
// the record itself — masked, no envelope — so the body IS the user.
func decodeUser(body []byte, caller string) (*IAMUser, error) {
	var u struct {
		ID    string `json:"id"`
		Name  string `json:"name"`
		Email string `json:"email"`
		Owner string `json:"owner"`
	}
	if err := json.Unmarshal(body, &u); err != nil {
		return nil, fmt.Errorf("iam: %s decode: %w", caller, err)
	}
	if u.Name == "" {
		return nil, fmt.Errorf("iam: %s: response carries no user", caller)
	}
	return &IAMUser{ID: u.ID, Name: u.Name, Email: u.Email, OrgIDs: []string{u.Owner}}, nil
}

// getUser reads one user through GET /v1/iam/users/get, which addresses a person
// within an organization by username or by email address.
//
// A miss is (nil, nil): "no such user" is an answer, not a failure. One address
// naming two accounts is IAM's 409 and reaches the caller as an error, because
// picking one of the two is how somebody gets resolved as a colleague.
func (c *IAMClient) getUser(ctx context.Context, caller string, q url.Values) (*IAMUser, error) {
	req, err := c.s2sRequest(ctx, "GET", "/v1/iam/users/get", q, nil)
	if err != nil {
		return nil, fmt.Errorf("iam: %s: %w", caller, err)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("iam: %s request: %w", caller, err)
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, nil
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("iam: %s returned %d: %s", caller, resp.StatusCode, truncate(string(body), 256))
	}
	return decodeUser(body, caller)
}

// LookupByAttribute resolves the user that attr=value names within org. attr is
// the handle IAM reads a person by: "email" (their address) or "name" (their
// username). org defaults to the client's admin Owner if empty.
//
// An address names at most one account, so the result holds zero or one user;
// maxResults is accepted for call compatibility and bounds nothing. Two accounts
// sharing one address is an error rather than an arbitrary pick — that choice is
// IAM's, and it is how somebody gets resolved as a colleague.
//
// Returns ([], nil) when no user matches — never an error for empty results.
// Errors are returned only for transport / decoding / IAM-side error responses.
//
// Any other attribute is REFUSED. IAM reads a user by username or by address and
// offers no general attribute search, so a phone number (or any other field) has
// no lookup to stand on, and answering one by scanning the org's roster would be
// a guess wearing a lookup's clothes.
func (c *IAMClient) LookupByAttribute(ctx context.Context, attr, value, org string, maxResults int) ([]IAMUser, error) {
	q := url.Values{}
	switch attr {
	case "":
		return nil, fmt.Errorf("iam: LookupByAttribute: attr is required")
	case "email", "name":
		q.Set(attr, value)
	default:
		return nil, fmt.Errorf("iam: LookupByAttribute: IAM reads a user by %q or %q; %q has no lookup", "email", "name", attr)
	}
	if value == "" {
		return nil, fmt.Errorf("iam: LookupByAttribute: value is required")
	}
	if org == "" {
		org = c.adminCreds().Owner
	}
	if org == "" {
		return nil, fmt.Errorf("iam: LookupByAttribute: org is required (no default Owner configured)")
	}
	q.Set("owner", org)

	user, err := c.getUser(ctx, "LookupByAttribute", q)
	if err != nil || user == nil {
		return nil, err
	}
	return []IAMUser{*user}, nil
}

// EnsureUser idempotently provisions an IAM user matching spec. If the user
// already exists (matched by email within spec.Owner), the existing user is
// returned without modification. Otherwise the user is created via
// POST /v1/iam/users and the created record is returned.
//
// A name already taken is IAM's HTTP 409, and that is this call's
// idempotent-replay signal: the account is already there, so resolve it by
// address and hand it back.
//
// spec.Email is required (used as the dedup key). spec.Owner defaults to the
// client's admin Owner if empty.
func (c *IAMClient) EnsureUser(ctx context.Context, spec EnsureUserSpec) (*IAMUser, error) {
	if spec.Email == "" {
		return nil, fmt.Errorf("iam: EnsureUser: email is required")
	}
	owner := spec.Owner
	if owner == "" {
		owner = c.adminCreds().Owner
	}
	if owner == "" {
		return nil, fmt.Errorf("iam: EnsureUser: owner is required (no default Owner configured)")
	}
	name := spec.Name
	if name == "" {
		// Fall back to local-part of email; IAM regenerates on collision.
		if i := strings.Index(spec.Email, "@"); i > 0 {
			name = spec.Email[:i]
		}
	}

	// The profile nests under `user`, beside a password field this call never
	// sets — the create takes a plaintext password as its own input precisely so
	// that a credential is never a property of the record. An account provisioned
	// here therefore has no password until one is established through IAM.
	body, err := json.Marshal(map[string]any{
		"user": map[string]any{
			"owner":       owner,
			"name":        name,
			"email":       spec.Email,
			"displayName": spec.DisplayName,
			"phone":       spec.Phone,
			"type":        spec.Type,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("iam: EnsureUser marshal: %w", err)
	}

	req, err := c.s2sRequest(ctx, "POST", "/v1/iam/users", nil, body)
	if err != nil {
		return nil, fmt.Errorf("iam: EnsureUser: %w", err)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("iam: EnsureUser request: %w", err)
	}
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	resp.Body.Close()

	// Already there — resolve the existing account by address and return it.
	if resp.StatusCode == http.StatusConflict {
		return c.fetchUserByEmail(ctx, owner, spec.Email)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("iam: EnsureUser returned %d: %s",
			resp.StatusCode, truncate(string(respBody), 256))
	}
	// The create answers with the record it wrote, carrying the id IAM minted,
	// so there is nothing further to fetch.
	return decodeUser(respBody, "EnsureUser")
}

// fetchUserByEmail resolves the user holding email within the given org. Used by
// EnsureUser to resolve an account that already existed.
//
// It is the same read as LookupByAttribute over the same route; the two differ
// only in what a miss MEANS. Here the caller has just been told the account
// exists, so not finding it is a contradiction and an error.
func (c *IAMClient) fetchUserByEmail(ctx context.Context, owner, email string) (*IAMUser, error) {
	q := url.Values{}
	q.Set("owner", owner)
	q.Set("email", email)

	user, err := c.getUser(ctx, "fetchUserByEmail", q)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, fmt.Errorf("iam: fetchUserByEmail: no user found for email=%s in owner=%s", email, owner)
	}
	return user, nil
}

// truncate clips s to at most n runes, appending "…" if truncated. Used for
// safe inclusion of IAM error bodies in returned error messages.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
