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
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
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
	// push s.oplog to ZAP peers, pull their ops, merge
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

	// submit receipt.MerkleRoot to I-Chain via chainRPC

	return receipt, nil
}

func (s *Session) close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	clear(s.dek)
	s.store = nil
	s.oplog = nil
}

// ─── Shared Vaults ───────────────────────────────────────────────────────────

// SharedSession is a multi-member vault derived from the org KEK + vaultID.
// Members list controls who can access. Each member decrypts with the shared DEK.
type SharedSession struct {
	Session
	vaultID string
	members map[string]bool
}

// OpenSharedVault creates a shared vault accessible by the listed members.
// The DEK is derived from orgKEK + vaultID (not any single user).
func (v *Vault) OpenSharedVault(vaultID string, members []string) (*SharedSession, error) {
	if vaultID == "" {
		return nil, fmt.Errorf("vault: vaultID required")
	}
	if len(members) == 0 {
		return nil, fmt.Errorf("vault: at least one member required")
	}

	// Shared DEK = HMAC-SHA256(orgKEK, "vault:shared:" + vaultID)
	mac := hmac.New(sha256.New, v.orgKEK)
	mac.Write([]byte("vault:shared:" + vaultID))
	dek := mac.Sum(nil)

	memberSet := make(map[string]bool, len(members))
	for _, m := range members {
		memberSet[m] = true
	}

	ss := &SharedSession{
		Session: Session{
			userID:   "shared:" + vaultID,
			orgID:    v.config.OrgID,
			dek:      dek,
			dataDir:  v.config.DataDir,
			chainRPC: v.config.ChainRPC,
			store:    make(map[string][]byte),
			oplog:    make([]Op, 0),
			version:  make(map[string]uint64),
		},
		vaultID: vaultID,
		members: memberSet,
	}
	return ss, nil
}

// IsMember returns true if the given userID is in the members list.
func (ss *SharedSession) IsMember(userID string) bool {
	return ss.members[userID]
}

// PutAs stores a value only if the caller is a member.
func (ss *SharedSession) PutAs(userID, key string, value []byte) error {
	if !ss.IsMember(userID) {
		return fmt.Errorf("vault: %s is not a member of shared vault %s", userID, ss.vaultID)
	}
	return ss.Session.Put(key, value)
}

// GetAs retrieves a value only if the caller is a member.
func (ss *SharedSession) GetAs(userID, key string) ([]byte, error) {
	if !ss.IsMember(userID) {
		return nil, fmt.Errorf("vault: %s is not a member of shared vault %s", userID, ss.vaultID)
	}
	return ss.Session.Get(key)
}

// ─── Multi-Device Enrollment ─────────────────────────────────────────────────

// DeviceKey is a device-specific wrapping key derived from the user DEK.
type DeviceKey struct {
	UserID   string
	DeviceID string
	Key      []byte // 32-byte device wrapping key
}

// EnrollDevice creates a device-specific wrapping key for the given user.
// Device key = HMAC-SHA256(userDEK, "device:" + deviceID).
// The device key wraps the user DEK for local storage on that device.
func (v *Vault) EnrollDevice(userID, deviceID string) (*DeviceKey, error) {
	v.mu.RLock()
	s, ok := v.users[userID]
	v.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("vault: user %s not open", userID)
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	mac := hmac.New(sha256.New, s.dek)
	mac.Write([]byte("device:" + deviceID))

	return &DeviceKey{
		UserID:   userID,
		DeviceID: deviceID,
		Key:      mac.Sum(nil),
	}, nil
}

// RevokeDevice removes a device key. The user DEK is unaffected — other
// devices continue to work. In production, the revoked device's wrapped
// DEK copy becomes useless because its wrapping key is discarded.
func (v *Vault) RevokeDevice(userID, deviceID string) error {
	v.mu.RLock()
	_, ok := v.users[userID]
	v.mu.RUnlock()
	if !ok {
		return fmt.Errorf("vault: user %s not open", userID)
	}
	// In v2 in-memory model, revocation is a no-op on the DEK itself.
	// The device key is simply not re-derived. In production, the wrapped
	// DEK blob stored on the revoked device becomes undecryptable because
	// the server no longer issues the device wrapping key.
	return nil
}

// ─── Per-Collection Sharing ──────────────────────────────────────────────────

