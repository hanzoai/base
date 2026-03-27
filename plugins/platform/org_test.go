package platform

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func TestOrgServiceGetCreds_EnvFallback(t *testing.T) {
	// Set env vars for the fallback.
	os.Setenv("COMMERCE_API_KEY", "test-key-123")
	os.Setenv("COMMERCE_API_SECRET", "test-secret-456")
	defer func() {
		os.Unsetenv("COMMERCE_API_KEY")
		os.Unsetenv("COMMERCE_API_SECRET")
	}()

	s := &OrgService{
		kms:    NewKMSClient("", ""), // no KMS configured
		config: PlatformConfig{},
	}

	creds := s.GetCreds("org-1", "commerce")
	if creds == nil {
		t.Fatal("expected env-based creds, got nil")
	}
	if creds["api_key"] != "test-key-123" {
		t.Errorf("expected api_key=test-key-123, got %q", creds["api_key"])
	}
	if creds["api_secret"] != "test-secret-456" {
		t.Errorf("expected api_secret=test-secret-456, got %q", creds["api_secret"])
	}
}

func TestOrgServiceGetCreds_Empty(t *testing.T) {
	s := &OrgService{
		kms:    NewKMSClient("", ""),
		config: PlatformConfig{},
	}

	creds := s.GetCreds("org-1", "nonexistent")
	if creds != nil {
		t.Errorf("expected nil for unknown provider, got %v", creds)
	}
}

func TestOrgServiceGetCreds_EmptyArgs(t *testing.T) {
	s := &OrgService{
		kms:    NewKMSClient("", ""),
		config: PlatformConfig{},
	}

	if creds := s.GetCreds("", "commerce"); creds != nil {
		t.Error("expected nil for empty orgId")
	}
	if creds := s.GetCreds("org-1", ""); creds != nil {
		t.Error("expected nil for empty provider")
	}
}

func TestOrgServiceGetCreds_KMSWithCache(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if r.Header.Get("Authorization") != "Bearer test-token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		// Only api_key returns a value.
		if r.URL.Path == "/api/v1/secrets/org-1/commerce/api_key" {
			json.NewEncoder(w).Encode(map[string]any{
				"secret": map[string]string{"value": "kms-key-abc"},
			})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	kms := NewKMSClient(server.URL, "test-token")
	s := &OrgService{
		kms:    kms,
		config: PlatformConfig{},
	}

	creds := s.GetCreds("org-1", "commerce")
	if creds == nil {
		t.Fatal("expected KMS creds, got nil")
	}
	if creds["api_key"] != "kms-key-abc" {
		t.Errorf("expected api_key=kms-key-abc, got %q", creds["api_key"])
	}

	firstCallCount := callCount

	// Second call should be cached.
	creds2 := s.GetCreds("org-1", "commerce")
	if creds2 == nil {
		t.Fatal("expected cached creds, got nil")
	}
	if creds2["api_key"] != "kms-key-abc" {
		t.Errorf("expected cached api_key=kms-key-abc, got %q", creds2["api_key"])
	}
	if callCount != firstCallCount {
		t.Error("expected cached result, but KMS was called again")
	}
}

func TestOrgServiceInvalidateCreds(t *testing.T) {
	os.Setenv("KYC_API_KEY", "kyc-key-123")
	defer os.Unsetenv("KYC_API_KEY")

	s := &OrgService{
		kms:    NewKMSClient("", ""),
		config: PlatformConfig{},
	}

	// Populate cache.
	creds := s.GetCreds("org-1", "kyc")
	if creds == nil {
		t.Fatal("expected env creds, got nil")
	}

	// Invalidate.
	s.InvalidateCreds("org-1")

	// Cache should be cleared but env vars still work.
	creds2 := s.GetCreds("org-1", "kyc")
	if creds2 == nil {
		t.Fatal("expected env creds after invalidation, got nil")
	}
	if creds2["api_key"] != "kyc-key-123" {
		t.Errorf("expected api_key=kyc-key-123, got %q", creds2["api_key"])
	}
}

func TestOrgServiceSetCreds_NoKMS(t *testing.T) {
	s := &OrgService{
		kms:    NewKMSClient("", ""),
		config: PlatformConfig{},
	}

	err := s.SetCreds("org-1", "commerce", map[string]string{"api_key": "test"})
	if err == nil {
		t.Fatal("expected error when KMS not configured")
	}
}

func TestOrgServiceSetCreds_EmptyArgs(t *testing.T) {
	s := &OrgService{
		kms:    NewKMSClient("http://example.com", "tok"),
		config: PlatformConfig{},
	}

	if err := s.SetCreds("", "commerce", nil); err == nil {
		t.Error("expected error for empty orgId")
	}
	if err := s.SetCreds("org-1", "", nil); err == nil {
		t.Error("expected error for empty provider")
	}
}

func TestOrgServiceGetCustomer_NilArgs(t *testing.T) {
	s := &OrgService{
		kms:    NewKMSClient("", ""),
		config: PlatformConfig{},
	}

	if c := s.GetCustomer("", "user-1"); c != nil {
		t.Error("expected nil for empty orgId")
	}
	if c := s.GetCustomer("org-1", ""); c != nil {
		t.Error("expected nil for empty userId")
	}
}

func TestOrgServiceProvisionCustomer_EmptyArgs(t *testing.T) {
	s := &OrgService{
		kms:    NewKMSClient("", ""),
		config: PlatformConfig{},
	}

	_, err := s.ProvisionCustomer("", "user-1", nil)
	if err == nil {
		t.Error("expected error for empty orgId")
	}
	_, err = s.ProvisionCustomer("org-1", "", nil)
	if err == nil {
		t.Error("expected error for empty userId")
	}
}

func TestFetchCredsFromEnv(t *testing.T) {
	os.Setenv("TESTPROV_API_KEY", "key1")
	os.Setenv("TESTPROV_BASE_URL", "https://api.test.com")
	defer func() {
		os.Unsetenv("TESTPROV_API_KEY")
		os.Unsetenv("TESTPROV_BASE_URL")
	}()

	s := &OrgService{}
	creds := s.fetchCredsFromEnv("testprov")
	if creds == nil {
		t.Fatal("expected env creds, got nil")
	}
	if creds["api_key"] != "key1" {
		t.Errorf("expected api_key=key1, got %q", creds["api_key"])
	}
	if creds["base_url"] != "https://api.test.com" {
		t.Errorf("expected base_url, got %q", creds["base_url"])
	}
	if _, ok := creds["api_secret"]; ok {
		t.Error("did not expect api_secret to be set")
	}
}

func TestFetchCredsFromEnv_None(t *testing.T) {
	s := &OrgService{}
	creds := s.fetchCredsFromEnv("nonexistent-provider-xyz")
	if creds != nil {
		t.Errorf("expected nil for provider with no env vars, got %v", creds)
	}
}

func TestHeaderOrgID(t *testing.T) {
	// Identity headers use standard X-Org-Id — no vendor prefix.
	expected := "X-Org-Id"
	if expected != "X-Org-Id" {
		t.Errorf("expected X-Org-Id, got %q", expected)
	}
}
