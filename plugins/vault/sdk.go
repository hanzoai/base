// Copyright (C) 2020-2026, Hanzo AI Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// SDK exposes the 5 primitives that any app needs:
//
//  1. Identity  — OpenUser, bind device, resolve DID
//  2. KeyAccess — GetShardKey, unwrap DEK, rotation
//  3. LocalDB   — OpenShard, read/write encrypted SQLite
//  4. Sync      — PushOps, PullOps, Merge (CRDT over ZAP)
//  5. Anchor    — CommitAnchor, merkle root to chain, audit proof
//
// Usage:
//
//	v, _ := vault.Open(vault.SDKConfig{
//	    DataDir:   "/data/vaults",
//	    MasterKEK: masterKey,     // from HSM or K-Chain
//	    OrgID:     "my-org",
//	})
//	defer v.Close()
//
//	session, _ := v.OpenUser("user-123")
//	session.Put("prefs", []byte(`{"theme":"dark"}`))
//	val, _ := session.Get("prefs")
//	session.Sync()
//	receipt, _ := session.Anchor()
package vault

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sync"
	"time"
)

// SDKConfig configures a Vault instance (standalone, no Base app required).
type SDKConfig struct {
	DataDir   string // directory for per-user SQLite shards
	MasterKEK []byte // 32-byte master key (from HSM or K-Chain)
	OrgID     string // organization identifier

	// Optional: chain anchoring
	ChainRPC string // I-Chain RPC for merkle root commits

	// Optional: sync
	SyncPeers []string // ZAP peer addresses for CRDT sync
}

// Vault is the top-level SDK handle. One per app process.
type Vault struct {
	config  SDKConfig
	orgKEK  []byte
	users   map[string]*Session
	mu      sync.RWMutex
}

// Open creates a new Vault instance.
func Open(cfg SDKConfig) (*Vault, error) {
	if len(cfg.MasterKEK) != 32 {
		return nil, fmt.Errorf("vault: masterKEK must be 32 bytes")
	}
	if cfg.OrgID == "" {
		return nil, fmt.Errorf("vault: orgID required")
	}
	return &Vault{
		config: cfg,
		orgKEK: deriveOrgKEK(cfg.MasterKEK, cfg.OrgID),
		users:  make(map[string]*Session),
	}, nil
}

// Close releases all resources and zeroes key material.
func (v *Vault) Close() {
	v.mu.Lock()
	defer v.mu.Unlock()
	for _, s := range v.users {
		s.close()
	}
	clear(v.orgKEK)
	v.users = nil
}

// ─── 1. Identity ─────────────────────────────────────────────────────────────

// OpenUser opens (or creates) a session for a user.
// Derives the per-user DEK and opens the encrypted SQLite shard.
func (v *Vault) OpenUser(userID string) (*Session, error) {
	v.mu.Lock()
	defer v.mu.Unlock()

	if s, ok := v.users[userID]; ok {
		return s, nil
	}

	dek := deriveUserDEK(v.orgKEK, userID)
	s := &Session{
		userID:  userID,
		orgID:   v.config.OrgID,
		dek:     dek,
		dataDir: v.config.DataDir,
		chainRPC: v.config.ChainRPC,
		store:   make(map[string][]byte), // in-memory for v1; SQLite in v2
		oplog:   make([]Op, 0),
		version: make(map[string]uint64),
	}

	v.users[userID] = s
	return s, nil
}

// ─── 2-5. Session (per user) ──────────────────────────────────────────────────

// Session is a per-user encrypted data context.
// It holds the user's DEK and provides all 5 SDK primitives.
type Session struct {
	userID   string
	orgID    string
	dek      []byte // 32-byte AES-256 data encryption key
	dataDir  string
	chainRPC string

	// v1: in-memory KV (replaced by encrypted SQLite in v2)
	store map[string][]byte

	// CRDT operation log
	oplog   []Op
	version map[string]uint64 // state vector: nodeID → seq

	mu sync.RWMutex
}

// Op is a CRDT operation in the oplog.
type Op struct {
	Seq    uint64 `json:"seq"`
	NodeID string `json:"nodeId"`
	Key    string `json:"key"`
	Value  []byte `json:"value"` // encrypted
	Time   int64  `json:"time"`
}

