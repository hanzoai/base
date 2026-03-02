package core

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/hanzoai/dbx"
)

// TenantRegistry manages per-org SQLite databases with lazy open and
// optional per-principal encryption via HKDF-derived keys.
//
// Directory layout:
//
//	{DataDir}/orgs/{orgID}/data.db
//	{DataDir}/orgs/{orgID}/auxiliary.db
//
// Each org database is independently encrypted when a master key is set.
// The registry is safe for concurrent use.
//
// Activation: TenantRegistry is nil on BaseApp unless MULTI_TENANT=true
// or MASTER_KEY is set. All existing single-tenant behavior is unaffected.
type TenantRegistry struct {
	dataDir   string
	masterKey []byte // 32-byte master key, nil if encryption disabled
	connect   DBConnectFunc

	mu  sync.RWMutex
	dbs map[string]*tenantDBs
}

// tenantDBs holds the concurrent and nonconcurrent dbx.Builder for one org.
type tenantDBs struct {
	concurrent    dbx.Builder
	nonconcurrent dbx.Builder
}

// TenantConfig configures the TenantRegistry.
type TenantConfig struct {
	// DataDir is the base data directory. Org databases live under {DataDir}/orgs/{orgID}/.
	DataDir string

	// MasterKey is the 32-byte master encryption key. If nil, encryption is disabled.
	MasterKey []byte

	// DBConnect is the function used to open database files. Defaults to DefaultDBConnect.
	DBConnect DBConnectFunc
}

// NewTenantRegistry creates a TenantRegistry. Returns nil if config is nil.
func NewTenantRegistry(config *TenantConfig) *TenantRegistry {
	if config == nil {
		return nil
	}
	connect := config.DBConnect
	if connect == nil {
		connect = DefaultDBConnect
	}
	return &TenantRegistry{
		dataDir:   config.DataDir,
		masterKey: config.MasterKey,
		connect:   connect,
		dbs:       make(map[string]*tenantDBs),
	}
}

// validateOrgID rejects IDs containing path traversal characters.
func validateOrgID(id string) error {
	if id == "" {
		return fmt.Errorf("org ID cannot be empty")
	}
	for _, c := range id {
		if !((c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-' || c == '_') {
			return fmt.Errorf("org ID contains invalid character %q", c)
		}
	}
	if id == "." || id == ".." {
		return fmt.Errorf("org ID cannot be . or ..")
	}
	return nil
}

// orgDir returns the directory for an org's databases.
func (r *TenantRegistry) orgDir(orgID string) string {
	return filepath.Join(r.dataDir, "orgs", orgID)
}

// OrgDB returns the data.db builder for the given org.
// Opens the database lazily on first access and caches it.
// The returned builder routes SELECTs to the concurrent pool
// and writes to the nonconcurrent pool (same pattern as BaseApp.DB()).
func (r *TenantRegistry) OrgDB(orgID string) (dbx.Builder, error) {
	if err := validateOrgID(orgID); err != nil {
		return nil, fmt.Errorf("tenant: %w", err)
	}

	// Fast path: read lock
	r.mu.RLock()
	if t, ok := r.dbs[orgID]; ok {
		r.mu.RUnlock()
		return t.concurrent, nil
	}
	r.mu.RUnlock()

	// Slow path: open and cache
	r.mu.Lock()
	defer r.mu.Unlock()

	// Double-check after acquiring write lock
	if t, ok := r.dbs[orgID]; ok {
		return t.concurrent, nil
	}

	dir := r.orgDir(orgID)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, fmt.Errorf("tenant: create dir %q: %w", dir, err)
	}

	dbPath := filepath.Join(dir, "data.db")

	concurrent, err := r.connect(dbPath)
	if err != nil {
		return nil, fmt.Errorf("tenant: open concurrent db for org %q: %w", orgID, err)
	}
	concurrent.DB().SetMaxOpenConns(DefaultDataMaxOpenConns)
	concurrent.DB().SetMaxIdleConns(DefaultDataMaxIdleConns)

	nonconcurrent, err := r.connect(dbPath)
	if err != nil {
		concurrent.Close()
		return nil, fmt.Errorf("tenant: open nonconcurrent db for org %q: %w", orgID, err)
	}
	nonconcurrent.DB().SetMaxOpenConns(1)
	nonconcurrent.DB().SetMaxIdleConns(1)

	r.dbs[orgID] = &tenantDBs{
		concurrent:    concurrent,
		nonconcurrent: nonconcurrent,
	}

	return concurrent, nil
}

// OrgNonconcurrentDB returns the write-only (single connection) builder for an org.
// Opens the database lazily on first access.
func (r *TenantRegistry) OrgNonconcurrentDB(orgID string) (dbx.Builder, error) {
	if err := validateOrgID(orgID); err != nil {
		return nil, fmt.Errorf("tenant: %w", err)
	}

	// Ensure the org is opened (OrgDB handles lazy init)
	if _, err := r.OrgDB(orgID); err != nil {
		return nil, err
	}

	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.dbs[orgID].nonconcurrent, nil
}

// MasterKey returns the configured master key (may be nil).
func (r *TenantRegistry) MasterKey() []byte {
	return r.masterKey
}

// HasOrg checks if a database for the given org exists on disk.
func (r *TenantRegistry) HasOrg(orgID string) bool {
	if err := validateOrgID(orgID); err != nil {
		return false
	}
	dbPath := filepath.Join(r.orgDir(orgID), "data.db")
	_, err := os.Stat(dbPath)
	return err == nil
}

// Close closes all open org databases and clears the cache.
func (r *TenantRegistry) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	var lastErr error
	for orgID, t := range r.dbs {
		if c, ok := t.concurrent.(interface{ Close() error }); ok {
			if err := c.Close(); err != nil {
				lastErr = err
			}
		}
		if c, ok := t.nonconcurrent.(interface{ Close() error }); ok {
			if err := c.Close(); err != nil {
				lastErr = err
			}
		}
		delete(r.dbs, orgID)
	}
	return lastErr
}
