package core

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/hanzoai/base/network"
	"github.com/hanzoai/dbx"
	sqlite "modernc.org/sqlite"
)

// baseNetwork is a local alias for network.Network so the BaseApp field can
// use a short name without shadowing the imported package.
type baseNetwork = network.Network

// attachNetwork is the one hook point into BaseApp that network integration
// requires. It is called at the end of initDataDB — standalone mode (the
// default) is a fast-path return with zero cost.
//
// When BASE_NETWORK=quasar, it builds a Network from the environment,
// starts it, and installs a commit hook on one pooled concurrentDB
// connection per shard key. Today we register a single hook against the
// default shard ("_") so baseline replication works; multi-shard
// registration is driven by per-request shard resolution upstream.
func (app *BaseApp) attachNetwork(ctx context.Context) error {
	net, err := network.FromEnv()
	if err != nil {
		return fmt.Errorf("network from env: %w", err)
	}
	if !net.Enabled() {
		return nil
	}
	if err := net.Start(ctx); err != nil {
		return fmt.Errorf("network start: %w", err)
	}
	app.network = net

	db := app.concurrentDB
	if db == nil {
		return nil
	}
	return installCommitHook(ctx, db, net, "_")
}

// installCommitHook reserves one *sql.Conn from the pool and wires the
// network commit hook onto its driver conn. The conn is retained for the
// lifetime of the process so the hook outlives pool churn.
//
// Failure here is soft: we log but do not block Bootstrap — a broken
// transport is less harmful than a refusal to start.
func installCommitHook(ctx context.Context, db dbx.Builder, net network.Network, shardID string) error {
	sqlDB, ok := unwrapSQL(db)
	if !ok {
		return fmt.Errorf("network: db is not *sql.DB (got %T)", db)
	}
	raw, err := sqlDB.Conn(ctx)
	if err != nil {
		return fmt.Errorf("network: reserve conn: %w", err)
	}
	return raw.Raw(func(driverConn any) error {
		reg, ok := driverConn.(sqlite.HookRegisterer)
		if !ok {
			return fmt.Errorf("network: driver conn %T has no commit hook", driverConn)
		}
		return net.InstallWALHook(commitHookAdapter{reg}, shardID)
	})
}

// commitHookAdapter narrows sqlite.HookRegisterer to the network package's
// own HookRegisterer shape, which takes func() int32 directly rather than
// the modernc-typed CommitHookFn. This adapter is the only place in Base
// that the sqlite driver type leaks into the integration surface.
type commitHookAdapter struct{ inner sqlite.HookRegisterer }

func (c commitHookAdapter) RegisterCommitHook(cb func() int32) {
	c.inner.RegisterCommitHook(sqlite.CommitHookFn(cb))
}

// unwrapSQL pulls the underlying *sql.DB out of a dbx.Builder. dbx.DB
// exposes DB() explicitly; other builders (transactions, wrappers) return
// false so the caller can degrade gracefully.
func unwrapSQL(b dbx.Builder) (*sql.DB, bool) {
	type hasDB interface{ DB() *sql.DB }
	if v, ok := b.(hasDB); ok {
		return v.DB(), true
	}
	return nil, false
}
