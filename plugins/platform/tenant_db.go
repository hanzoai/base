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

// TenantDB manages per-org AND per-user SQLite databases.
//
// Directory layout:
//
//	{DataDir}/tenants/{orgSlug}/org.db              ← org-level shared data
//	{DataDir}/tenants/{orgSlug}/users/{userId}/data.db  ← per-user PII + keys
//
// Each file is independently encrypted with a unique DEK:
//
//	org DEK  = HMAC-SHA256(masterKey, orgSlug)
//	user DEK = HMAC-SHA256(masterKey, orgSlug + ":" + userId)
//
// Zero data commingling — org data, user PII, and keys are all in separate
// files with separate encryption keys.
type TenantDB struct {
	app       core.App
	masterKey string

	mu  sync.RWMutex
	dbs map[string]string // key → db path
}

func NewTenantDB(app core.App, masterKey string) *TenantDB {
	return &TenantDB{
		app:       app,
		masterKey: masterKey,
		dbs:       make(map[string]string),
	}
}

// TenantsDir returns the base directory for all tenant databases.
func (t *TenantDB) TenantsDir() string {
	return filepath.Join(t.app.DataDir(), "tenants")
}

// validateSlug rejects slugs containing path traversal characters.
func validateSlug(s string) error {
	if s == "" {
		return fmt.Errorf("slug cannot be empty")
	}
	for _, c := range s {
		if !((c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-' || c == '_') {
			return fmt.Errorf("slug contains invalid character %q", c)
		}
	}
	if s == "." || s == ".." {
		return fmt.Errorf("slug cannot be . or ..")
	}
	return nil
}

// --- Org-level database ---

// OrgDir returns the directory for an org. Validates slug to prevent path traversal.
func (t *TenantDB) OrgDir(orgSlug string) string {
	if err := validateSlug(orgSlug); err != nil {
		return filepath.Join(t.TenantsDir(), "_invalid")
	}
	return filepath.Join(t.TenantsDir(), orgSlug)
}

// OrgDBPath returns the org-level SQLite database path.
func (t *TenantDB) OrgDBPath(orgSlug string) string {
	return filepath.Join(t.OrgDir(orgSlug), "org.db")
}

// OrgDEK derives the org-level data encryption key.
func (t *TenantDB) OrgDEK(orgSlug string) string {
	if t.masterKey == "" {
		return ""
	}
	mac := hmac.New(sha256.New, []byte(t.masterKey))
	mac.Write([]byte(orgSlug))
	return hex.EncodeToString(mac.Sum(nil))
}

// ProvisionOrg creates the org directory and org-level database.
func (t *TenantDB) ProvisionOrg(orgSlug string) (string, error) {
	if err := validateSlug(orgSlug); err != nil {
		return "", fmt.Errorf("invalid org slug: %w", err)
	}
	dir := t.OrgDir(orgSlug)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", fmt.Errorf("create org dir %q: %w", dir, err)
	}
	// Create users subdirectory
	if err := os.MkdirAll(filepath.Join(dir, "users"), 0700); err != nil {
		return "", fmt.Errorf("create org users dir: %w", err)
	}

	dbPath := t.OrgDBPath(orgSlug)

	t.mu.Lock()
	t.dbs["org:"+orgSlug] = dbPath
	t.mu.Unlock()

	return dbPath, nil
}

// --- Per-user database ---

// UserDir returns the directory for a specific user within an org.
func (t *TenantDB) UserDir(orgSlug, userId string) string {
	if err := validateSlug(userId); err != nil {
		return filepath.Join(t.OrgDir(orgSlug), "users", "_invalid")
	}
	return filepath.Join(t.OrgDir(orgSlug), "users", userId)
}

// UserDBPath returns the per-user SQLite database path.
func (t *TenantDB) UserDBPath(orgSlug, userId string) string {
	return filepath.Join(t.UserDir(orgSlug, userId), "data.db")
}

