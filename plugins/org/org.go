// OrgService provides per-org configuration, credential resolution, and
// customer identity management. Registered in app.Store() as "org" so
// Base Functions (Goja JS) can call methods directly:
//
//	var org = $app.store().get("org")
//	var creds = org.getCreds(orgId, "commerce")
//	var config = org.getConfig(orgId)
//	var customer = org.getCustomer(orgId, userId)
package org

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/hanzoai/base/core"
)

const orgCacheTTL = 5 * time.Minute

// OrgService provides per-org configuration, credential resolution, and
// customer identity management.
type OrgService struct {
	app    core.App
	kms    *KMSClient
	config Config

	configCache sync.Map // orgId -> *orgConfigEntry
	credsCache  sync.Map // "orgId/provider" -> *credsCacheEntry
}

type orgConfigEntry struct {
	data    map[string]any
	expires time.Time
}

type credsCacheEntry struct {
	creds   map[string]string
	expires time.Time
}

// GetConfig returns the org_configs record for an org. Cached 5min.
func (s *OrgService) GetConfig(orgId string) map[string]any {
	if orgId == "" {
		return nil
	}

	// Check cache.
	if v, ok := s.configCache.Load(orgId); ok {
		entry := v.(*orgConfigEntry)
		if time.Now().Before(entry.expires) {
			return entry.data
		}
		s.configCache.Delete(orgId)
	}

	// Query the collection.
	record, err := s.app.FindFirstRecordByFilter(
		collectionOrgConfigs,
		"org_id = {:orgId}",
		map[string]any{"orgId": orgId},
	)
	if err != nil {
		s.app.Logger().Debug("org config not found",
			"org_id", orgId,
			"error", err.Error(),
		)
		return nil
	}

	data := map[string]any{
		"id":             record.Id,
		"org_id":         record.GetString("org_id"),
		"display_name":   record.GetString("display_name"),
		"status":         record.GetString("status"),
		"kms_project_id": record.GetString("kms_project_id"),
		"fee_schedule":   record.Get("fee_schedule"),
		"features":       record.Get("features"),
		"providers":      record.Get("providers"),
		"chain_config":   record.Get("chain_config"),
		"metadata":       record.Get("metadata"),
	}

	s.configCache.Store(orgId, &orgConfigEntry{
		data:    data,
		expires: time.Now().Add(orgCacheTTL),
	})

	return data
}

// GetCreds fetches per-org credentials from KMS.
// Path convention: /orgs/{orgId}/{provider}/{key}
// Returns map like {"api_key": "...", "api_secret": "...", "base_url": "..."}
// Cached 5min per (orgId, provider) pair.
//
// An org that has not configured a provider has no credentials for it, and that
// is the answer. There used to be a fallback that read
// os.Getenv(PROVIDER + "_API_KEY") when KMS held no row — and `provider` is a
// path segment the caller writes, so any org could name openai, anthropic or
// github and be handed the DEPLOYMENT's key: the process environment, which is
// where the KMSSecret CRDs put the platform's own secrets. A tenant asked for
// its own credentials and got the operator's. The read is KMS or nothing.
func (s *OrgService) GetCreds(orgId, provider string) map[string]string {
	// A provider names ONE folder under the org, and it arrives as one segment
	// of an address. A caller that writes a separator into it is naming a
	// coordinate rather than a provider, and there is no provider by that name.
	if orgId == "" || !segment(provider) {
		return nil
	}

	cacheKey := orgId + "/" + provider

	// Check cache.
	if v, ok := s.credsCache.Load(cacheKey); ok {
		entry := v.(*credsCacheEntry)
		if time.Now().Before(entry.expires) {
			return entry.creds
		}
		s.credsCache.Delete(cacheKey)
	}

	creds := s.fetchCredsFromKMS(orgId, provider)

	if creds != nil {
		s.credsCache.Store(cacheKey, &credsCacheEntry{
			creds:   creds,
			expires: time.Now().Add(orgCacheTTL),
		})
	}

	return creds
}

