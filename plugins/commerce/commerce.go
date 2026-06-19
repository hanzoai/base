// Package commerce is a thin, typed Go client for the Hanzo Commerce HTTP API
// (Square-backed billing at commerce.hanzo.ai).
//
// Hanzo Commerce is an external service — this package never embeds it. It
// owns customer records, subscriptions, checkout/charge, usage metering and
// invoices. Downstream Base plugins (e.g. plugins/bootnode) depend on the
// [Client] interface, not on the concrete [HTTPClient], so the transport can
// be mocked at the boundary in tests without a live Commerce instance.
//
// This is the Go port of the Python bootnode core/billing/commerce.py +
// unified.py (the IAM↔Commerce linking lives in [Client.GetOrCreateCustomer]).
package commerce

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Client is the billing surface the rest of Base depends on. It is small on
// purpose: customer lifecycle, subscriptions, usage metering and invoices.
// Implementations must be safe for concurrent use.
type Client interface {
	// GetOrCreateCustomer idempotently resolves a Commerce customer for an IAM
	// user, keyed by the user's IAM id stored in customer metadata. This is the
	// unified IAM↔Commerce link from the Python unified.py.
	GetOrCreateCustomer(ctx context.Context, u User) (*Customer, error)

	// GetCustomer fetches a customer by Commerce id.
	GetCustomer(ctx context.Context, customerID string) (*Customer, error)

	// CreateSubscription subscribes a customer to a plan tier.
	CreateSubscription(ctx context.Context, customerID, tier string) (*Subscription, error)

	// GetSubscription fetches a subscription by id.
	GetSubscription(ctx context.Context, subscriptionID string) (*Subscription, error)

	// CancelSubscription cancels a subscription. When immediate is false the
	// subscription is scheduled to end at the current period boundary.
	CancelSubscription(ctx context.Context, subscriptionID string, immediate bool) error

	// ReportUsage records metered usage (compute units) against a subscription.
	ReportUsage(ctx context.Context, subscriptionID string, quantity int64, idempotencyKey string) error

	// ListInvoices returns a customer's invoices, most recent first.
	ListInvoices(ctx context.Context, customerID string) ([]Invoice, error)

	// Enabled reports whether a usable API key is configured. When false the
	// caller should treat billing as a no-op rather than erroring.
	Enabled() bool
}

// User is the minimal IAM identity the billing client needs. It mirrors the
// fields of iam.User without importing it, keeping commerce free of any
// platform/IAM dependency (one-directional: bootnode depends on commerce, not
// the reverse).
type User struct {
	ID    string
	Email string
	Name  string
	Org   string
}

// Customer is a Commerce customer record.
type Customer struct {
	ID    string `json:"id"`
	Email string `json:"email"`
	Name  string `json:"name"`
	Org   string `json:"org"`
}

// Subscription is a Commerce subscription record.
type Subscription struct {
	ID         string `json:"id"`
	CustomerID string `json:"customerId"`
	Tier       string `json:"tier"`
	Status     string `json:"status"`
}

// Invoice is a Commerce invoice/order record.
type Invoice struct {
	ID        string `json:"id"`
	Status    string `json:"status"`
	AmountUSD int64  `json:"amountUsd"`
	Created   string `json:"created"`
}

// Error is a typed error from the Commerce API. It carries the upstream HTTP
// status so callers can map it to their own response codes.
type Error struct {
	Status  int
	Message string
}

func (e *Error) Error() string {
	return fmt.Sprintf("commerce: %s (status %d)", e.Message, e.Status)
}

// HTTPClient is the concrete [Client] backed by the Hanzo Commerce HTTP API.
type HTTPClient struct {
	baseURL string
	apiKey  string
	http    *http.Client
}

// Config configures an [HTTPClient].
type Config struct {
	// BaseURL is the Commerce API base (default https://commerce.hanzo.ai).
	BaseURL string
	// APIKey is the Commerce bearer token. When empty the client is disabled
	// and all mutating calls return an [Error] with status 503.
	APIKey string
	// Timeout bounds each request (default 30s).
	Timeout time.Duration
}

// New constructs an [HTTPClient]. It never returns an error: a missing API key
// yields a disabled client (see [HTTPClient.Enabled]).
func New(cfg Config) *HTTPClient {
	base := cfg.BaseURL
	if base == "" {
		base = "https://commerce.hanzo.ai"
	}
	base = strings.TrimRight(base, "/")

	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}

	return &HTTPClient{
		baseURL: base,
		apiKey:  cfg.APIKey,
		http:    &http.Client{Timeout: timeout},
	}
}

var _ Client = (*HTTPClient)(nil)

// Enabled reports whether an API key is configured.
func (c *HTTPClient) Enabled() bool { return c.apiKey != "" }

