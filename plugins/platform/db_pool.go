package platform

// Pool manager is now in github.com/hanzoai/dbx.
// This file re-exports the types for backward compatibility
// and provides environment-variable-driven defaults.

import (
	"os"
	"runtime"
	"strconv"
	"time"

	"github.com/hanzoai/dbx"
)

type DBPool = dbx.Pool
type DBPoolConfig = dbx.PoolConfig
type DBPoolManager = dbx.PoolManager
type PoolStats = dbx.PoolStats

func NewDBPoolManager(config DBPoolConfig) *DBPoolManager {
	return dbx.NewPoolManager(config)
}

// DefaultPoolConfig returns a PoolConfig with production defaults,
// overridable via environment variables:
//
//	DB_POOL_MAX          — max open DB pools (default: 2000, hard cap: 2000)
//	DB_POOL_IDLE_TIMEOUT — idle timeout duration (default: 30s, e.g. "1m", "45s")
//	DB_POOL_SHARDS       — number of lock shards (default: runtime.NumCPU())
//	DB_POOL_READ_CONNS   — read connections per DB (default: 4)
func DefaultPoolConfig() DBPoolConfig {
	c := DBPoolConfig{
		MaxPools:    2000,
		ReadConns:   4,
		NumShards:   runtime.NumCPU(),
		IdleTimeout: 30 * time.Second,
	}

	if v := os.Getenv("DB_POOL_MAX"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			c.MaxPools = n
		}
	}
	if v := os.Getenv("DB_POOL_IDLE_TIMEOUT"); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d >= 0 {
			c.IdleTimeout = d
		}
	}
	if v := os.Getenv("DB_POOL_SHARDS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			c.NumShards = n
		}
	}
	if v := os.Getenv("DB_POOL_READ_CONNS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			c.ReadConns = n
		}
	}

	return c
}
