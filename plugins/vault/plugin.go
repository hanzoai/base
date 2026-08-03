// Copyright (C) 2020-2026, Hanzo AI Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package vault provides per-user encrypted SQLite shards with CRDT sync
// and on-chain anchoring. This is the unified local-first data SDK.
//
// Architecture:
//
//	Cloud HSM / K-Chain ML-KEM
//	  └── Master key (unwrapped via HSM or threshold decryption)
//	        └── User DEK = cek.DeriveKey(master, org/{orgID}/{userID}, "vault")
//	              └── SQLite shard (AES-256-GCM encrypted)
//	                    └── CRDT sync via ZAP (conflict-free merge)
//	                          └── Merkle root anchored to chain
//
// Usage:
//
//	vault.MustRegister(app, vault.Config{
//	    DataDir:    "/data/vaults",
//	    MasterKey:  masterKeyBytes, // from HSM or K-Chain
//	    OrgID:      "my-org",
//	    ChainRPC:   "http://localhost:9650/ext/bc/I", // optional anchoring
//	    SyncEnabled: true,
//	})
//
// Each authenticated user gets their own encrypted SQLite file.
// Reads/writes are instant (local). CRDT syncs in background.
// Chain stores merkle roots, never row data.
package vault

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"

	"github.com/hanzoai/base/core"
	"github.com/hanzoai/base/tools/hook"
	"github.com/hanzoai/cek"
	"github.com/hanzoai/namespace"
	luxlog "github.com/luxfi/log"
)

// Config configures the vault plugin.
type Config struct {
	Enabled     bool   `json:"enabled"`
	DataDir     string `json:"dataDir"`     // directory for per-user SQLite shards
	OrgID       string `json:"orgId"`       // organization identifier
	MasterKey   []byte `json:"-"`           // 32-byte master KEK (from HSM/K-Chain)
	ChainRPC    string `json:"chainRpc"`    // optional: I-Chain RPC for merkle anchoring
	SyncEnabled bool   `json:"syncEnabled"` // enable CRDT sync via ZAP
	ZAPPort     int    `json:"zapPort"`     // ZAP listen port for sync (default 9999)
}

func (c Config) validate() error {
	if c.DataDir == "" {
		return fmt.Errorf("vault: dataDir is required")
	}
	if c.OrgID == "" {
		return fmt.Errorf("vault: orgId is required")
	}
	if len(c.MasterKey) != 32 {
		return fmt.Errorf("vault: masterKey must be 32 bytes, got %d", len(c.MasterKey))
	}
	return nil
}

// MustRegister registers the vault plugin and panics on error.
func MustRegister(app core.App, config Config) {
	if err := Register(app, config); err != nil {
		panic(err)
	}
}

// Register registers the vault plugin with a Base app.
func Register(app core.App, config Config) error {
	if !config.Enabled {
		return nil
	}
	if err := config.validate(); err != nil {
		return err
	}
	if config.ZAPPort == 0 {
		config.ZAPPort = 9999
	}

	p := &plugin{
		app:    app,
		config: config,
		shards: make(map[string]*UserShard),
		logger: luxlog.New("component", "vault"),
	}

	if err := os.MkdirAll(config.DataDir, 0700); err != nil {
		return fmt.Errorf("vault: create data dir: %w", err)
	}

	// Register routes on serve.
	app.OnServe().Bind(&hook.Handler[*core.ServeEvent]{
		Id: "__vault__",
		Func: func(e *core.ServeEvent) error {
			p.registerRoutes(e.Router)
			return e.Next()
		},
	})

	// Close all shards on terminate.
	app.OnTerminate().Bind(&hook.Handler[*core.TerminateEvent]{
		Id: "__vaultCleanup__",
		Func: func(e *core.TerminateEvent) error {
			p.closeAll()
			return e.Next()
		},
	})

	return nil
}

type plugin struct {
	app    core.App
	config Config
	shards map[string]*UserShard
	mu     sync.RWMutex
	logger luxlog.Logger
}

// UserShard is an encrypted per-user SQLite database.
type UserShard struct {
	UserID string
	Path   string // filesystem path to the .db file
	DEK    []byte // 32-byte data encryption key for this user
}

