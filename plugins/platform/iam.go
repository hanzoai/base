package platform

import (
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
