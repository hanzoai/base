package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Client is a thin HTTP client for the Base API.
type Client struct {
	BaseURL   string
	Token     string
	Tenant    string
	APIPrefix string
	HTTP      *http.Client
}

// NewClient returns a Client with sensible defaults.
func NewClient(baseURL, token, tenant string) *Client {
	return &Client{
		BaseURL:   strings.TrimRight(baseURL, "/"),
		Token:     token,
		Tenant:    tenant,
		APIPrefix: "/api",
		HTTP:      &http.Client{Timeout: 30 * time.Second},
	}
}

// Do executes a raw HTTP request against the Base API and returns
// the decoded JSON body. Non-2xx responses are returned as an error.
func (c *Client) Do(method, path string, body any) (json.RawMessage, int, error) {
	var reqBody io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, 0, fmt.Errorf("marshal request body: %w", err)
		}
		reqBody = bytes.NewReader(b)
	}

	fullURL := c.BaseURL + c.APIPrefix + path

	req, err := http.NewRequest(method, fullURL, reqBody)
	if err != nil {
		return nil, 0, fmt.Errorf("build request: %w", err)
	}

	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.Token != "" {
		req.Header.Set("Authorization", c.Token)
	}
	if c.Tenant != "" {
		req.Header.Set("X-Org-Id", c.Tenant)
	}

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		return nil, resp.StatusCode, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(raw))
	}

	if len(raw) == 0 {
		return nil, resp.StatusCode, nil
	}

	return json.RawMessage(raw), resp.StatusCode, nil
}

// Get performs a GET request.
func (c *Client) Get(path string) (json.RawMessage, int, error) {
	return c.Do(http.MethodGet, path, nil)
}

// Post performs a POST request with a JSON body.
func (c *Client) Post(path string, body any) (json.RawMessage, int, error) {
	return c.Do(http.MethodPost, path, body)
}

// Patch performs a PATCH request with a JSON body.
func (c *Client) Patch(path string, body any) (json.RawMessage, int, error) {
	return c.Do(http.MethodPatch, path, body)
}

// Delete performs a DELETE request.
func (c *Client) Delete(path string) (json.RawMessage, int, error) {
	return c.Do(http.MethodDelete, path, nil)
}

// BuildQuery encodes query parameters for list endpoints.
func BuildQuery(filter string, limit int, sort string, extra map[string]string) string {
	params := url.Values{}
	if filter != "" {
		params.Set("filter", filter)
	}
	if limit > 0 {
		params.Set("perPage", fmt.Sprintf("%d", limit))
	}
	if sort != "" {
		params.Set("sort", sort)
	}
	for k, v := range extra {
		params.Set(k, v)
	}
	if len(params) == 0 {
		return ""
	}
	return "?" + params.Encode()
}