// fetchCredsFromKMS tries to fetch credentials from KMS for known key names.
func (s *OrgService) fetchCredsFromKMS(orgId, provider string) map[string]string {
	if !s.kms.configured() {
		return nil
	}

	keys := []string{"api_key", "api_secret", "base_url", "webhook_secret"}
	creds := make(map[string]string)

	for _, key := range keys {
		secretPath := provider + "/" + key
		val, err := s.kms.GetSecret(orgId, secretPath)
		if err != nil {
			continue
		}
		if val != "" {
			creds[key] = val
		}
	}

	if len(creds) == 0 {
		return nil
	}
	return creds
}

// SetCreds stores credentials in KMS for an org+provider.
func (s *OrgService) SetCreds(orgId, provider string, creds map[string]string) error {
	if orgId == "" {
		return fmt.Errorf("org: orgId is required")
	}
	// The provider and each key name one folder and one secret in it, so each
	// is one segment. A separator in either names a coordinate of the caller's
	// choosing instead.
	if !segment(provider) {
		return fmt.Errorf("%w: provider %q", ErrName, provider)
	}
	for key := range creds {
		if !segment(key) {
			return fmt.Errorf("%w: %q", ErrName, key)
		}
	}
	if !s.kms.configured() {
		return fmt.Errorf("org: KMS not configured")
	}

	for key, val := range creds {
		secretPath := provider + "/" + key
		if err := s.kms.SetSecret(orgId, secretPath, val); err != nil {
			return fmt.Errorf("org: set cred %s/%s: %w", provider, key, err)
		}
	}

	// Invalidate cache for this org+provider.
	s.credsCache.Delete(orgId + "/" + provider)

	return nil
}

// InvalidateCreds clears the credential cache for an org (all providers).
func (s *OrgService) InvalidateCreds(orgId string) {
	s.credsCache.Range(func(key, _ any) bool {
		k := key.(string)
		if strings.HasPrefix(k, orgId+"/") {
			s.credsCache.Delete(key)
		}
		return true
	})
}

// customer is the org_customers row for (orgId, userId), the one query that
// reads it.
func (s *OrgService) customer(orgId, userId string) (*core.Record, error) {
	return s.app.FindFirstRecordByFilter(
		collectionOrgCustomers,
		"org_id = {:orgId} && user_id = {:userId}",
		map[string]any{"orgId": orgId, "userId": userId},
	)
}

// GetCustomer looks up the org_customers record for (orgId, userId).
func (s *OrgService) GetCustomer(orgId, userId string) map[string]any {
	if orgId == "" || userId == "" {
		return nil
	}

	record, err := s.customer(orgId, userId)
	if err != nil {
		return nil
	}

	return customerRecordToMap(record)
}

// BindComplianceApp records that a compliance application belongs to one org's
// user, so that a later read of it can be answered.
//
// The vendor's application id is a bare string that arrives in a URL and says
// nothing about who created it, so without this there is no question to ask and
// every caller reaches every application.
func (s *OrgService) BindComplianceApp(orgId, userId, applicationId string) error {
	if orgId == "" || userId == "" || applicationId == "" {
		return fmt.Errorf("org: orgId, userId and applicationId are required")
	}

	if _, err := s.GetOrProvisionCustomer(orgId, userId); err != nil {
		return err
	}

	record, err := s.customer(orgId, userId)
	if err != nil {
		return fmt.Errorf("org: customer for org=%s user=%s: %w", orgId, userId, err)
	}

	record.Set(fieldComplianceApp, applicationId)

	return s.app.Save(record)
}

// ComplianceApp reports which of an org's users holds a compliance
// application. An org that holds no such application answers false, which is
// what a caller naming another org's application gets.
func (s *OrgService) ComplianceApp(orgId, applicationId string) (string, bool) {
	if orgId == "" || applicationId == "" {
		return "", false
	}

	record, err := s.app.FindFirstRecordByFilter(
		collectionOrgCustomers,
		"org_id = {:orgId} && "+fieldComplianceApp+" = {:app}",
		map[string]any{"orgId": orgId, "app": applicationId},
	)
	if err != nil {
		return "", false
	}

	return record.GetString("user_id"), true
}

