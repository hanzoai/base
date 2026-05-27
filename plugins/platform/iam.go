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
)

// IAMUser represents an authenticated user from Hanzo IAM.
type IAMUser struct {
	ID     string   `json:"id"`
	Email  string   `json:"email"`
	Name   string   `json:"name"`
	OrgIDs []string `json:"orgIds"`
}

// tokenCacheEntry holds a cached IAM validation result.
type tokenCacheEntry struct {
	user    *IAMUser
	expires time.Time
}

const tokenCacheTTL = 5 * time.Minute

// ValidateIAMToken validates a bearer token against the IAM userinfo endpoint
// at config.IAMEndpoint/api/userinfo.
//
// This is a convenience function that creates a one-off HTTP request. For
// production use with caching, use the IAMClient returned by NewIAMClient.
func ValidateIAMToken(token string, config PlatformConfig) (*IAMUser, error) {
	endpoint := config.IAMEndpoint
	if endpoint == "" {
		endpoint = "https://hanzo.id"
	}

	req, err := http.NewRequest("GET", endpoint+"/api/userinfo", nil)
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

	resp, err := http.PostForm(endpoint+"/oauth/token", data)
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
type IAMClient struct {
	baseURL    string
	httpClient *http.Client

	mu    sync.RWMutex
	cache map[string]*tokenCacheEntry
	admin AdminCreds
}

// NewIAMClient creates a new IAM client pointed at the given base URL.
func NewIAMClient(baseURL string) *IAMClient {
	if baseURL == "" {
		baseURL = "https://hanzo.id"
	}
	// Trim trailing slash.
	baseURL = strings.TrimRight(baseURL, "/")

	return &IAMClient{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
		cache: make(map[string]*tokenCacheEntry),
	}
}

// ValidateToken validates a Bearer token against IAM userinfo. Results are
// cached for 5 minutes.
func (c *IAMClient) ValidateToken(token string) (*IAMUser, error) {
	c.mu.RLock()
	entry, ok := c.cache[token]
	c.mu.RUnlock()

	if ok && time.Now().Before(entry.expires) {
		return entry.user, nil
	}

	user, err := c.fetchUserInfo(token)
	if err != nil {
		c.mu.Lock()
		delete(c.cache, token)
		c.mu.Unlock()
		return nil, err
	}

	c.mu.Lock()
	c.cache[token] = &tokenCacheEntry{
		user:    user,
		expires: time.Now().Add(tokenCacheTTL),
	}
	if len(c.cache) > 1000 {
		c.evictExpiredLocked()
	}
	c.mu.Unlock()

	return user, nil
}

func (c *IAMClient) fetchUserInfo(token string) (*IAMUser, error) {
	req, err := http.NewRequest("GET", c.baseURL+"/api/userinfo", nil)
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

func (c *IAMClient) evictExpiredLocked() {
	now := time.Now()
	for k, v := range c.cache {
		if now.After(v.expires) {
			delete(c.cache, k)
		}
	}
}

// InvalidateToken removes a token from the cache.
func (c *IAMClient) InvalidateToken(token string) {
	c.mu.Lock()
	delete(c.cache, token)
	c.mu.Unlock()
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

// ResolveAPIKey resolves an IAM API key (hk-/pk-/sk-) to user + org context.
// Uses IAM's GET /api/get-user?accessKey= endpoint. Results cached 5 minutes.
func (c *IAMClient) ResolveAPIKey(accessKey string) (*IAMUser, error) {
	// Check cache (same cache as JWT tokens)
	c.mu.RLock()
	entry, ok := c.cache[accessKey]
	c.mu.RUnlock()

	if ok && time.Now().Before(entry.expires) {
		return entry.user, nil
	}

	user, err := c.fetchUserByKey(accessKey)
	if err != nil {
		c.mu.Lock()
		delete(c.cache, accessKey)
		c.mu.Unlock()
		return nil, err
	}

	c.mu.Lock()
	c.cache[accessKey] = &tokenCacheEntry{
		user:    user,
		expires: time.Now().Add(tokenCacheTTL),
	}
	if len(c.cache) > 1000 {
		c.evictExpiredLocked()
	}
	c.mu.Unlock()

	return user, nil
}

func (c *IAMClient) fetchUserByKey(accessKey string) (*IAMUser, error) {
	u := c.baseURL + "/api/get-user?accessKey=" + url.QueryEscape(accessKey)

	req, err := http.NewRequest("GET", u, nil)
	if err != nil {
		return nil, fmt.Errorf("iam: create key request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("iam: key request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("iam: get-user returned %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Data struct {
			Owner string `json:"owner"`
			Name  string `json:"name"`
			Email string `json:"email"`
			ID    string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("iam: decode key user: %w", err)
	}

	if result.Data.Name == "" {
		return nil, fmt.Errorf("iam: key resolved to empty user")
	}

	return &IAMUser{
		ID:     result.Data.ID,
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

// normalizePhoneCandidates returns the set of phone shapes IAM might have
// stored. IAM/Casdoor inconsistently persists phones — some rows include the
// leading "+", some are raw digits, US numbers sometimes omit the +1 country
// code. We probe all three shapes to catch either persistence path.
func normalizePhoneCandidates(phone string) []string {
	if phone == "" {
		return nil
	}
	out := []string{phone}
	if strings.HasPrefix(phone, "+") {
		stripped := strings.TrimPrefix(phone, "+")
		if stripped != "" {
			out = append(out, stripped)
		}
		// US: drop +1 country code to match a raw 10-digit national number.
		if strings.HasPrefix(stripped, "1") {
			noUS := strings.TrimPrefix(stripped, "1")
			if noUS != "" {
				out = append(out, noUS)
			}
		}
	}
	// Deduplicate while preserving order.
	seen := make(map[string]struct{}, len(out))
	deduped := out[:0]
	for _, v := range out {
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		deduped = append(deduped, v)
	}
	return deduped
}

// adminCreds returns a snapshot of the configured admin credentials.
func (c *IAMClient) adminCreds() AdminCreds {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.admin
}

// LookupByAttribute performs a server-to-server lookup of users matching
// attr=value within org. attr is an IAM user field name ("phone", "email",
// "name", etc.). org defaults to the client's admin Owner if empty.
// maxResults caps the page size; values <= 0 default to 10.
//
// For attr=="phone", LookupByAttribute probes multiple phone normalizations
// (raw, with leading "+", US +1 stripped) since IAM stores phones in
// inconsistent shapes depending on the signup path.
//
// Returns ([], nil) when no user matches — never an error for empty results.
// Errors are returned only for transport / decoding / IAM-side error responses.
func (c *IAMClient) LookupByAttribute(ctx context.Context, attr, value, org string, maxResults int) ([]IAMUser, error) {
	if attr == "" {
		return nil, fmt.Errorf("iam: LookupByAttribute: attr is required")
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

	// Build candidate values. Phone gets multiple normalizations; everything
	// else uses the value verbatim.
	candidates := []string{value}
	if attr == "phone" {
		candidates = normalizePhoneCandidates(value)
	}

	var matches []IAMUser
	seenIDs := make(map[string]struct{})
	for _, candidate := range candidates {
		if candidate == "" {
			continue
		}
		q := url.Values{}
		q.Set("owner", org)
		q.Set("clientId", creds.ClientID)
		q.Set("clientSecret", creds.ClientSecret)
		q.Set("pageSize", fmt.Sprintf("%d", maxResults))
		q.Set("p", "1")
		q.Set("field", attr)
		q.Set("value", candidate)

		req, err := http.NewRequestWithContext(ctx, "GET", c.baseURL+"/api/get-users?"+q.Encode(), nil)
		if err != nil {
			return nil, fmt.Errorf("iam: LookupByAttribute build request: %w", err)
		}
		resp, err := c.httpClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("iam: LookupByAttribute request: %w", err)
		}
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("iam: LookupByAttribute returned %d: %s", resp.StatusCode, truncate(string(body), 256))
		}
		var envelope struct {
			Status string `json:"status"`
			Msg    string `json:"msg"`
			Data   []struct {
				ID    string `json:"id"`
				Name  string `json:"name"`
				Email string `json:"email"`
				Owner string `json:"owner"`
			} `json:"data"`
		}
		if err := json.Unmarshal(body, &envelope); err != nil {
			return nil, fmt.Errorf("iam: LookupByAttribute decode: %w", err)
		}
		if envelope.Status == "error" {
			return nil, fmt.Errorf("iam: LookupByAttribute: %s", envelope.Msg)
		}
		for _, u := range envelope.Data {
			if u.ID == "" {
				continue
			}
			if _, ok := seenIDs[u.ID]; ok {
				continue
			}
			seenIDs[u.ID] = struct{}{}
			matches = append(matches, IAMUser{
				ID:     u.ID,
				Name:   u.Name,
				Email:  u.Email,
				OrgIDs: []string{u.Owner},
			})
			if len(matches) >= maxResults {
				return matches, nil
			}
		}
	}
	return matches, nil
}

// EnsureUser idempotently provisions an IAM user matching spec. If the user
// already exists (matched by email within spec.Owner), the existing user is
// returned without modification. Otherwise the user is created via
// POST /api/add-user and the new user is fetched and returned.
//
// EnsureUser treats both HTTP 409 and IAM's status:"error" + "already exists"
// envelope as the idempotent-replay path — IAM responds with HTTP 200 in
// either case depending on version, and both shapes mean "this user is
// already there, fetch it".
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

	payload := map[string]any{
		"owner":        owner,
		"name":         name,
		"email":        spec.Email,
		"displayName":  spec.DisplayName,
		"phone":        spec.Phone,
		"type":         spec.Type,
		"organization": owner,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("iam: EnsureUser marshal: %w", err)
	}

	q := url.Values{}
	q.Set("clientId", creds.ClientID)
	q.Set("clientSecret", creds.ClientSecret)
	// IAM's add-user uses `id` (owner/name) for routing; include both for
	// safety against handlers that read either shape.
	q.Set("id", owner+"/"+name)

	req, err := http.NewRequestWithContext(ctx, "POST",
		c.baseURL+"/api/add-user?"+q.Encode(), bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("iam: EnsureUser build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("iam: EnsureUser request: %w", err)
	}
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	resp.Body.Close()

	// HTTP 409 → already exists, fetch and return.
	if resp.StatusCode == http.StatusConflict {
		return c.fetchUserByEmail(ctx, owner, spec.Email)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("iam: EnsureUser returned %d: %s",
			resp.StatusCode, truncate(string(respBody), 256))
	}

	// HTTP 200 — could be success (status:ok) or IAM-style error envelope.
	var envelope struct {
		Status string `json:"status"`
		Msg    string `json:"msg"`
		Data   any    `json:"data"`
	}
	if err := json.Unmarshal(respBody, &envelope); err != nil {
		return nil, fmt.Errorf("iam: EnsureUser decode: %w", err)
	}
	if envelope.Status == "error" {
		// IAM's idempotent-replay signal: "X already exists". Could be
		// username, email, or phone — any of them means the user is in IAM
		// already and we should resolve it by email.
		if isAlreadyExistsMsg(envelope.Msg) {
			return c.fetchUserByEmail(ctx, owner, spec.Email)
		}
		return nil, fmt.Errorf("iam: EnsureUser: %s", envelope.Msg)
	}
	// Success — fetch the freshly-created user to get its IAM-assigned ID.
	return c.fetchUserByEmail(ctx, owner, spec.Email)
}

// fetchUserByEmail does a server-to-server lookup of a user by email within
// the given org. Used by EnsureUser to resolve both new and already-existing
// users to a canonical *IAMUser.
func (c *IAMClient) fetchUserByEmail(ctx context.Context, owner, email string) (*IAMUser, error) {
	creds := c.adminCreds()
	q := url.Values{}
	q.Set("owner", owner)
	q.Set("email", email)
	q.Set("clientId", creds.ClientID)
	q.Set("clientSecret", creds.ClientSecret)

	req, err := http.NewRequestWithContext(ctx, "GET",
		c.baseURL+"/api/get-user?"+q.Encode(), nil)
	if err != nil {
		return nil, fmt.Errorf("iam: fetchUserByEmail build request: %w", err)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("iam: fetchUserByEmail request: %w", err)
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("iam: fetchUserByEmail returned %d: %s",
			resp.StatusCode, truncate(string(body), 256))
	}
	var envelope struct {
		Status string `json:"status"`
		Msg    string `json:"msg"`
		Data   struct {
			ID    string `json:"id"`
			Name  string `json:"name"`
			Email string `json:"email"`
			Owner string `json:"owner"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, fmt.Errorf("iam: fetchUserByEmail decode: %w", err)
	}
	if envelope.Status == "error" {
		return nil, fmt.Errorf("iam: fetchUserByEmail: %s", envelope.Msg)
	}
	if envelope.Data.ID == "" {
		return nil, fmt.Errorf("iam: fetchUserByEmail: no user found for email=%s in owner=%s", email, owner)
	}
	return &IAMUser{
		ID:     envelope.Data.ID,
		Name:   envelope.Data.Name,
		Email:  envelope.Data.Email,
		OrgIDs: []string{envelope.Data.Owner},
	}, nil
}

// isAlreadyExistsMsg recognizes IAM's various "X already exists" error messages
// emitted by add-user when the username/email/phone collides.
func isAlreadyExistsMsg(msg string) bool {
	if msg == "" {
		return false
	}
	low := strings.ToLower(msg)
	return strings.Contains(low, "already exists")
}

// truncate clips s to at most n runes, appending "…" if truncated. Used for
// safe inclusion of IAM error bodies in returned error messages.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
