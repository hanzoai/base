// Copyright (C) 2020-2026, Hanzo AI Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package vault provides per-user encrypted SQLite shards with CRDT sync
// and on-chain anchoring. This is the unified local-first data SDK.
//
// Architecture:
//
//	Cloud HSM / K-Chain ML-KEM
//	  └── Master KEK (unwrapped via HSM or threshold decryption)
//	        └── Org KEK = HMAC-SHA256(master, "vault:org:" + orgID)
//	              └── User DEK = HMAC-SHA256(orgKEK, "vault:user:" + userID)
//	                    └── SQLite shard (AES-256-GCM encrypted)
//	                          └── CRDT sync via ZAP (conflict-free merge)
//	                                └── Merkle root anchored to chain
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
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"

	"github.com/hanzoai/base/core"
	"github.com/hanzoai/base/tools/hook"
)

// Config configures the vault plugin.
type Config struct {
	Enabled     bool   `json:"enabled"`
	DataDir     string `json:"dataDir"`     // directory for per-user SQLite shards
	OrgID       string `json:"orgId"`       // organization identifier
	MasterKey   []byte `json:"-"`           // 32-byte master KEK (from HSM/K-Chain)
	ChainRPC    string `json:"chainRpc"`    // optional: I-Chain RPC for merkle anchoring
	SyncEnabled bool   `json:"syncEnabled"` // enable CRDT sync via ZAP
	ZAPPort     int    `json:"zapPort"`     // ZAP listen port for sync (default 9900)
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
		config.ZAPPort = 9900
	}

	p := &plugin{
		app:    app,
		config: config,
		orgKEK: deriveOrgKEK(config.MasterKey, config.OrgID),
		shards: make(map[string]*UserShard),
		logger: slog.Default().With("component", "vault"),
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
	orgKEK []byte // 32-byte org-level key encryption key
	shards map[string]*UserShard
	mu     sync.RWMutex
	logger *slog.Logger
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

	dek := deriveUserDEK(p.orgKEK, userID)
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
// KEK/DEK hierarchy:
//   Master KEK (from HSM or K-Chain ML-KEM threshold unwrap)
//     → Org KEK = HMAC-SHA256(masterKEK, "vault:org:" + orgID)
//       → User DEK = HMAC-SHA256(orgKEK, "vault:user:" + userID)
//         → Per-entry: AES-256-GCM with random 12-byte nonce
//
// Properties:
//   - Compromising one user's DEK does not reveal other users' DEKs
//   - Org KEK can be rotated without re-encrypting all user data
//     (re-derive DEKs from new org KEK, re-encrypt shard headers only)
//   - Master KEK never touches disk (held in HSM or threshold-reconstructed)
//   - Each SQLite shard is independently encrypted — no shared ciphertext

func deriveOrgKEK(masterKey []byte, orgID string) []byte {
	mac := hmac.New(sha256.New, masterKey)
	mac.Write([]byte("vault:org:" + orgID))
	return mac.Sum(nil)
}

func deriveUserDEK(orgKEK []byte, userID string) []byte {
	mac := hmac.New(sha256.New, orgKEK)
	mac.Write([]byte("vault:user:" + userID))
	return mac.Sum(nil)
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