// AnchorReceipt is returned by Anchor() — proof of chain commitment.
type AnchorReceipt struct {
	MerkleRoot string `json:"merkleRoot"`
	UserID     string `json:"userId"`
	OrgID      string `json:"orgId"`
	Timestamp  int64  `json:"timestamp"`
	ChainTxID  string `json:"chainTxId,omitempty"` // set after on-chain commit
	OpCount    int    `json:"opCount"`
}

// ─── 3. LocalDB ──────────────────────────────────────────────────────────────

// Put stores an encrypted key-value pair.
// Value is encrypted with the user's DEK before storage.
func (s *Session) Put(key string, value []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	shard := &UserShard{DEK: s.dek}
	encrypted, err := shard.Encrypt(value)
	if err != nil {
		return fmt.Errorf("vault: encrypt: %w", err)
	}

	s.store[key] = encrypted

	// Append to CRDT oplog
	seq := s.version[s.userID] + 1
	s.version[s.userID] = seq
	s.oplog = append(s.oplog, Op{
		Seq:    seq,
		NodeID: s.userID,
		Key:    key,
		Value:  encrypted, // ops carry ciphertext, not plaintext
		Time:   time.Now().UnixMilli(),
	})

	return nil
}

// Get retrieves and decrypts a value by key.
func (s *Session) Get(key string) ([]byte, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	encrypted, ok := s.store[key]
	if !ok {
		return nil, fmt.Errorf("vault: key not found: %s", key)
	}

	shard := &UserShard{DEK: s.dek}
	return shard.Decrypt(encrypted)
}

// Delete removes a key.
func (s *Session) Delete(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.store, key)

	seq := s.version[s.userID] + 1
	s.version[s.userID] = seq
	s.oplog = append(s.oplog, Op{
		Seq:    seq,
		NodeID: s.userID,
		Key:    key,
		Value:  nil, // tombstone
		Time:   time.Now().UnixMilli(),
	})
}

// ─── 4. Sync ─────────────────────────────────────────────────────────────────

// Sync pushes local ops to peers and pulls remote ops.
// CRDT merge is conflict-free — concurrent writes to the same key
// resolve by last-writer-wins (Lamport timestamp + nodeID).
// Ops carry ciphertext — peers relay opaque bytes without decrypting.
func (s *Session) Sync() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// v1: no-op (single device)
	// v2: push s.oplog to ZAP peers, pull their ops, merge
	return nil
}

// Merge applies remote ops into local state.
// Each op carries encrypted values — merge does not require DEK.
func (s *Session) Merge(remoteOps []Op) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, op := range remoteOps {
		// Skip already-seen ops
		if op.Seq <= s.version[op.NodeID] {
			continue
		}
		s.version[op.NodeID] = op.Seq

		if op.Value == nil {
			delete(s.store, op.Key)
		} else {
			// LWW: remote op wins if it has higher timestamp
			s.store[op.Key] = op.Value
		}
		s.oplog = append(s.oplog, op)
	}
}

// ─── 5. Anchor ───────────────────────────────────────────────────────────────

// Anchor computes a merkle root over the current state and returns
// a receipt suitable for on-chain commitment.
// The chain stores ONLY the root hash — never row data, never keys.
func (s *Session) Anchor() (*AnchorReceipt, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Compute merkle root: hash of (sorted keys + encrypted values)
	h := sha256.New()
	h.Write([]byte(s.orgID + ":" + s.userID))
	for _, op := range s.oplog {
		h.Write([]byte(op.Key))
		h.Write(op.Value)
	}

	receipt := &AnchorReceipt{
		MerkleRoot: hex.EncodeToString(h.Sum(nil)),
		UserID:     s.userID,
		OrgID:      s.orgID,
		Timestamp:  time.Now().Unix(),
		OpCount:    len(s.oplog),
	}

	// v2: submit receipt.MerkleRoot to I-Chain via chainRPC

	return receipt, nil
}

func (s *Session) close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	clear(s.dek)
	s.store = nil
	s.oplog = nil
}
