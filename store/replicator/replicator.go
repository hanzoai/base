// Package replicator gives Base's per-tenant SQLite substrate continuous
// streaming replication and point-in-time restore via hanzoai/replicate — the
// HA / resilience layer (Pillar 2): a pod dying or rescheduling restores each
// tenant DB from its replica (bounded RPO, no data loss).
//
// # One at-rest boundary — encryption lives in the storage client
//
// Encryption is NOT applied at replicate's LTX layer. In production the
// replica client is a vfs-backed client that PQ-encrypts every block with the
// tenant's age key (store.TenantKey) — the SAME key the whole-file path uses.
// So there is exactly one at-rest boundary (luxfi/age, ML-KEM-768 + X25519),
// one key per tenant, across the whole-file, block, and replica-stream paths,
// with no double-encryption.
//
// (replicate v0.8.0's built-in age — Replica.AgeRecipients — is deliberately
// unused: it encrypts the LTX stream BEFORE the file/s3 client peeks the LTX
// header for a timestamp, so those clients reject the ciphertext. Keeping
// encryption in the storage client is both the correct single-boundary design
// and the working one.)
//
// Lifecycle per tenant DB:
//
//	Restore(...)   pull the DB from its replica into a fresh pod (failover / hydrate)
//	Open(...)      start tailing the WAL to the replica
//	Handle.Sync    force a checkpoint + push (driven by the store's checkpoint)
//	Handle.Close   stop streaming (on evict / shutdown)
package replicator

import (
	"context"
	"errors"

	"github.com/hanzoai/replicate"
)

// Handle is a running replication of one tenant DB. Stop it on evict/close.
type Handle struct {
	db *replicate.DB
}

// Open starts replication of the SQLite DB at localPath to client. The caller
// keeps writing through its own connection; replicate tails the WAL on Sync.
// client owns at-rest encryption (vfs-backed, per-tenant, in production).
func Open(localPath string, client replicate.ReplicaClient) (*Handle, error) {
	if client == nil {
		return nil, errors.New("replicator: replica client is required")
	}
	db := replicate.NewDB(localPath)
	db.MonitorInterval = 0 // explicit-sync: the store's checkpoint drives Sync
	r := replicate.NewReplicaWithClient(db, client)
	r.MonitorEnabled = false
	db.Replica = r
	if err := db.Open(); err != nil {
		return nil, err
	}
	return &Handle{db: db}, nil
}

// Sync checkpoints the WAL into the shadow WAL and pushes pending frames to the
// replica, tightening the RPO to "now". Called from the store's checkpoint path.
func (h *Handle) Sync(ctx context.Context) error {
	if err := h.db.Sync(ctx); err != nil {
		return err
	}
	return h.db.Replica.Sync(ctx)
}

// Close stops streaming and releases the replicate handle.
func (h *Handle) Close(ctx context.Context) error {
	return h.db.Close(ctx)
}

// Restore pulls the tenant DB from its replica into localPath (most-recent
// state). Returns (false, nil) when no replica exists yet (fresh tenant —
// nothing to restore). This is the restore-if-absent path a pod runs on
// hydrate before opening SQLite.
func Restore(ctx context.Context, localPath string, client replicate.ReplicaClient) (bool, error) {
	if client == nil {
		return false, errors.New("replicator: replica client is required")
	}
	has, err := hasSnapshot(ctx, client)
	if err != nil {
		return false, err
	}
	if !has {
		return false, nil
	}

	db := replicate.NewDB(localPath)
	r := replicate.NewReplicaWithClient(db, client)
	db.Replica = r
	opt := replicate.NewRestoreOptions()
	opt.OutputPath = localPath
	if err := r.Restore(ctx, opt); err != nil {
		if errors.Is(err, replicate.ErrNoSnapshots) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// hasSnapshot reports whether the replica holds any LTX file at level 0 — the
// gate that distinguishes a fresh tenant (nothing to restore) from one whose
// DB must be pulled back on failover.
func hasSnapshot(ctx context.Context, client replicate.ReplicaClient) (bool, error) {
	if err := client.Init(ctx); err != nil {
		return false, err
	}
	it, err := client.LTXFiles(ctx, 0, 0, false)
	if err != nil {
		return false, err
	}
	defer it.Close()
	if it.Next() {
		return true, nil
	}
	return false, it.Err()
}
