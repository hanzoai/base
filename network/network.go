// Package network replicates Base SQLite commits across peers via the
// luxfi/consensus Quasar engine. One shard = one Quasar engine = one DAG.
//
// See docs/NETWORK.md for the full design. Contract:
//
//   - Enabled()       — FromEnv toggles off when BASE_NETWORK is unset.
//   - Start/Stop      — lifecycle for all shard engines and transport.
//   - InstallWALHook  — per-connection commit hook; captures frames,
//                       submits to the shard engine.
//   - WriterFor       — which pod owns shardID (consistent hash).
//   - MembersFor      — replica set for shardID.
package network

import (
	"context"
	"fmt"
)

// Network is the public API Base uses. Standalone mode returns a no-op
// implementation from FromEnv; all methods are safe to call.
type Network interface {
	Enabled() bool

	Start(ctx context.Context) error
	Stop(ctx context.Context) error

	// InstallWALHook wires the commit hook on a raw SQLite driver connection
	// for the given shard. The hook captures the committed frame, signs it,
	// and submits it to the shard's Quasar engine. Apply-on-finalize is
	// idempotent by (salt, cksm).
	InstallWALHook(rawConn any, shardID string) error

	// WriterFor returns the current writer endpoint for shardID and whether
	// it is the local pod. Gateway uses this for request routing.
	WriterFor(shardID string) (endpoint string, local bool)

	// MembersFor returns all replica endpoints for shardID.
	MembersFor(shardID string) []string

	// Metrics exposes the Prom collectors. Callers register via their own
	// registry; we never touch the global default.
	Metrics() *Metrics
}

// New constructs a Network from an already-validated Config. Callers who
// build a Config by hand use this; most callers use FromEnv.
//
// Singleton (len(Peers) == 0) collapses to the standalone no-op path. A
// one-pod workload does not need consensus, does not need ZAP, does not
// start a listener, and does not self-dial. Scaling up to N>1 flips the
// same binary into a full network node by adding peers.
func New(cfg Config) (Network, error) {
	if err := cfg.validate(); err != nil {
		return nil, fmt.Errorf("network: %w", err)
	}
	if !cfg.Enabled || len(cfg.Peers) == 0 {
		return &noop{}, nil
	}
	return newNode(cfg)
}

// FromEnv reads BASE_* env vars. When BASE_NETWORK is empty or "standalone"
// it returns a no-op implementation with Enabled() == false. On validation
// error the error is returned so the caller can fail fast.
func FromEnv() (Network, error) {
	cfg, err := ConfigFromEnv()
	if err != nil {
		return nil, err
	}
	return New(cfg)
}

// noop is the standalone-mode implementation. All methods are safe and
// allocate nothing.
type noop struct{}

func (noop) Enabled() bool                              { return false }
func (noop) Start(context.Context) error                { return nil }
func (noop) Stop(context.Context) error                 { return nil }
func (noop) InstallWALHook(any, string) error           { return nil }
func (noop) WriterFor(string) (string, bool)            { return "", true }
func (noop) MembersFor(string) []string                 { return nil }
func (noop) Metrics() *Metrics                          { return nil }
