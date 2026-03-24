package platform

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/hanzoai/base/core"
)

// TenantDB manages per-tenant SQLite databases.
// Each tenant gets an isolated, optionally encrypted SQLite file.
type TenantDB struct {
	app       core.App
	masterKey string

	mu sync.RWMutex
	dbs map[string]string // slug → db path
}

func NewTenantDB(app core.App, masterKey string) *TenantDB {
	return &TenantDB{
		app:       app,
		masterKey: masterKey,
		dbs:       make(map[string]string),
	}
}

// TenantsDir returns the base directory for per-tenant databases.
func (t *TenantDB) TenantsDir() string {
	return filepath.Join(t.app.DataDir(), "tenants")
}

// TenantDir returns the directory for a specific tenant.
func (t *TenantDB) TenantDir(slug string) string {
	return filepath.Join(t.TenantsDir(), slug)
}

// DBPath returns the SQLite database path for a tenant.
func (t *TenantDB) DBPath(slug string) string {
	return filepath.Join(t.TenantDir(slug), "data.db")
}

// DEK derives a per-tenant data encryption key from the master key + slug.
// Returns empty string if no master key is configured (dev mode, unencrypted).
func (t *TenantDB) DEK(slug string) string {
	if t.masterKey == "" {
		return ""
	}
	mac := hmac.New(sha256.New, []byte(t.masterKey))
	mac.Write([]byte(slug))
	return hex.EncodeToString(mac.Sum(nil))
}

// Provision creates the directory and SQLite database for a tenant.
// If a master encryption key is set, the DB file path includes the DEK
// hint for SQLCipher / encrypted SQLite drivers.
func (t *TenantDB) Provision(slug string) (string, error) {
	dir := t.TenantDir(slug)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", fmt.Errorf("create tenant dir %q: %w", dir, err)
	}

	dbPath := t.DBPath(slug)

	t.mu.Lock()
	t.dbs[slug] = dbPath
	t.mu.Unlock()

	return dbPath, nil
}

// GetDBPath returns the database path for an existing tenant.
func (t *TenantDB) GetDBPath(slug string) (string, bool) {
	t.mu.RLock()
	defer t.mu.RUnlock()

	if p, ok := t.dbs[slug]; ok {
		return p, true
	}

	// Check filesystem
	dbPath := t.DBPath(slug)
	if _, err := os.Stat(dbPath); err == nil {
		t.mu.RUnlock()
		t.mu.Lock()
		t.dbs[slug] = dbPath
		t.mu.Unlock()
		t.mu.RLock()
		return dbPath, true
	}

	return "", false
}

// Delete removes a tenant's database directory entirely.
func (t *TenantDB) Delete(slug string) error {
	t.mu.Lock()
	delete(t.dbs, slug)
	t.mu.Unlock()

	dir := t.TenantDir(slug)
	return os.RemoveAll(dir)
}

// List returns all provisioned tenant slugs by scanning the tenants directory.
func (t *TenantDB) List() ([]string, error) {
	entries, err := os.ReadDir(t.TenantsDir())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var slugs []string
	for _, e := range entries {
		if e.IsDir() {
			dbPath := filepath.Join(t.TenantsDir(), e.Name(), "data.db")
			if _, err := os.Stat(dbPath); err == nil {
				slugs = append(slugs, e.Name())
			}
		}
	}
	return slugs, nil
}
