package org

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
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

// Cache parameters. 10K is a safe default for the per-base fleet sizes Hanzo
// runs (each service has its own IAMClient instance and a single token is
// shared across many requests, so the working set stays well under cap).
//
// A token and a key are cached for different lengths because they die
// differently. A token carries its own expiry and IAM has already refused it by
// the time one matters, so holding the validation for five minutes costs
// nothing. A key has no expiry at all — revoking it at IAM is the only way to
// end it — so every second it is cached is a second a revoked key still opens
// the org. Singleflight already collapses a burst into one upstream call, so
// the short window buys promptness rather than costing load.
const (
	tokenCacheTTL    = 5 * time.Minute
	keyCacheTTL      = 30 * time.Second
	defaultCacheSize = 10_000
)

// ValidateIAMToken validates a bearer token against the IAM userinfo endpoint
// at config.IAMEndpoint/v1/iam/oauth/userinfo.
//
// This is a convenience function that creates a one-off HTTP request. For
// production use with caching, use the IAMClient returned by NewIAMClient.
func ValidateIAMToken(token string, config Config) (*IAMUser, error) {
	endpoint, client := resolveIAM(config.IAMEndpoint, 10*time.Second)

	req, err := http.NewRequest("GET", endpoint+"/v1/iam/oauth/userinfo", nil)
	if err != nil {
		return nil, fmt.Errorf("iam: create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := client.Do(req)
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
func ExchangeOAuth2Token(code, redirectURI string, config Config) (accessToken, refreshToken string, err error) {
	// Minting addresses the BRAND, so this one stays on the brand's public
	// origin and does not take the in-cluster wire.
	//
	// IAM resolves the issuer per brand from the request Host (internal/oidc:
	// resolveIssuer(c.Host())) because a relying party that discovered through
	// lux.id pins `iss` to lux.id and rejects a hanzo.id-issued token. The ZAP
	// request frame carries no Host — zap-proto/http encodes method, target,
	// proto, headers and body, and Host is skipped from the headers as
	// frame-owned without a frame slot to own it — so a token minted over that
	// wire would carry the default brand's issuer whatever host was asked for.
	// Reads are unaffected: nothing below reads `iss`.
	endpoint := strings.TrimRight(strings.TrimSpace(config.IAMEndpoint), "/")
	if endpoint == "" {
		endpoint = "https://hanzo.id"
	}
	if low := strings.ToLower(endpoint); !strings.HasPrefix(low, "http://") && !strings.HasPrefix(low, "https://") {
		return "", "", fmt.Errorf("iam: token exchange needs the brand's public origin, "+
			"not the in-cluster address %q: the issuer is derived from the request host, "+
			"so a token minted through the cluster address carries the wrong brand", config.IAMEndpoint)
	}
	client := &http.Client{Timeout: 10 * time.Second}

	data := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {redirectURI},
		"client_id":     {config.IAMClientID},
		"client_secret": {config.IAMClientSecret},
	}

	resp, err := client.PostForm(endpoint+"/v1/iam/oauth/token", data)
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
// Cache: a TTL-expirable LRU (hashicorp/golang-lru/v2) per credential kind —
// ValidateToken reads tokens, ResolveAPIKey reads keys — because the two expire
// on different clocks. LRU evicts the oldest entry when a cache is full. No
// O(n) eviction scans.
//
// Singleflight: golang.org/x/sync/singleflight coalesces concurrent
// validation requests for the same token into a single upstream IAM
// call. Under load (N goroutines validating the same JWT simultaneously),
// only one HTTP request hits IAM; the remaining N-1 wait and reuse the
// result.
type IAMClient struct {
	baseURL    string
	httpClient *http.Client

	tokens *expirable.LRU[string, *IAMUser]
	keys   *expirable.LRU[string, *IAMUser]
	sf     singleflight.Group

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
	if cacheSize <= 0 {
		cacheSize = defaultCacheSize
	}

	// The endpoint's scheme names the wire (see iam_transport.go): https:// is
	// net/http as before, a bare host:port or zap:// is ZAP. Every method below
	// is written against net/http and is unchanged by the choice.
	baseURL, httpClient := resolveIAM(baseURL, 10*time.Second)

	return &IAMClient{
		baseURL:    baseURL,
		httpClient: httpClient,
		tokens:     expirable.NewLRU[string, *IAMUser](cacheSize, nil, tokenCacheTTL),
		keys:       expirable.NewLRU[string, *IAMUser](cacheSize, nil, keyCacheTTL),
	}
}

// ValidateToken validates a Bearer token against IAM userinfo. Results are
// cached for tokenCacheTTL (5 minutes). Concurrent validations of the same
// token are coalesced into a single upstream call via singleflight.
func (c *IAMClient) ValidateToken(token string) (*IAMUser, error) {
	if user, ok := c.tokens.Get(token); ok {
		return user, nil
	}
	// Singleflight key namespace: prefix "v:" so a token cannot collide with
	// the ResolveAPIKey namespace ("k:"). Without prefixes, a token whose
	// literal value happened to match an access key would share the same
	// inflight slot and one method's result could be returned by the other.
	v, err, _ := c.sf.Do("v:"+token, func() (any, error) {
		// Re-check cache after acquiring the singleflight slot: a concurrent
		// caller may have populated it while we were waiting.
		if user, ok := c.tokens.Get(token); ok {
			return user, nil
		}
		user, err := c.fetchUserInfo(token)
		if err != nil {
			return nil, err
		}
		c.tokens.Add(token, user)
		return user, nil
	})
	if err != nil {
		// Ensure no stale entry persists for this token after a failed fetch.
		c.tokens.Remove(token)
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

// InvalidateToken drops a credential from the cache. Safe to call for either
// JWT bearer tokens or pk-/sk-/hk- API keys — a value is only ever in the one
// cache that holds its kind, and removing what is not there is nothing.
func (c *IAMClient) InvalidateToken(token string) {
	c.tokens.Remove(token)
	c.keys.Remove(token)
}

// ── API Key Resolution (pk-/sk-/hk- keys managed by IAM) ────────────────

// IAMKey represents an API key from IAM's Key table.
type IAMKey struct {
	Owner       string `json:"owner"`
	Name        string `json:"name"`
	Type        string `json:"type"` // Organization, Application, User
	Org         string `json:"organization"`
	Application string `json:"application"`
	User        string `json:"user"`
	AccessKey   string `json:"accessKey"`
	State       string `json:"state"`
}

// The key prefixes this process resolves (always hyphen, never underscore):
//
//   pk-  publishable key  (printed in a web page)
//   sk-  secret key       (the org's own server)
//   hk-  hanzo key        (a person's IAM key)
//
// IAM mints them all and holds them in one store. A prefix a door here does not
// resolve reaches exactly what an anonymous caller reaches, so classifying one
// is a question with no consumer and no answer.

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

// ResolveAPIKey resolves an IAM API key (hk-/pk-/sk-) to user + org context.
// Uses IAM's GET /v1/iam/keys/principal?accessKey= endpoint. Results are cached
// for tokenCacheTTL; concurrent resolves of the same key are coalesced via
// singleflight.
func (c *IAMClient) ResolveAPIKey(accessKey string) (*IAMUser, error) {
	if user, ok := c.keys.Get(accessKey); ok {
		return user, nil
	}
	v, err, _ := c.sf.Do("k:"+accessKey, func() (any, error) {
		// Re-check cache after acquiring the singleflight slot.
		if user, ok := c.keys.Get(accessKey); ok {
			return user, nil
		}
		user, err := c.fetchUserByKey(accessKey)
		if err != nil {
			return nil, err
		}
		c.keys.Add(accessKey, user)
		return user, nil
	})
	if err != nil {
		c.keys.Remove(accessKey)
		return nil, err
	}
	return v.(*IAMUser), nil
}

func (c *IAMClient) fetchUserByKey(accessKey string) (*IAMUser, error) {
	creds := c.adminCreds()
	if creds.ClientID == "" || creds.ClientSecret == "" {
		return nil, fmt.Errorf("iam: ResolveAPIKey: admin credentials not configured (call SetAdminCreds)")
	}
	u := c.baseURL + "/v1/iam/keys/principal?accessKey=" + url.QueryEscape(accessKey)

	req, err := http.NewRequest("GET", u, nil)
	if err != nil {
		return nil, fmt.Errorf("iam: create key request: %w", err)
	}
	req.SetBasicAuth(creds.ClientID, creds.ClientSecret)
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("iam: key request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("iam: keys/principal returned %d: %s", resp.StatusCode, string(body))
	}

	var envelope struct {
		Status string `json:"status"`
		Msg    string `json:"msg"`
		Data   *struct {
			Owner string `json:"owner"`
			Name  string `json:"name"`
			Email string `json:"email"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return nil, fmt.Errorf("iam: decode key principal: %w", err)
	}
	if envelope.Status != "ok" || envelope.Data == nil {
		return nil, fmt.Errorf("iam: key did not resolve: %s", envelope.Msg)
	}
	if envelope.Data.Owner == "" || envelope.Data.Name == "" {
		return nil, fmt.Errorf("iam: key resolved to empty user")
	}

	// The key endpoint answers with the account, never its opaque id: what it
	// discloses is deliberately the narrowest projection a resolver needs. So the
	// subject is the account's own canonical spelling, owner/name, which is unique
	// across tenants where a bare username is unique only inside one.
	return &IAMUser{
		ID:     envelope.Data.Owner + "/" + envelope.Data.Name,
		Name:   envelope.Data.Name,
		Email:  envelope.Data.Email,
		OrgIDs: []string{envelope.Data.Owner},
	}, nil
}

// ── Server-to-Server User Operations ────────────────────────────────────
//
// These methods present the calling service's OWN IAM application credentials
// as client_secret_basic, and carry no session. They are for the flows where a
// service looks up or provisions IAM users (onboarding, KYC reconciliation,
// deduplication).
//
// What those credentials reach is the application's capability allowlist and the
// tenant it serves — never platform scope, and never another tenant's rows. IAM
// refuses an owner the credential is not scoped to rather than answering with
// its own, so a lookup either reads the org it named or fails.

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

// LookupByAttribute performs a server-to-server lookup of users matching
// attr=value within org. org defaults to the client's admin Owner if empty.
// maxResults caps the page size; values <= 0 default to 10.
//
// IAM's user collection narrows on ONE attribute, the email address, so "email"
// is the only attr this answers. Any other name is refused rather than sent: a
// filter IAM does not offer is a filter it ignores, the reply is then the org's
// first page, and a caller reading that as a match binds the wrong person.
//
// Returns ([], nil) when no user matches — never an error for empty results.
// Errors are returned only for transport / decoding / IAM-side error responses.
func (c *IAMClient) LookupByAttribute(ctx context.Context, attr, value, org string, maxResults int) ([]IAMUser, error) {
	if attr != "email" {
		return nil, fmt.Errorf("iam: LookupByAttribute: no %q filter; the user collection narrows on email", attr)
	}
	if value == "" {
		return nil, fmt.Errorf("iam: LookupByAttribute: value is required")
	}
	creds := c.adminCreds()
	if creds.ClientID == "" || creds.ClientSecret == "" {
		return nil, fmt.Errorf("iam: LookupByAttribute: admin credentials not configured (call SetAdminCreds)")
	}
	if org == "" {
		org = creds.Owner
	}
	if org == "" {
		return nil, fmt.Errorf("iam: LookupByAttribute: org is required (no default Owner configured)")
	}
	if maxResults <= 0 {
		maxResults = 10
	}

	users, err := c.listUsers(ctx, org, value, maxResults)
	if err != nil {
		return nil, fmt.Errorf("iam: LookupByAttribute: %w", err)
	}
	return users, nil
}

// listUsers reads the page of users in owner carrying email. It is the ONE place
// this client addresses the user collection, so a lookup and a provision cannot
// come to disagree about where that collection is or what it answers with.
//
// A page is the record list itself — no envelope — and an owner that holds no
// match is an empty list, not an error.
func (c *IAMClient) listUsers(ctx context.Context, owner, email string, limit int) ([]IAMUser, error) {
	creds := c.adminCreds()
	q := url.Values{}
	q.Set("owner", owner)
	q.Set("email", email)
	if limit > 0 {
		q.Set("limit", strconv.Itoa(limit))
	}

	req, err := http.NewRequestWithContext(ctx, "GET", c.baseURL+"/v1/iam/users?"+q.Encode(), nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.SetBasicAuth(creds.ClientID, creds.ClientSecret)
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request: %w", err)
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, nil
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("users returned %d: %s", resp.StatusCode, truncate(string(body), 256))
	}

	var page struct {
		Users []struct {
			ID    string `json:"id"`
			Name  string `json:"name"`
			Email string `json:"email"`
			Owner string `json:"owner"`
		} `json:"users"`
	}
	if err := json.Unmarshal(body, &page); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}

	out := make([]IAMUser, 0, len(page.Users))
	for _, u := range page.Users {
		if u.ID == "" {
			continue
		}
		out = append(out, IAMUser{ID: u.ID, Name: u.Name, Email: u.Email, OrgIDs: []string{u.Owner}})
	}
	return out, nil
}

// EnsureUser idempotently provisions an IAM user matching spec. If the user
// already exists (matched by email within spec.Owner), the existing user is
// returned without modification. Otherwise the user is created via
// POST /v1/iam/users and the created record is returned.
//
// The ADDRESS decides, so the read comes first. IAM's create refuses a duplicate
// username and admits a duplicate address, and a second row under one address is
// a row no lookup by address will resolve again — it makes both unreachable.
// Creating first and treating the refusal as "already there" would be that write.
//
// spec.Email is required (used as the dedup key). spec.Owner defaults to the
// client's admin Owner if empty.
func (c *IAMClient) EnsureUser(ctx context.Context, spec EnsureUserSpec) (*IAMUser, error) {
	if spec.Email == "" {
		return nil, fmt.Errorf("iam: EnsureUser: email is required")
	}
	creds := c.adminCreds()
	if creds.ClientID == "" || creds.ClientSecret == "" {
		return nil, fmt.Errorf("iam: EnsureUser: admin credentials not configured (call SetAdminCreds)")
	}
	owner := spec.Owner
	if owner == "" {
		owner = creds.Owner
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

	existing, err := c.listUsers(ctx, owner, spec.Email, 2)
	if err != nil {
		return nil, fmt.Errorf("iam: EnsureUser: %w", err)
	}
	if len(existing) > 1 {
		return nil, fmt.Errorf("iam: EnsureUser: email=%s names %d users in owner=%s", spec.Email, len(existing), owner)
	}
	if len(existing) == 1 {
		return &existing[0], nil
	}

	// The profile rides under "user": the create body carries the record beside
	// the write-only fields that never land on it, so a flat body reads as a user
	// with nothing in it.
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

	req, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/v1/iam/users", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("iam: EnsureUser build request: %w", err)
	}
	req.SetBasicAuth(creds.ClientID, creds.ClientSecret)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("iam: EnsureUser request: %w", err)
	}
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	resp.Body.Close()

	// The username was taken between the read above and this write — the racing
	// provision that lost. Both agree on the address, so resolve by it.
	if resp.StatusCode == http.StatusConflict {
		return c.fetchUserByEmail(ctx, owner, spec.Email)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, fmt.Errorf("iam: EnsureUser returned %d: %s",
			resp.StatusCode, truncate(string(respBody), 256))
	}

	var created struct {
		ID    string `json:"id"`
		Name  string `json:"name"`
		Email string `json:"email"`
		Owner string `json:"owner"`
	}
	if err := json.Unmarshal(respBody, &created); err != nil {
		return nil, fmt.Errorf("iam: EnsureUser decode: %w", err)
	}
	if created.ID == "" {
		return nil, fmt.Errorf("iam: EnsureUser: created user carries no id")
	}
	return &IAMUser{
		ID:     created.ID,
		Name:   created.Name,
		Email:  created.Email,
		OrgIDs: []string{created.Owner},
	}, nil
}

// fetchUserByEmail resolves the ONE user carrying email within owner. Used by
// EnsureUser to resolve a user a racing provision created first.
//
// An address naming two accounts names none: two rows refuse rather than hand
// back the first, because choosing between them is how somebody ends up acting
// under a colleague's identity.
func (c *IAMClient) fetchUserByEmail(ctx context.Context, owner, email string) (*IAMUser, error) {
	users, err := c.listUsers(ctx, owner, email, 2)
	if err != nil {
		return nil, fmt.Errorf("iam: fetchUserByEmail: %w", err)
	}
	switch len(users) {
	case 0:
		return nil, fmt.Errorf("iam: fetchUserByEmail: no user found for email=%s in owner=%s", email, owner)
	case 1:
		return &users[0], nil
	default:
		return nil, fmt.Errorf("iam: fetchUserByEmail: email=%s names %d users in owner=%s", email, len(users), owner)
	}
}

// truncate clips s to at most n runes, appending "…" if truncated. Used for
// safe inclusion of IAM error bodies in returned error messages.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
