package org

import (
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/hanzoai/base/core"
	"github.com/hanzoai/cek"
	"github.com/hanzoai/namespace"
)

// OrgDB manages per-org AND per-user SQLite databases.
//
// Directory layout:
//
//	{DataDir}/orgs/{orgSlug}/org.db              ← org-level shared data
//	{DataDir}/orgs/{orgSlug}/users/{userId}/data.db  ← per-user PII + keys
//
// Each file gets its own key, derived by github.com/hanzoai/cek from the master
// key and the namespace naming whose data it is:
//
//	org DEK  = cek.DeriveKey(master, org/{orgSlug},           "org")
//	user DEK = cek.DeriveKey(master, org/{orgSlug}/{userId},  "user")
//
// Zero data commingling — org data and user PII live in separate files under
// separate keys. TestOrgDB_DEK_IsCEKDerivation holds these two lines to the
// code, because a comment about a derivation cannot fail when the derivation
// drifts and this package has been wrong that way before.
type OrgDB struct {
	app       core.App
	masterKey string

	mu  sync.RWMutex
	dbs map[string]string // key → db path
}

func NewOrgDB(app core.App, masterKey string) *OrgDB {
	return &OrgDB{
		app:       app,
		masterKey: masterKey,
		dbs:       make(map[string]string),
	}
}

// OrgsDir returns the base directory for all org databases.
func (t *OrgDB) OrgsDir() string {
	return filepath.Join(t.app.DataDir(), "orgs")
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
func (t *OrgDB) OrgDir(orgSlug string) string {
	if err := validateSlug(orgSlug); err != nil {
		return filepath.Join(t.OrgsDir(), "_invalid")
	}
	return filepath.Join(t.OrgsDir(), orgSlug)
}

// OrgDBPath returns the org-level SQLite database path.
func (t *OrgDB) OrgDBPath(orgSlug string) string {
	return filepath.Join(t.OrgDir(orgSlug), "org.db")
}

// The subsystem each key belongs to. One store is one subsystem, so these name
// what the file holds rather than who it is for — who is the namespace.
const (
	orgDEKSubsystem  = "org"
	userDEKSubsystem = "user"
)

// dek derives ns's key for subsystem with github.com/hanzoai/cek, the one key
// derivation in the platform.
//
// An empty master key is dev mode: no key, and no encryption, so the caller
// gets "" and no error. A master key of any OTHER wrong length is a
// misconfiguration and is reported, because a key that quietly becomes no key
// is how data ends up in the clear while the deployment believes otherwise.
func (t *OrgDB) dek(ns namespace.Namespace, subsystem string) (string, error) {
	if t.masterKey == "" {
		return "", nil
	}
	key, err := cek.DeriveKey([]byte(t.masterKey), ns, subsystem)
	if err != nil {
		return "", fmt.Errorf("platform: derive %s key: %w", subsystem, err)
	}
	return hex.EncodeToString(key), nil
}

// OrgDEK derives the org-level data encryption key.
func (t *OrgDB) OrgDEK(orgSlug string) (string, error) {
	ns, err := namespace.Org(orgSlug)
	if err != nil {
		return "", err
	}
	return t.dek(ns, orgDEKSubsystem)
}

// ProvisionOrg creates the org directory and org-level database.
func (t *OrgDB) ProvisionOrg(orgSlug string) (string, error) {
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
func (t *OrgDB) UserDir(orgSlug, userId string) string {
	if err := validateSlug(userId); err != nil {
		return filepath.Join(t.OrgDir(orgSlug), "users", "_invalid")
	}
	return filepath.Join(t.OrgDir(orgSlug), "users", userId)
}

// UserDBPath returns the per-user SQLite database path.
func (t *OrgDB) UserDBPath(orgSlug, userId string) string {
	return filepath.Join(t.UserDir(orgSlug, userId), "data.db")
}

// UserDEK derives the per-user data encryption key. User PII gets its own key,
// separate from the org's.
//
// The org rides in the namespace alongside the user, so acme/alice and
// globex/alice stay different keys even where two orgs use the same user id.
//
// namespace.Of is the door here, rather than OrgProject, because these ids are
// already slugs by this package's own contract — validateSlug, and the org's
// directory is named with the raw slug. Sanitizing here and not there would key
// a file by one rendering of a name and place it by another; erroring instead
// keeps the key and the path reading the same string.
func (t *OrgDB) UserDEK(orgSlug, userId string) (string, error) {
	ns, err := namespace.Of(orgSlug + "/" + userId)
	if err != nil {
		return "", err
	}
	return t.dek(ns, userDEKSubsystem)
}

// ProvisionUser creates the per-user directory and database.
func (t *OrgDB) ProvisionUser(orgSlug, userId string) (string, error) {
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
func (t *OrgDB) GetOrgDBPath(orgSlug string) (string, bool) {
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
func (t *OrgDB) GetUserDBPath(orgSlug, userId string) (string, bool) {
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
func (t *OrgDB) DeleteUser(orgSlug, userId string) error {
	t.mu.Lock()
	delete(t.dbs, "user:"+orgSlug+":"+userId)
	t.mu.Unlock()
	return os.RemoveAll(t.UserDir(orgSlug, userId))
}

// DeleteOrg removes an org's entire directory (including all user databases).
func (t *OrgDB) DeleteOrg(orgSlug string) error {
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
func (t *OrgDB) ListOrgs() ([]string, error) {
	entries, err := os.ReadDir(t.OrgsDir())
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
func (t *OrgDB) ListUsers(orgSlug string) ([]string, error) {
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
