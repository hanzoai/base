// Copyright (C) 2020-2026, Hanzo AI Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package vault

import (
	"fmt"
	"sync"
)

// SyncProvider relays encrypted CRDT ops between devices/users.
type SyncProvider interface {
	Push(vaultID string, ops []Op) error
	Pull(vaultID string, since uint64) ([]Op, error)
	Subscribe(vaultID string, callback func([]Op)) error
}

// StorageProvider stores encrypted snapshots and blobs.
type StorageProvider interface {
	PutSnapshot(vaultID string, data []byte) (string, error) // returns content hash
	GetSnapshot(vaultID string, hash string) ([]byte, error)
	PutBlob(key string, data []byte) (string, error)
	GetBlob(hash string) ([]byte, error)
}

// RecoveryProvider assists with key recovery via threshold shares.
type RecoveryProvider interface {
	StoreShare(userID string, shareIndex int, encryptedShare []byte) error
	FetchShares(userID string, indices []int) ([][]byte, error)
}

// ProviderRegistry discovers and manages providers.
type ProviderRegistry struct {
	mu        sync.RWMutex
	sync      map[string]SyncProvider
	storage   map[string]StorageProvider
	recovery  map[string]RecoveryProvider
}

// NewProviderRegistry creates an empty registry.
func NewProviderRegistry() *ProviderRegistry {
	return &ProviderRegistry{
		sync:     make(map[string]SyncProvider),
		storage:  make(map[string]StorageProvider),
		recovery: make(map[string]RecoveryProvider),
	}
}

// RegisterSync registers a named sync provider.
func (r *ProviderRegistry) RegisterSync(name string, p SyncProvider) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sync[name] = p
}

// RegisterStorage registers a named storage provider.
func (r *ProviderRegistry) RegisterStorage(name string, p StorageProvider) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.storage[name] = p
}

// RegisterRecovery registers a named recovery provider.
func (r *ProviderRegistry) RegisterRecovery(name string, p RecoveryProvider) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.recovery[name] = p
}

// GetSync returns a sync provider by name.
func (r *ProviderRegistry) GetSync(name string) (SyncProvider, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.sync[name]
	if !ok {
		return nil, fmt.Errorf("vault: sync provider %q not found", name)
	}
	return p, nil
}

// GetStorage returns a storage provider by name.
func (r *ProviderRegistry) GetStorage(name string) (StorageProvider, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.storage[name]
	if !ok {
		return nil, fmt.Errorf("vault: storage provider %q not found", name)
	}
	return p, nil
}

// GetRecovery returns a recovery provider by name.
func (r *ProviderRegistry) GetRecovery(name string) (RecoveryProvider, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.recovery[name]
	if !ok {
		return nil, fmt.Errorf("vault: recovery provider %q not found", name)
	}
	return p, nil
}