// UserDEK derives the per-user data encryption key.
// Different from the org DEK — user PII is encrypted with a user-specific key.
func (t *TenantDB) UserDEK(orgSlug, userId string) string {
	if t.masterKey == "" {
		return ""
	}
	mac := hmac.New(sha256.New, []byte(t.masterKey))
	mac.Write([]byte(orgSlug + ":" + userId))
	return hex.EncodeToString(mac.Sum(nil))
}

// ProvisionUser creates the per-user directory and database.
func (t *TenantDB) ProvisionUser(orgSlug, userId string) (string, error) {
	if err := validateSlug(orgSlug); err != nil {
		return "", fmt.Errorf("invalid org slug: %w", err)
	}
	if err := validateSlug(userId); err != nil {
		return "", fmt.Errorf("invalid user id: %w", err)
	}
	dir := t.UserDir(orgSlug, userId)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", fmt.Errorf("create user dir %q: %w", dir, err)
	}

	dbPath := t.UserDBPath(orgSlug, userId)

	t.mu.Lock()
	t.dbs["user:"+orgSlug+":"+userId] = dbPath
	t.mu.Unlock()

	return dbPath, nil
}

// --- Lookup ---

// GetOrgDBPath returns the database path for an existing org.
func (t *TenantDB) GetOrgDBPath(orgSlug string) (string, bool) {
	t.mu.RLock()
	if p, ok := t.dbs["org:"+orgSlug]; ok {
		t.mu.RUnlock()
		return p, true
	}
	t.mu.RUnlock()

	dbPath := t.OrgDBPath(orgSlug)
	if _, err := os.Stat(dbPath); err == nil {
		t.mu.Lock()
		t.dbs["org:"+orgSlug] = dbPath
		t.mu.Unlock()
		return dbPath, true
	}
	return "", false
}

// GetUserDBPath returns the database path for an existing user.
func (t *TenantDB) GetUserDBPath(orgSlug, userId string) (string, bool) {
	key := "user:" + orgSlug + ":" + userId
	t.mu.RLock()
	if p, ok := t.dbs[key]; ok {
		t.mu.RUnlock()
		return p, true
	}
	t.mu.RUnlock()

	dbPath := t.UserDBPath(orgSlug, userId)
	if _, err := os.Stat(dbPath); err == nil {
		t.mu.Lock()
		t.dbs[key] = dbPath
		t.mu.Unlock()
		return dbPath, true
	}
	return "", false
}

// --- Lifecycle ---

// DeleteUser removes a user's database directory.
func (t *TenantDB) DeleteUser(orgSlug, userId string) error {
	t.mu.Lock()
	delete(t.dbs, "user:"+orgSlug+":"+userId)
	t.mu.Unlock()
	return os.RemoveAll(t.UserDir(orgSlug, userId))
}

// DeleteOrg removes an org's entire directory (including all user databases).
func (t *TenantDB) DeleteOrg(orgSlug string) error {
	t.mu.Lock()
	// Remove all cached paths for this org
	for k := range t.dbs {
		if len(k) > 4 && k[:4] == "org:" && k[4:] == orgSlug {
			delete(t.dbs, k)
		}
		if len(k) > 5+len(orgSlug) && k[:5+len(orgSlug)] == "user:"+orgSlug+":" {
			delete(t.dbs, k)
		}
	}
	t.mu.Unlock()
	return os.RemoveAll(t.OrgDir(orgSlug))
}

// ListOrgs returns all provisioned org slugs.
func (t *TenantDB) ListOrgs() ([]string, error) {
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
			if _, err := os.Stat(t.OrgDBPath(e.Name())); err == nil {
				slugs = append(slugs, e.Name())
			}
		}
	}
	return slugs, nil
}

// ListUsers returns all provisioned user IDs within an org.
func (t *TenantDB) ListUsers(orgSlug string) ([]string, error) {
	usersDir := filepath.Join(t.OrgDir(orgSlug), "users")
	entries, err := os.ReadDir(usersDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var ids []string
	for _, e := range entries {
		if e.IsDir() {
			if _, err := os.Stat(t.UserDBPath(orgSlug, e.Name())); err == nil {
				ids = append(ids, e.Name())
			}
		}
	}
	return ids, nil
}