// ProvisionCustomer creates a new customer identity for a user in an org.
// Generates a sequential customer_id, creates the record.
func (s *OrgService) ProvisionCustomer(orgId, userId string, opts map[string]any) (map[string]any, error) {
	if orgId == "" || userId == "" {
		return nil, fmt.Errorf("org: orgId and userId are required")
	}

	// Check if already exists.
	existing := s.GetCustomer(orgId, userId)
	if existing != nil {
		return nil, fmt.Errorf("org: customer already exists for org=%s user=%s", orgId, userId)
	}

	// Generate sequential customer ID within org.
	custId, err := s.nextCustomerId(orgId)
	if err != nil {
		return nil, fmt.Errorf("org: generate customer_id: %w", err)
	}

	col, err := s.app.FindCollectionByNameOrId(collectionOrgCustomers)
	if err != nil {
		return nil, fmt.Errorf("org: %s collection not found: %w", collectionOrgCustomers, err)
	}

	record := core.NewRecord(col)
	record.Set("org_id", orgId)
	record.Set("user_id", userId)
	record.Set("customer_id", custId)
	record.Set("status", "active")

	// Apply optional fields from opts.
	if opts != nil {
		for _, field := range []string{"display_name", "broker_account_id", "commerce_customer_id", "mpc_vault_id"} {
			if v, ok := opts[field]; ok {
				record.Set(field, v)
			}
		}
		if v, ok := opts["metadata"]; ok {
			record.Set("metadata", v)
		}
		if v, ok := opts["status"]; ok {
			record.Set("status", v)
		}
	}

	if err := s.app.Save(record); err != nil {
		return nil, fmt.Errorf("org: save customer: %w", err)
	}

	s.app.Logger().Info("provisioned org customer",
		"org_id", orgId,
		"user_id", userId,
		"customer_id", custId,
	)

	return customerRecordToMap(record), nil
}

// GetOrProvisionCustomer returns existing customer or creates one.
func (s *OrgService) GetOrProvisionCustomer(orgId, userId string) (map[string]any, error) {
	existing := s.GetCustomer(orgId, userId)
	if existing != nil {
		return existing, nil
	}
	return s.ProvisionCustomer(orgId, userId, nil)
}

// nextCustomerId generates the next sequential customer ID for an org.
// Format: zero-padded 6 digits like "000001".
func (s *OrgService) nextCustomerId(orgId string) (string, error) {
	// Find the current max customer_id for this org.
	records, err := s.app.FindRecordsByFilter(
		collectionOrgCustomers,
		"org_id = {:orgId}",
		"-customer_id",
		1, 0,
		map[string]any{"orgId": orgId},
	)
	if err != nil || len(records) == 0 {
		return "000001", nil
	}

	maxId := records[0].GetString("customer_id")
	var num int
	_, _ = fmt.Sscanf(maxId, "%d", &num)
	num++

	return fmt.Sprintf("%06d", num), nil
}

// customerRecordToMap converts a customer record to a plain map.
func customerRecordToMap(record *core.Record) map[string]any {
	return map[string]any{
		"id":                   record.Id,
		"org_id":               record.GetString("org_id"),
		"user_id":              record.GetString("user_id"),
		"customer_id":          record.GetString("customer_id"),
		"status":               record.GetString("status"),
		"display_name":         record.GetString("display_name"),
		"broker_account_id":    record.GetString("broker_account_id"),
		"commerce_customer_id": record.GetString("commerce_customer_id"),
		"mpc_vault_id":         record.GetString("mpc_vault_id"),
		fieldComplianceApp:     record.GetString(fieldComplianceApp),
		"metadata":             record.Get("metadata"),
	}
}
