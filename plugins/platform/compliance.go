package platform

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// ComplianceClient handles communication with the luxfi/compliance service.
// The compliance service provides KYC/AML, sanctions screening, transaction
// monitoring, and regulatory validation. This is an optional extension —
// if ComplianceEndpoint is empty, compliance features are disabled.
type ComplianceClient struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client
}

// NewComplianceClient creates a client for the compliance service.
func NewComplianceClient(baseURL, apiKey string) *ComplianceClient {
	if baseURL == "" {
		return nil
	}
	return &ComplianceClient{
		baseURL: strings.TrimRight(baseURL, "/"),
		apiKey:  apiKey,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// ComplianceStatus represents a user's KYC/compliance status.
type ComplianceStatus struct {
	ApplicationID string `json:"application_id"`
	Status        string `json:"status"`     // draft, pending, pending_kyc, approved, rejected
	KYCStatus     string `json:"kyc_status"` // not_started, pending, verified, failed
	KYCProvider   string `json:"kyc_provider,omitempty"`
}

// ScreeningResult from AML/sanctions screening.
type ScreeningResult struct {
	RiskLevel string `json:"risk_level"` // low, medium, high, critical
	Matches   int    `json:"matches"`
	Cleared   bool   `json:"cleared"`
}

// CreateApplication creates a compliance application for a user.
func (c *ComplianceClient) CreateApplication(givenName, familyName, email, country string) (string, error) {
	if c == nil {
		return "", fmt.Errorf("compliance: not configured")
	}

	body := map[string]string{
		"given_name":  givenName,
		"family_name": familyName,
		"email":       email,
		"country":     country,
	}

	var result struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	}
	if err := c.post("/v1/applications", body, &result); err != nil {
		return "", err
	}
	return result.ID, nil
}

// InitiateKYC starts identity verification for an application.
func (c *ComplianceClient) InitiateKYC(applicationID, provider string) (verificationID, redirectURL string, err error) {
	if c == nil {
		return "", "", fmt.Errorf("compliance: not configured")
	}

	reqBody := map[string]string{
		"application_id": applicationID,
	}
	if provider != "" {
		reqBody["provider"] = provider
	}

	var result struct {
		VerificationID string `json:"verification_id"`
		RedirectURL    string `json:"redirect_url"`
	}
	if err := c.post("/v1/kyc/verify", reqBody, &result); err != nil {
		return "", "", err
	}
	return result.VerificationID, result.RedirectURL, nil
}

// GetKYCStatus returns the current KYC status for an application.
func (c *ComplianceClient) GetKYCStatus(applicationID string) (*ComplianceStatus, error) {
	if c == nil {
		return nil, fmt.Errorf("compliance: not configured")
	}

	var result ComplianceStatus
	if err := c.get("/v1/kyc/status/"+applicationID, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// ScreenIndividual runs AML/sanctions screening.
func (c *ComplianceClient) ScreenIndividual(givenName, familyName, country string) (*ScreeningResult, error) {
	if c == nil {
		return nil, fmt.Errorf("compliance: not configured")
	}

	body := map[string]string{
		"given_name":  givenName,
		"family_name": familyName,
		"country":     country,
	}

	var result ScreeningResult
	if err := c.post("/v1/aml/screen", body, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// ValidatePayment checks payment compliance (travel rule, sanctions, CTR).
func (c *ComplianceClient) ValidatePayment(fromID, toID string, amount float64, currency, jurisdiction string) (approved bool, reason string, err error) {
	if c == nil {
		return true, "compliance not configured, allowing by default", nil
	}

	body := map[string]interface{}{
		"from_account_id": fromID,
		"to_account_id":   toID,
		"amount":          amount,
		"currency":        currency,
		"jurisdiction":    jurisdiction,
	}

	var result struct {
		Decision string `json:"decision"` // approve, decline, review
		Reason   string `json:"reason"`
	}
	if err := c.post("/v1/payments/validate", body, &result); err != nil {
		return false, "", err
	}
	return result.Decision == "approve", result.Reason, nil
}

// Enabled returns true if the compliance client is configured.
func (c *ComplianceClient) Enabled() bool {
	return c != nil && c.baseURL != ""
}

func (c *ComplianceClient) post(path string, body interface{}, result interface{}) error {
	jsonBody, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("compliance: marshal request: %w", err)
	}

	req, err := http.NewRequest("POST", c.baseURL+path, bytes.NewReader(jsonBody))
	if err != nil {
		return fmt.Errorf("compliance: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		req.Header.Set("X-Api-Key", c.apiKey)
	}

	return c.doRequest(req, result)
}

func (c *ComplianceClient) get(path string, result interface{}) error {
	req, err := http.NewRequest("GET", c.baseURL+path, nil)
	if err != nil {
		return fmt.Errorf("compliance: create request: %w", err)
	}
	if c.apiKey != "" {
		req.Header.Set("X-Api-Key", c.apiKey)
	}

	return c.doRequest(req, result)
}

func (c *ComplianceClient) doRequest(req *http.Request, result interface{}) error {
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("compliance: request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return fmt.Errorf("compliance: %s returned %d: %s", req.URL.Path, resp.StatusCode, string(body))
	}

	if result != nil {
		if err := json.NewDecoder(resp.Body).Decode(result); err != nil {
			return fmt.Errorf("compliance: decode response: %w", err)
		}
	}
	return nil
}
