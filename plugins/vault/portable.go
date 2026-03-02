// Copyright (C) 2020-2026, Hanzo AI Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package vault

import (
	"encoding/json"
	"fmt"
	"time"
)

// Bundle is a portable vault export: JSON envelope with encrypted data + oplog + metadata.
type Bundle struct {
	Version   int             `json:"version"`
	UserID    string          `json:"userId"`
	OrgID     string          `json:"orgId"`
	Timestamp int64           `json:"timestamp"`
	Snapshot  json.RawMessage `json:"snapshot"` // encrypted store entries
	Oplog     []Op            `json:"oplog"`
	Metadata  BundleMetadata  `json:"metadata"`
}

// BundleMetadata holds non-sensitive metadata about the bundle.
type BundleMetadata struct {
	KeyCount int    `json:"keyCount"`
	OpCount  int    `json:"opCount"`
	Format   string `json:"format"` // "vault-bundle-v3"
}

// ExportVault exports the session's encrypted state as a portable JSON bundle.
// The bundle contains ciphertext only — the DEK is NOT included.
// The caller must supply the DEK separately to import.
func ExportVault(session *Session) ([]byte, error) {
	session.mu.RLock()
	defer session.mu.RUnlock()

	if session.store == nil {
		return nil, fmt.Errorf("vault: session is closed")
	}

	// Serialize the encrypted store as a map of key → hex-encoded ciphertext.
	snapshot := make(map[string][]byte, len(session.store))
	for k, v := range session.store {
		snapshot[k] = v
	}

	snapshotJSON, err := json.Marshal(snapshot)
	if err != nil {
		return nil, fmt.Errorf("vault: marshal snapshot: %w", err)
	}

	oplog := make([]Op, len(session.oplog))
	copy(oplog, session.oplog)

	bundle := Bundle{
		Version:   3,
		UserID:    session.userID,
		OrgID:     session.orgID,
		Timestamp: time.Now().Unix(),
		Snapshot:  snapshotJSON,
		Oplog:     oplog,
		Metadata: BundleMetadata{
			KeyCount: len(snapshot),
			OpCount:  len(oplog),
			Format:   "vault-bundle-v3",
		},
	}

	data, err := json.Marshal(bundle)
	if err != nil {
		return nil, fmt.Errorf("vault: marshal bundle: %w", err)
	}
	return data, nil
}

// ImportVault imports a vault from a portable bundle.
// The caller must provide the correct DEK to decrypt values.
func ImportVault(data []byte, dek []byte) (*Session, error) {
	if len(dek) != 32 {
		return nil, fmt.Errorf("vault: DEK must be 32 bytes")
	}

	var bundle Bundle
	if err := json.Unmarshal(data, &bundle); err != nil {
		return nil, fmt.Errorf("vault: unmarshal bundle: %w", err)
	}

	if bundle.Version != 3 {
		return nil, fmt.Errorf("vault: unsupported bundle version %d", bundle.Version)
	}

	// Deserialize the encrypted store.
	var snapshot map[string][]byte
	if err := json.Unmarshal(bundle.Snapshot, &snapshot); err != nil {
		return nil, fmt.Errorf("vault: unmarshal snapshot: %w", err)
	}

	// Rebuild version map from oplog.
	version := make(map[string]uint64)
	for _, op := range bundle.Oplog {
		if op.Seq > version[op.NodeID] {
			version[op.NodeID] = op.Seq
		}
	}

	session := &Session{
		userID:  bundle.UserID,
		orgID:   bundle.OrgID,
		dek:     make([]byte, 32),
		store:   snapshot,
		oplog:   bundle.Oplog,
		version: version,
	}
	copy(session.dek, dek)

	return session, nil
}
