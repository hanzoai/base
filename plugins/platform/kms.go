package platform

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

const secretCacheTTL = 1 * time.Minute

// FetchSecret fetches a secret value from Hanzo KMS at the given path.
// Uses Universal Auth machine identity (config.IAMClientID / IAMClientSecret).
func FetchSecret(path string, config PlatformConfig) (string, error) {
	endpoint := config.KMSEndpoint
	if endpoint == "" {
		return "", fmt.Errorf("kms: endpoint not configured")
	}
	endpoint = strings.TrimRight(endpoint, "/")

	token, err := authenticateKMS(config)
	if err != nil {
		return "", fmt.Errorf("kms: auth failed: %w", err)
	}

	url := fmt.Sprintf("%s/api/v3/secrets/raw/%s", endpoint, path)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return "", fmt.Errorf("kms: create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("kms: request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return "", fmt.Errorf("kms: get secret returned %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Secret struct {
			SecretValue string `json:"secretValue"`
		} `json:"secret"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("kms: decode response: %w", err)
	}

	return result.Secret.SecretValue, nil
}

// CreateOrgProject creates a KMS project (workspace environment) for an
// org identified by slug. Returns the project/environment ID.
func CreateOrgProject(orgSlug string, config PlatformConfig) (string, error) {
	endpoint := config.KMSEndpoint
	if endpoint == "" {
		return "", fmt.Errorf("kms: endpoint not configured")
	}
	endpoint = strings.TrimRight(endpoint, "/")

	token, err := authenticateKMS(config)
	if err != nil {
		return "", fmt.Errorf("kms: auth failed: %w", err)
	}

	payload, err := json.Marshal(map[string]string{
		"name": "org-" + orgSlug,
		"slug": orgSlug,
	})
	if err != nil {
		return "", fmt.Errorf("kms: marshal payload: %w", err)
	}

	req, err := http.NewRequest("POST", endpoint+"/api/v2/workspace/environments", bytes.NewReader(payload))
	if err != nil {
		return "", fmt.Errorf("kms: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("kms: request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return "", fmt.Errorf("kms: create project returned %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Environment struct {
			ID   string `json:"id"`
			Slug string `json:"slug"`
		} `json:"environment"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("kms: decode response: %w", err)
	}

	return result.Environment.ID, nil
}

// kmsTokenCache caches the KMS access token to avoid re-authenticating on every call.
var (
	kmsTokenMu      sync.Mutex
	kmsTokenValue   string
	kmsTokenExpires time.Time
)

