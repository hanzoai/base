package platform

// Pool manager is now in github.com/hanzoai/dbx.
// This file re-exports the types for backward compatibility.

import "github.com/hanzoai/dbx"

type DBPool = dbx.Pool
type DBPoolConfig = dbx.PoolConfig
type DBPoolManager = dbx.PoolManager
type PoolStats = dbx.PoolStats

func NewDBPoolManager(config DBPoolConfig) *DBPoolManager {
	return dbx.NewPoolManager(config)
}