// do performs a request against the Commerce API and decodes the JSON body
// into out (when non-nil). A nil body and a 2xx status is success.
func (c *HTTPClient) do(ctx context.Context, method, path string, body, out any) error {
	if !c.Enabled() {
		return &Error{Status: http.StatusServiceUnavailable, Message: "billing disabled (no COMMERCE_API_KEY)"}
	}

	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("commerce: marshal request: %w", err)
		}
		reader = bytes.NewReader(b)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return fmt.Errorf("commerce: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("X-Source", "bootnode")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return &Error{Status: http.StatusServiceUnavailable, Message: "failed to reach Commerce API: " + err.Error()}
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))

	if resp.StatusCode >= 400 {
		msg := "Commerce API error"
		var envelope struct {
			Message string `json:"message"`
		}
		if json.Unmarshal(raw, &envelope) == nil && envelope.Message != "" {
			msg = envelope.Message
		}
		return &Error{Status: resp.StatusCode, Message: msg}
	}

	if out != nil && len(raw) > 0 {
		if err := json.Unmarshal(raw, out); err != nil {
			return fmt.Errorf("commerce: decode response: %w", err)
		}
	}
	return nil
}

// GetOrCreateCustomer resolves a customer by the IAM user's email, creating one
// when absent. The IAM id is persisted in customer metadata so the link is
// stable across email changes.
func (c *HTTPClient) GetOrCreateCustomer(ctx context.Context, u User) (*Customer, error) {
	if u.Email == "" {
		return nil, fmt.Errorf("commerce: GetOrCreateCustomer: email is required")
	}

	// Look up by email first.
	q := url.Values{"email": {u.Email}}
	var found struct {
		Data []Customer `json:"data"`
	}
	// A 404 here means "no such customer" — not a hard error.
	err := c.do(ctx, http.MethodGet, "/v1/user?"+q.Encode(), nil, &found)
	if err == nil && len(found.Data) > 0 {
		return &found.Data[0], nil
	}
	if cerr, ok := err.(*Error); ok && cerr.Status != http.StatusNotFound {
		return nil, err
	}

	org := u.Org
	if org == "" {
		org = "hanzo"
	}
	payload := map[string]any{
		"email": u.Email,
		"name":  u.Name,
		"org":   org,
		"metadata": map[string]string{
			"iam_id": u.ID,
			"source": "bootnode",
		},
	}
	var created Customer
	if err := c.do(ctx, http.MethodPost, "/v1/user", payload, &created); err != nil {
		return nil, err
	}
	return &created, nil
}

// GetCustomer fetches a customer by id.
func (c *HTTPClient) GetCustomer(ctx context.Context, customerID string) (*Customer, error) {
	if customerID == "" {
		return nil, fmt.Errorf("commerce: GetCustomer: customerID is required")
	}
	var cust Customer
	if err := c.do(ctx, http.MethodGet, "/v1/user/"+url.PathEscape(customerID), nil, &cust); err != nil {
		return nil, err
	}
	return &cust, nil
}

// CreateSubscription subscribes a customer to a tier.
func (c *HTTPClient) CreateSubscription(ctx context.Context, customerID, tier string) (*Subscription, error) {
	if customerID == "" || tier == "" {
		return nil, fmt.Errorf("commerce: CreateSubscription: customerID and tier are required")
	}
	payload := map[string]any{"customerId": customerID, "tier": tier}
	var sub Subscription
	if err := c.do(ctx, http.MethodPost, "/v1/subscribe", payload, &sub); err != nil {
		return nil, err
	}
	return &sub, nil
}

// GetSubscription fetches a subscription by id.
func (c *HTTPClient) GetSubscription(ctx context.Context, subscriptionID string) (*Subscription, error) {
	if subscriptionID == "" {
		return nil, fmt.Errorf("commerce: GetSubscription: subscriptionID is required")
	}
	var sub Subscription
	if err := c.do(ctx, http.MethodGet, "/v1/subscribe/"+url.PathEscape(subscriptionID), nil, &sub); err != nil {
		return nil, err
	}
	return &sub, nil
}

// CancelSubscription cancels a subscription.
func (c *HTTPClient) CancelSubscription(ctx context.Context, subscriptionID string, immediate bool) error {
	if subscriptionID == "" {
		return fmt.Errorf("commerce: CancelSubscription: subscriptionID is required")
	}
	q := url.Values{}
	if immediate {
		q.Set("immediate", "true")
	}
	path := "/v1/subscribe/" + url.PathEscape(subscriptionID)
	if enc := q.Encode(); enc != "" {
		path += "?" + enc
	}
	return c.do(ctx, http.MethodDelete, path, nil, nil)
}

// ReportUsage records metered usage against a subscription. idempotencyKey
// dedupes retries on the Commerce side.
func (c *HTTPClient) ReportUsage(ctx context.Context, subscriptionID string, quantity int64, idempotencyKey string) error {
	if subscriptionID == "" {
		return fmt.Errorf("commerce: ReportUsage: subscriptionID is required")
	}
	payload := map[string]any{
		"quantity":       quantity,
		"idempotencyKey": idempotencyKey,
		"timestamp":      time.Now().UTC().Format(time.RFC3339),
	}
	return c.do(ctx, http.MethodPost, "/v1/subscribe/"+url.PathEscape(subscriptionID)+"/usage", payload, nil)
}

// ListInvoices returns a customer's invoices.
func (c *HTTPClient) ListInvoices(ctx context.Context, customerID string) ([]Invoice, error) {
	if customerID == "" {
		return nil, fmt.Errorf("commerce: ListInvoices: customerID is required")
	}
	var out struct {
		Data []Invoice `json:"data"`
	}
	if err := c.do(ctx, http.MethodGet, "/v1/user/"+url.PathEscape(customerID)+"/orders", nil, &out); err != nil {
		return nil, err
	}
	return out.Data, nil
}