// GetShard returns (or creates) the encrypted SQLite shard for a user.
func (p *plugin) GetShard(userID string) (*UserShard, error) {
	p.mu.RLock()
	if shard, ok := p.shards[userID]; ok {
		p.mu.RUnlock()
		return shard, nil
	}
	p.mu.RUnlock()

	p.mu.Lock()
	defer p.mu.Unlock()

	// Double-check after acquiring write lock.
	if shard, ok := p.shards[userID]; ok {
		return shard, nil
	}

	dek, err := vaultKey(p.config.MasterKey, p.config.OrgID, userID, userSubsystem)
	if err != nil {
		return nil, err
	}
	dbPath := filepath.Join(p.config.DataDir, p.config.OrgID, userID+".db")

	if err := os.MkdirAll(filepath.Dir(dbPath), 0700); err != nil {
		return nil, fmt.Errorf("vault: create user dir: %w", err)
	}

	shard := &UserShard{
		UserID: userID,
		Path:   dbPath,
		DEK:    dek,
	}

	p.shards[userID] = shard
	p.logger.Info("vault shard opened", "user", userID, "path", dbPath)
	return shard, nil
}

func (p *plugin) closeAll() {
	p.mu.Lock()
	defer p.mu.Unlock()
	for id, shard := range p.shards {
		clear(shard.DEK)
		delete(p.shards, id)
	}
	p.logger.Info("all vault shards closed")
}

// ─── Key Derivation ──────────────────────────────────────────────────────────
//
// One derivation, github.com/hanzoai/cek, from the master key and the namespace
// naming whose data the shard holds:
//
//	user shard   = cek.DeriveKey(master, org/{orgID}/{userID},  "vault")
//	shared vault = cek.DeriveKey(master, org/{orgID}/{vaultID}, "vault-shared")
//
// There is no intermediate org KEK any more. It was there to be rotatable
// without re-encrypting user data, nothing ever rotated it, and what it did in
// practice was give this plugin a second key derivation of its own. The org is
// in the namespace instead, so two orgs that use the same user id still get
// different keys — which is the property the hierarchy was really providing.
//
// Properties that remain:
//   - Compromising one user's DEK does not reveal any other user's DEK
//   - The master key never touches disk (HSM or threshold-reconstructed)
//   - Each shard is independently encrypted — no shared ciphertext
const (
	userSubsystem   = "vault"
	sharedSubsystem = "vault-shared"
)

// vaultKey derives the key for one member of an org — a user, or a shared vault
// — from the master key. name rides in the namespace with orgID rather than in
// the subsystem, so the key is bound to the org that owns it.
//
// OrgProject is the door here, rather than namespace.Of, because these ids are
// DIDs ("did:lux:user:alice") chosen elsewhere, and a namespace segment is
// [a-z0-9][a-z0-9_-]* — a DID's colons are not legal in one. OrgProject folds
// each half through Sanitize, which is INJECTIVE, so two distinct DIDs cannot
// land on one key however they are spelled. A lossy fold here would be two
// users sharing a vault.
func vaultKey(master []byte, orgID, name, subsystem string) ([]byte, error) {
	ns, err := namespace.OrgProject(orgID, name)
	if err != nil {
		return nil, fmt.Errorf("vault: %w", err)
	}
	key, err := cek.DeriveKey(master, ns, subsystem)
	if err != nil {
		return nil, fmt.Errorf("vault: derive %s key: %w", subsystem, err)
	}
	return key, nil
}

// Encrypt encrypts plaintext with the user's DEK using AES-256-GCM.
func (s *UserShard) Encrypt(plaintext []byte) ([]byte, error) {
	block, err := aes.NewCipher(s.DEK)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	return gcm.Seal(nonce, nonce, plaintext, nil), nil
}

// Decrypt decrypts ciphertext (nonce-prepended) with the user's DEK.
func (s *UserShard) Decrypt(ciphertext []byte) ([]byte, error) {
	block, err := aes.NewCipher(s.DEK)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonceSize := gcm.NonceSize()
	if len(ciphertext) < nonceSize {
		return nil, fmt.Errorf("ciphertext too short")
	}
	return gcm.Open(nil, ciphertext[:nonceSize], ciphertext[nonceSize:], nil)
}
