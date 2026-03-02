// Copyright (C) 2020-2026, Hanzo AI Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package vault

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// ─── LocalSyncProvider ──────────────────────────────────────────────────────

// LocalSyncProvider uses filesystem + channels for local/self-hosted sync.
type LocalSyncProvider struct {
	dir string

	mu          sync.Mutex
	ops         map[string][]Op           // vaultID → ops
	subscribers map[string][]func([]Op)   // vaultID → callbacks
}

// NewLocalSyncProvider creates a sync provider backed by a directory.
func NewLocalSyncProvider(dir string) (*LocalSyncProvider, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, fmt.Errorf("vault: create sync dir: %w", err)
	}
	return &LocalSyncProvider{
		dir:         dir,
		ops:         make(map[string][]Op),
		subscribers: make(map[string][]func([]Op)),
	}, nil
}

// Push appends ops and notifies subscribers.
func (p *LocalSyncProvider) Push(vaultID string, ops []Op) error {
	p.mu.Lock()
	p.ops[vaultID] = append(p.ops[vaultID], ops...)
	subs := p.subscribers[vaultID]
	p.mu.Unlock()

	for _, cb := range subs {
		cb(ops)
	}
	return nil
}

// Pull returns ops with Seq > since for the given vault.
func (p *LocalSyncProvider) Pull(vaultID string, since uint64) ([]Op, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	all := p.ops[vaultID]
	var result []Op
	for _, op := range all {
		if op.Seq > since {
			result = append(result, op)
		}
	}
	return result, nil
}

// Subscribe registers a callback for new ops.
func (p *LocalSyncProvider) Subscribe(vaultID string, callback func([]Op)) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.subscribers[vaultID] = append(p.subscribers[vaultID], callback)
	return nil
}

// ─── LocalStorageProvider ───────────────────────────────────────────────────

// LocalStorageProvider uses content-addressed file storage.
type LocalStorageProvider struct {
	dir string
	mu  sync.Mutex
}

// NewLocalStorageProvider creates a storage provider backed by a directory.
func NewLocalStorageProvider(dir string) (*LocalStorageProvider, error) {
	for _, sub := range []string{"snapshots", "blobs"} {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0700); err != nil {
			return nil, fmt.Errorf("vault: create storage dir: %w", err)
		}
	}
	return &LocalStorageProvider{dir: dir}, nil
}

func contentHash(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

// PutSnapshot stores a snapshot and returns its content hash.
func (p *LocalStorageProvider) PutSnapshot(vaultID string, data []byte) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	hash := contentHash(data)
	vaultDir := filepath.Join(p.dir, "snapshots", vaultID)
	if err := os.MkdirAll(vaultDir, 0700); err != nil {
		return "", fmt.Errorf("vault: create snapshot dir: %w", err)
	}
	path := filepath.Join(vaultDir, hash)
	if err := os.WriteFile(path, data, 0600); err != nil {
		return "", fmt.Errorf("vault: write snapshot: %w", err)
	}
	return hash, nil
}

// GetSnapshot retrieves a snapshot by vault ID and content hash.
func (p *LocalStorageProvider) GetSnapshot(vaultID string, hash string) ([]byte, error) {
	path := filepath.Join(p.dir, "snapshots", vaultID, hash)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("vault: read snapshot: %w", err)
	}
	return data, nil
}

// PutBlob stores a blob and returns its content hash.
func (p *LocalStorageProvider) PutBlob(key string, data []byte) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	hash := contentHash(data)
	path := filepath.Join(p.dir, "blobs", hash)
	if err := os.WriteFile(path, data, 0600); err != nil {
		return "", fmt.Errorf("vault: write blob: %w", err)
	}
	return hash, nil
}

// GetBlob retrieves a blob by content hash.
func (p *LocalStorageProvider) GetBlob(hash string) ([]byte, error) {
	path := filepath.Join(p.dir, "blobs", hash)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("vault: read blob: %w", err)
	}
	return data, nil
}

// ─── LocalRecoveryProvider ──────────────────────────────────────────────────

// LocalRecoveryProvider stores threshold key shares in a local directory.
type LocalRecoveryProvider struct {
	dir string
	mu  sync.Mutex
}

// NewLocalRecoveryProvider creates a recovery provider backed by a directory.
func NewLocalRecoveryProvider(dir string) (*LocalRecoveryProvider, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, fmt.Errorf("vault: create recovery dir: %w", err)
	}
	return &LocalRecoveryProvider{dir: dir}, nil
}

// StoreShare stores an encrypted share for a user.
func (p *LocalRecoveryProvider) StoreShare(userID string, shareIndex int, encryptedShare []byte) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	userDir := filepath.Join(p.dir, userID)
	if err := os.MkdirAll(userDir, 0700); err != nil {
		return fmt.Errorf("vault: create user recovery dir: %w", err)
	}
	path := filepath.Join(userDir, fmt.Sprintf("share-%d", shareIndex))
	if err := os.WriteFile(path, encryptedShare, 0600); err != nil {
		return fmt.Errorf("vault: write share: %w", err)
	}
	return nil
}

// FetchShares retrieves encrypted shares for a user by index.
func (p *LocalRecoveryProvider) FetchShares(userID string, indices []int) ([][]byte, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	shares := make([][]byte, 0, len(indices))
	for _, idx := range indices {
		path := filepath.Join(p.dir, userID, fmt.Sprintf("share-%d", idx))
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("vault: read share %d: %w", idx, err)
		}
		shares = append(shares, data)
	}
	return shares, nil
}

// ─── Interface compliance ───────────────────────────────────────────────────

var (
	_ SyncProvider     = (*LocalSyncProvider)(nil)
	_ StorageProvider  = (*LocalStorageProvider)(nil)
	_ RecoveryProvider = (*LocalRecoveryProvider)(nil)
)

