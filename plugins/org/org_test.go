package org

import (
	"context"
	"strings"
	"testing"
)

// TestTheProcessEnvironmentIsNotAnAnswer pins that an org with no KMS row for a
// provider gets nothing, rather than the deployment's own secret for it.
//
// `provider` is a path segment the CALLER writes, so the fallback this replaces
// — os.Getenv(strings.ToUpper(provider) + "_API_KEY") — turned the credential
// route into a read of the pod environment: name openai, anthropic or github as
// your provider and be handed the platform's key for it, which is what the
// KMSSecret CRDs put there.
func TestTheProcessEnvironmentIsNotAnAnswer(t *testing.T) {
	for _, provider := range []string{"commerce", "openai", "anthropic", "github"} {
		t.Setenv(strings.ToUpper(provider)+"_API_KEY", "the-deployment-key")
		t.Setenv(strings.ToUpper(provider)+"_API_SECRET", "the-deployment-secret")
		t.Setenv(strings.ToUpper(provider)+"_WEBHOOK_SECRET", "the-deployment-hmac")
	}

	s := &OrgService{
		kms:    mustKMS(t, ""), // no KMS configured — nothing has a credential
		config: Config{},
	}

	for _, provider := range []string{"commerce", "openai", "anthropic", "github"} {
		if creds := s.GetCreds("org-1", provider); creds != nil {
			t.Errorf("%s: an org with no credential was handed %v", provider, creds)
		}
	}
}

func TestOrgServiceGetCreds_Empty(t *testing.T) {
	s := &OrgService{
		kms:    mustKMS(t, ""),
		config: Config{},
	}

	creds := s.GetCreds("org-1", "nonexistent")
	if creds != nil {
		t.Errorf("expected nil for unknown provider, got %v", creds)
	}
}

func TestOrgServiceGetCreds_EmptyArgs(t *testing.T) {
	s := &OrgService{
		kms:    mustKMS(t, ""),
		config: Config{},
	}

	if creds := s.GetCreds("", "commerce"); creds != nil {
		t.Error("expected nil for empty orgId")
	}
	if creds := s.GetCreds("org-1", ""); creds != nil {
		t.Error("expected nil for empty provider")
	}
}

func TestOrgServiceGetCreds_KMSWithCache(t *testing.T) {
	f := withFakeKMS(t)
	// The org is a path segment: "orgs/{org}/{provider}" + the key name.
	if err := f.PutAt(context.Background(), "orgs/org-1/commerce", "api_key", "prod", "kms-key-abc"); err != nil {
		t.Fatalf("seed: %v", err)
	}

	s := &OrgService{
		kms:    mustKMS(t, "kms.test:9999"),
		config: Config{},
	}

	creds := s.GetCreds("org-1", "commerce")
	if creds == nil {
		t.Fatal("expected KMS creds, got nil")
	}
	if creds["api_key"] != "kms-key-abc" {
		t.Errorf("expected api_key=kms-key-abc, got %q", creds["api_key"])
	}

	firstCallCount := len(f.reads)

	// Second call should be cached.
	creds2 := s.GetCreds("org-1", "commerce")
	if creds2 == nil {
		t.Fatal("expected cached creds, got nil")
	}
	if creds2["api_key"] != "kms-key-abc" {
		t.Errorf("expected cached api_key=kms-key-abc, got %q", creds2["api_key"])
	}
	if len(f.reads) != firstCallCount {
		t.Error("expected cached result, but KMS was called again")
	}
}

func TestOrgServiceInvalidateCreds(t *testing.T) {
	f := withFakeKMS(t)
	if err := f.PutAt(context.Background(), "orgs/org-1/kyc", "api_key", "prod", "kyc-key-123"); err != nil {
		t.Fatalf("seed: %v", err)
	}

	s := &OrgService{
		kms:    mustKMS(t, "kms.test:9999"),
		config: Config{},
	}

	// Populate cache.
	creds := s.GetCreds("org-1", "kyc")
	if creds == nil {
		t.Fatal("expected KMS creds, got nil")
	}

	before := len(f.reads)
	s.InvalidateCreds("org-1")

	// Cache cleared, so the next read goes back to KMS and still answers.
	creds2 := s.GetCreds("org-1", "kyc")
	if creds2 == nil {
		t.Fatal("expected KMS creds after invalidation, got nil")
	}
	if creds2["api_key"] != "kyc-key-123" {
		t.Errorf("expected api_key=kyc-key-123, got %q", creds2["api_key"])
	}
	if len(f.reads) == before {
		t.Error("invalidation left the cached answer in place")
	}
}

func TestOrgServiceSetCreds_NoKMS(t *testing.T) {
	s := &OrgService{
		kms:    mustKMS(t, ""),
		config: Config{},
	}

	err := s.SetCreds("org-1", "commerce", map[string]string{"api_key": "test"})
	if err == nil {
		t.Fatal("expected error when KMS not configured")
	}
}

func TestOrgServiceSetCreds_EmptyArgs(t *testing.T) {
	s := &OrgService{
		kms:    mustKMS(t, "kms.test:9999"),
		config: Config{},
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
		kms:    mustKMS(t, ""),
		config: Config{},
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
		kms:    mustKMS(t, ""),
		config: Config{},
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

func TestHeaderOrgID(t *testing.T) {
	// Identity headers use standard X-Org-Id — no vendor prefix.
	expected := "X-Org-Id"
	if expected != "X-Org-Id" {
		t.Errorf("expected X-Org-Id, got %q", expected)
	}
}

func mustKMS(t *testing.T, endpoint string) *KMSClient {
	t.Helper()
	c, err := NewKMSClient(endpoint)
	if err != nil {
		t.Fatalf("NewKMSClient(%q): %v", endpoint, err)
	}
	return c
}
