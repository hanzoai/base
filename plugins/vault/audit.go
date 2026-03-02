// Copyright (C) 2020-2026, Hanzo AI Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package vault

import (
	"crypto/sha256"
	"encoding/hex"
	"sync"
	"time"
)

// AuditEntry records a single vault operation.
type AuditEntry struct {
	Timestamp time.Time `json:"timestamp"`
	VaultID   string    `json:"vaultId"`   // org:user
	Actor     string    `json:"actor"`     // DID of who performed the action
	Action    string    `json:"action"`    // "put", "get", "delete", "sync", "anchor", "grant", "revoke"
	Resource  string    `json:"resource"`  // key, capability ID, etc.
	Hash      string    `json:"hash"`      // SHA-256 of entry for chaining
	PrevHash  string    `json:"prevHash"`  // hash of previous entry (chain link)
}

// AuditLog is an append-only log of vault operations.
// Entries are hash-chained: each entry's PrevHash points to the previous entry's Hash.
// The last hash is the audit merkle root, suitable for chain anchoring.
type AuditLog struct {
	entries []AuditEntry
	mu      sync.RWMutex
}

// NewAuditLog creates an empty audit log.
func NewAuditLog() *AuditLog {
	return &AuditLog{}
}

// Record appends an entry to the audit log.
// The entry is hash-chained to the previous entry automatically.
func (al *AuditLog) Record(vaultID, actor, action, resource string) {
	al.mu.Lock()
	defer al.mu.Unlock()

	prevHash := ""
	if len(al.entries) > 0 {
		prevHash = al.entries[len(al.entries)-1].Hash
	}

	entry := AuditEntry{
		Timestamp: time.Now(),
		VaultID:   vaultID,
		Actor:     actor,
		Action:    action,
		Resource:  resource,
		PrevHash:  prevHash,
	}

	h := sha256.New()
	h.Write([]byte(entry.VaultID))
	h.Write([]byte(entry.Actor))
	h.Write([]byte(entry.Action))
	h.Write([]byte(entry.Resource))
	h.Write([]byte(entry.PrevHash))
	entry.Hash = hex.EncodeToString(h.Sum(nil))

	al.entries = append(al.entries, entry)
}

// GetAuditLog returns entries for a vaultID since a given time.
// Pass time.Time{} (zero) to get all entries.
func (al *AuditLog) GetAuditLog(vaultID string, since time.Time) []AuditEntry {
	al.mu.RLock()
	defer al.mu.RUnlock()

	var result []AuditEntry
	for _, e := range al.entries {
		if e.VaultID != vaultID {
			continue
		}
		if !since.IsZero() && e.Timestamp.Before(since) {
			continue
		}
		result = append(result, e)
	}
	return result
}

// MerkleRoot returns the hash of the last entry, which is the root of the
// hash chain. This is the value anchored to chain.
// Returns empty string if the log is empty.
func (al *AuditLog) MerkleRoot() string {
	al.mu.RLock()
	defer al.mu.RUnlock()

	if len(al.entries) == 0 {
		return ""
	}
	return al.entries[len(al.entries)-1].Hash
}

// Len returns the number of entries in the audit log.
func (al *AuditLog) Len() int {
	al.mu.RLock()
	defer al.mu.RUnlock()
	return len(al.entries)
}

// Verify checks that the hash chain is consistent.
// Returns true if every entry's PrevHash matches the previous entry's Hash.
func (al *AuditLog) Verify() bool {
	al.mu.RLock()
	defer al.mu.RUnlock()

	for i, e := range al.entries {
		// Recompute hash
		h := sha256.New()
		h.Write([]byte(e.VaultID))
		h.Write([]byte(e.Actor))
		h.Write([]byte(e.Action))
		h.Write([]byte(e.Resource))
		h.Write([]byte(e.PrevHash))
		computed := hex.EncodeToString(h.Sum(nil))

		if computed != e.Hash {
			return false
		}
		if i > 0 && e.PrevHash != al.entries[i-1].Hash {
			return false
		}
		if i == 0 && e.PrevHash != "" {
			return false
		}
	}
	return true
}