// authenticateKMS obtains a short-lived access token via Universal Auth.
// Caches the token and reuses it until 1 minute before expiry.
func authenticateKMS(config PlatformConfig) (string, error) {
	kmsTokenMu.Lock()
	defer kmsTokenMu.Unlock()

	if kmsTokenValue != "" && time.Now().Before(kmsTokenExpires) {
		return kmsTokenValue, nil
	}

	endpoint := config.KMSEndpoint
	if endpoint == "" {
		return "", fmt.Errorf("kms: endpoint not configured")
	}
	endpoint = strings.TrimRight(endpoint, "/")

	payload, err := json.Marshal(map[string]string{
		"clientId":     config.IAMClientID,
		"clientSecret": config.IAMClientSecret,
	})
	if err != nil {
		return "", fmt.Errorf("kms: marshal auth payload: %w", err)
	}

	resp, err := http.Post(
		endpoint+"/api/v1/auth/universal-auth/login",
		"application/json",
		bytes.NewReader(payload),
	)
	if err != nil {
		return "", fmt.Errorf("kms: auth request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return "", fmt.Errorf("kms: auth returned %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		AccessToken string `json:"accessToken"`
		ExpiresIn   int    `json:"expiresIn"` // seconds
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("kms: decode auth response: %w", err)
	}

	kmsTokenValue = result.AccessToken
	// Cache until 1 minute before expiry, default to 4 minutes if not provided.
	ttl := time.Duration(result.ExpiresIn)*time.Second - time.Minute
	if ttl <= 0 {
		ttl = 4 * time.Minute
	}
	kmsTokenExpires = time.Now().Add(ttl)

	return kmsTokenValue, nil
}

// --------------------------------------------------------------------------
// KMSClient with caching (for production use)
// --------------------------------------------------------------------------

type secretCacheEntry struct {
	value   string
	expires time.Time
}

// KMSClient handles secret operations with caching.
type KMSClient struct {
	baseURL    string
	authToken  string
	httpClient *http.Client

	mu    sync.RWMutex
	cache map[string]*secretCacheEntry
}

// NewKMSClient creates a new KMS client. If baseURL or authToken is empty,
// operations will return errors but the plugin still functions.
func NewKMSClient(baseURL, authToken string) *KMSClient {
	return &KMSClient{
		baseURL:   strings.TrimRight(baseURL, "/"),
		authToken: authToken,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
		cache: make(map[string]*secretCacheEntry),
	}
}

// GetSecret fetches a secret with caching (1 min TTL).
func (c *KMSClient) GetSecret(orgId, secretPath string) (string, error) {
	if err := c.checkConfig(); err != nil {
		return "", err
	}

	cacheKey := orgId + "/" + secretPath

	c.mu.RLock()
	entry, ok := c.cache[cacheKey]
	c.mu.RUnlock()

	if ok && time.Now().Before(entry.expires) {
		return entry.value, nil
	}

	url := fmt.Sprintf("%s/api/v1/secrets/%s/%s", c.baseURL, orgId, secretPath)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return "", fmt.Errorf("kms: create request: %w", err)
	}
	c.setAuth(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("kms: request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return "", fmt.Errorf("kms: get secret returned %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Secret struct {
			Value string `json:"value"`
		} `json:"secret"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("kms: decode response: %w", err)
	}

	c.mu.Lock()
	c.cache[cacheKey] = &secretCacheEntry{
		value:   result.Secret.Value,
		expires: time.Now().Add(secretCacheTTL),
	}
	if len(c.cache) > 5000 {
		c.evictExpiredLocked()
	}
	c.mu.Unlock()

	return result.Secret.Value, nil
}

// SetSecret creates or updates a secret.
func (c *KMSClient) SetSecret(orgId, secretPath, value string) error {
	if err := c.checkConfig(); err != nil {
		return err
	}

	url := fmt.Sprintf("%s/api/v1/secrets/%s/%s", c.baseURL, orgId, secretPath)
	payload, _ := json.Marshal(map[string]string{"value": value})

	req, err := http.NewRequest("POST", url, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("kms: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	c.setAuth(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("kms: request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("kms: set secret returned %d: %s", resp.StatusCode, string(body))
	}

	cacheKey := orgId + "/" + secretPath
	c.mu.Lock()
	delete(c.cache, cacheKey)
	c.mu.Unlock()

	return nil
}

// DeleteSecret removes a secret.
func (c *KMSClient) DeleteSecret(orgId, secretPath string) error {
	if err := c.checkConfig(); err != nil {
		return err
	}

	url := fmt.Sprintf("%s/api/v1/secrets/%s/%s", c.baseURL, orgId, secretPath)
	req, err := http.NewRequest("DELETE", url, nil)
	if err != nil {
		return fmt.Errorf("kms: create request: %w", err)
	}
	c.setAuth(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("kms: request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("kms: delete secret returned %d: %s", resp.StatusCode, string(body))
	}

	cacheKey := orgId + "/" + secretPath
	c.mu.Lock()
	delete(c.cache, cacheKey)
	c.mu.Unlock()

	return nil
}

func (c *KMSClient) checkConfig() error {
	if c.baseURL == "" {
		return fmt.Errorf("kms: base URL not configured")
	}
	if c.authToken == "" {
		return fmt.Errorf("kms: auth token not configured")
	}
	return nil
}

func (c *KMSClient) setAuth(req *http.Request) {
	req.Header.Set("Authorization", "Bearer "+c.authToken)
}

func (c *KMSClient) evictExpiredLocked() {
	now := time.Now()
	for k, v := range c.cache {
		if now.After(v.expires) {
			delete(c.cache, k)
		}
	}
}

// InvalidateCache clears all cached secrets for an org.
func (c *KMSClient) InvalidateCache(orgId string) {
	prefix := orgId + "/"
	c.mu.Lock()
	for k := range c.cache {
		if strings.HasPrefix(k, prefix) {
			delete(c.cache, k)
		}
	}
	c.mu.Unlock()
}