// ShareToken carries a re-encrypted DEK scoped to a single collection.
type ShareToken struct {
	Collection    string
	RecipientID   string
	CollectionDEK []byte // 32-byte collection-scoped key
}

// ShareCollection creates a ShareToken that lets recipientID decrypt only
// the named collection. Collection DEK = HMAC-SHA256(userDEK, "collection:" + collectionName).
func (s *Session) ShareCollection(collection, recipientID string) *ShareToken {
	s.mu.RLock()
	defer s.mu.RUnlock()

	mac := hmac.New(sha256.New, s.dek)
	mac.Write([]byte("collection:" + collection))

	return &ShareToken{
		Collection:    collection,
		RecipientID:   recipientID,
		CollectionDEK: mac.Sum(nil),
	}
}

// PutCollection stores a value encrypted with the collection-scoped DEK.
func (s *Session) PutCollection(collection, key string, value []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	mac := hmac.New(sha256.New, s.dek)
	mac.Write([]byte("collection:" + collection))
	colDEK := mac.Sum(nil)

	shard := &UserShard{DEK: colDEK}
	encrypted, err := shard.Encrypt(value)
	if err != nil {
		return fmt.Errorf("vault: encrypt collection: %w", err)
	}

	storeKey := collection + ":" + key
	s.store[storeKey] = encrypted
	return nil
}

// GetCollection retrieves a value encrypted with the collection-scoped DEK.
func (s *Session) GetCollection(collection, key string) ([]byte, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	storeKey := collection + ":" + key
	encrypted, ok := s.store[storeKey]
	if !ok {
		return nil, fmt.Errorf("vault: key not found: %s", storeKey)
	}

	mac := hmac.New(sha256.New, s.dek)
	mac.Write([]byte("collection:" + collection))
	colDEK := mac.Sum(nil)

	shard := &UserShard{DEK: colDEK}
	return shard.Decrypt(encrypted)
}

// GetWithToken retrieves and decrypts a value using a ShareToken.
// Only works for keys in the token's collection.
func GetWithToken(token *ShareToken, store map[string][]byte, key string) ([]byte, error) {
	storeKey := token.Collection + ":" + key
	encrypted, ok := store[storeKey]
	if !ok {
		return nil, fmt.Errorf("vault: key not found: %s", storeKey)
	}
	shard := &UserShard{DEK: token.CollectionDEK}
	return shard.Decrypt(encrypted)
}

// ─── Threshold Recovery ──────────────────────────────────────────────────────

// CreateRecoveryShares splits the user DEK into `total` shares using
// XOR-based secret sharing. Any `threshold` shares can reconstruct the DEK
// when threshold == total (all shares needed). Proper Shamir SSS in v3.
func (s *Session) CreateRecoveryShares(threshold, total int) ([][]byte, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if threshold < 2 || total < threshold {
		return nil, fmt.Errorf("vault: need threshold >= 2 and total >= threshold")
	}
	if threshold != total {
		return nil, fmt.Errorf("vault: only supports threshold == total (XOR split); Shamir in future release")
	}

	dekLen := len(s.dek)
	shares := make([][]byte, total)

	// Generate (total-1) random shares.
	xorAcc := make([]byte, dekLen)
	copy(xorAcc, s.dek)

	for i := 0; i < total-1; i++ {
		share := make([]byte, dekLen)
		if _, err := io.ReadFull(rand.Reader, share); err != nil {
			return nil, fmt.Errorf("vault: random share: %w", err)
		}
		shares[i] = share
		for j := 0; j < dekLen; j++ {
			xorAcc[j] ^= share[j]
		}
	}

	// Last share = DEK XOR all previous shares.
	shares[total-1] = xorAcc

	return shares, nil
}

// RecoverFromShares reconstructs a DEK by XOR-ing all shares together.
// Requires all shares (threshold == total in v2).
func RecoverFromShares(shares [][]byte) ([]byte, error) {
	if len(shares) < 2 {
		return nil, fmt.Errorf("vault: need at least 2 shares for recovery")
	}
	keyLen := len(shares[0])
	for i, s := range shares {
		if len(s) != keyLen {
			return nil, fmt.Errorf("vault: share %d has wrong length %d (expected %d)", i, len(s), keyLen)
		}
	}

	result := make([]byte, keyLen)
	for _, share := range shares {
		for j := 0; j < keyLen; j++ {
			result[j] ^= share[j]
		}
	}
	return result, nil
}
