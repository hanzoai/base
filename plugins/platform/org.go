// OrgService provides per-org configuration, credential resolution, and
// customer identity management. Registered in app.Store() as "org" so
// Base Functions (Goja JS) can call methods directly:
//
//	var org = $app.store().get("org")
//	var creds = org.getCreds(orgId, "commerce")
//	var config = org.getConfig(orgId)
//	var customer = org.getCustomer(orgId, userId)
package platform

import (
	"fmt"
	"log/slog"
	"os"
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
	config PlatformConfig

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
			slog.String("org_id", orgId),
			slog.String("error", err.Error()),
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
// Falls back to env vars if KMS not configured or secret not found (dev mode).
func (s *OrgService) GetCreds(orgId, provider string) map[string]string {
	if orgId == "" || provider == "" {
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
	if creds == nil {
		creds = s.fetchCredsFromEnv(provider)
	}

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
	if s.kms == nil || s.kms.baseURL == "" {
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

// fetchCredsFromEnv falls back to environment variables.
// Pattern: {PROVIDER}_API_KEY, {PROVIDER}_API_SECRET, etc.
func (s *OrgService) fetchCredsFromEnv(provider string) map[string]string {
	prefix := strings.ToUpper(provider) + "_"
	keys := []string{"API_KEY", "API_SECRET", "BASE_URL", "WEBHOOK_SECRET"}
	creds := make(map[string]string)

	for _, key := range keys {
		val := os.Getenv(prefix + key)
		if val != "" {
			creds[strings.ToLower(key)] = val
		}
	}

	if len(creds) == 0 {
		return nil
	}
	return creds
}

// SetCreds stores credentials in KMS for an org+provider.
func (s *OrgService) SetCreds(orgId, provider string, creds map[string]string) error {
	if orgId == "" || provider == "" {
		return fmt.Errorf("org: orgId and provider are required")
	}
	if s.kms == nil || s.kms.baseURL == "" {
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

// GetCustomer looks up the org_customers record for (orgId, userId).
func (s *OrgService) GetCustomer(orgId, userId string) map[string]any {
	if orgId == "" || userId == "" {
		return nil
	}

	record, err := s.app.FindFirstRecordByFilter(
		collectionOrgCustomers,
		"org_id = {:orgId} && user_id = {:userId}",
		map[string]any{"orgId": orgId, "userId": userId},
	)
	if err != nil {
		return nil
	}

	return customerRecordToMap(record)
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
		slog.String("org_id", orgId),
		slog.String("user_id", userId),
		slog.String("customer_id", custId),
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
		"id":                  record.Id,
		"org_id":              record.GetString("org_id"),
		"user_id":             record.GetString("user_id"),
		"customer_id":         record.GetString("customer_id"),
		"status":              record.GetString("status"),
		"display_name":        record.GetString("display_name"),
		"broker_account_id":   record.GetString("broker_account_id"),
		"commerce_customer_id":  record.GetString("commerce_customer_id"),
		"mpc_vault_id":        record.GetString("mpc_vault_id"),
		"metadata":            record.Get("metadata"),
	}
}
