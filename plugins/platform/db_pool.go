package platform

import (
	"container/list"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"

	"github.com/hanzoai/base/core"
	"github.com/pocketbase/dbx"
)

// DBPool holds a dual-pool connection pair for one SQLite database.
//
// Reads use the concurrent pool (configurable MaxOpenConns), writes use
// the nonconcurrent pool (1 conn) to avoid SQLITE_BUSY under WAL mode.
//
// Callers must bracket usage with Acquire/Release so the LRU manager
// knows when a pool is safe to evict.
type DBPool struct {
	Path          string
	Concurrent    *dbx.DB // reads
	Nonconcurrent *dbx.DB // writes (serialized)
	refCount      int64
}

func (p *DBPool) Acquire() { atomic.AddInt64(&p.refCount, 1) }
func (p *DBPool) Release() { atomic.AddInt64(&p.refCount, -1) }
func (p *DBPool) InUse() bool { return atomic.LoadInt64(&p.refCount) > 0 }

func (p *DBPool) Close() error {
	var firstErr error
	if p.Concurrent != nil {
		if err := p.Concurrent.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if p.Nonconcurrent != nil {
		if err := p.Nonconcurrent.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// DBPoolConfig tunes the pool manager for a given workload.
type DBPoolConfig struct {
	// MaxPools is the maximum number of open database file pools.
	// Each pool holds 2 connections (read + write). Default: 256.
	//
	// Sizing guide (per service instance):
	//   4 CPU / 8 GB  → 128-256 pools (~50 MB overhead)
	//   8 CPU / 16 GB → 256-512 pools (~100 MB overhead)
	//  16 CPU / 32 GB → 512-1024 pools (~200 MB overhead)
	//
	// Each idle pool costs ~200 KB (two sqlite3 connections with
	// 32 MB page cache split across read + write, but idle conns
	// release most pages). Active pools under load use more.
	MaxPools int

	// ReadConns is the max open connections in the read pool per DB.
	// Higher values improve read throughput but use more memory.
	// Default: 4. Max recommended: min(NumCPU, 16).
	ReadConns int

	// ReadIdleConns is the idle connection count for the read pool.
	// Default: 2.
	ReadIdleConns int

	// Connect is the SQLite connection function.
	// Default: core.DefaultDBConnect (WAL mode, busy_timeout=10s).
	Connect core.DBConnectFunc
}

func (c *DBPoolConfig) defaults() {
	if c.MaxPools <= 0 {
		c.MaxPools = 256
	}
	if c.ReadConns <= 0 {
		c.ReadConns = 4
	}
	if c.ReadIdleConns <= 0 {
		c.ReadIdleConns = 2
	}
	if c.Connect == nil {
		c.Connect = core.DefaultDBConnect
	}
}

// DBPoolManager manages an LRU cache of SQLite connection pools.
//
// Design:
//   - Services are stateless: any pod serves any org/user by loading
//     the correct SQLite file from shared storage.
//   - Pools are opened lazily on first access and evicted LRU when
//     capacity is reached.
//   - Eviction never closes a pool that has active references (InUse).
//   - The manager is safe for concurrent use from multiple goroutines.
type DBPoolManager struct {
	mu     sync.Mutex
	pools  map[string]*list.Element
	lru    *list.List
	config DBPoolConfig
	stats  PoolStats
}

// PoolStats tracks pool manager metrics for monitoring.
type PoolStats struct {
	Hits      int64 // cache hits (pool already open)
	Misses    int64 // cache misses (new pool opened)
	Evictions int64 // pools evicted from LRU
	Opens     int64 // total pools opened
	Errors    int64 // connection open failures
}

type lruEntry struct {
	key  string
	pool *DBPool
}

// NewDBPoolManager creates a pool manager with the given config.
func NewDBPoolManager(config DBPoolConfig) *DBPoolManager {
	config.defaults()
	return &DBPoolManager{
		pools:  make(map[string]*list.Element, config.MaxPools),
		lru:    list.New(),
		config: config,
	}
}

// Get returns a connection pool for the given database path.
// The pool is created if it doesn't exist.
// The caller MUST call pool.Release() when done.
func (m *DBPoolManager) Get(dbPath string) (*DBPool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Cache hit
	if elem, ok := m.pools[dbPath]; ok {
		m.lru.MoveToFront(elem)
		pool := elem.Value.(*lruEntry).pool
		pool.Acquire()
		atomic.AddInt64(&m.stats.Hits, 1)
		return pool, nil
	}

	// Cache miss — open new pool
	atomic.AddInt64(&m.stats.Misses, 1)

	pool, err := m.openPool(dbPath)
	if err != nil {
		atomic.AddInt64(&m.stats.Errors, 1)
		return nil, err
	}
	atomic.AddInt64(&m.stats.Opens, 1)

	// Evict if at capacity
	for m.lru.Len() >= m.config.MaxPools {
		if !m.evictOne() {
			break // all in use, allow temporary over-capacity
		}
	}

	entry := &lruEntry{key: dbPath, pool: pool}
	elem := m.lru.PushFront(entry)
	m.pools[dbPath] = elem

	pool.Acquire()
	return pool, nil
}

func (m *DBPoolManager) openPool(dbPath string) (*DBPool, error) {
	// Ensure parent directory exists
	if err := os.MkdirAll(filepath.Dir(dbPath), 0700); err != nil {
		return nil, fmt.Errorf("create dir for %s: %w", dbPath, err)
	}

	concurrent, err := m.config.Connect(dbPath)
	if err != nil {
		return nil, fmt.Errorf("open read pool %s: %w", dbPath, err)
	}
	concurrent.DB().SetMaxOpenConns(m.config.ReadConns)
	concurrent.DB().SetMaxIdleConns(m.config.ReadIdleConns)

	nonconcurrent, err := m.config.Connect(dbPath)
	if err != nil {
		concurrent.Close()
		return nil, fmt.Errorf("open write pool %s: %w", dbPath, err)
	}
	nonconcurrent.DB().SetMaxOpenConns(1)
	nonconcurrent.DB().SetMaxIdleConns(1)

	return &DBPool{
		Path:          dbPath,
		Concurrent:    concurrent,
		Nonconcurrent: nonconcurrent,
	}, nil
}

func (m *DBPoolManager) evictOne() bool {
	for elem := m.lru.Back(); elem != nil; elem = elem.Prev() {
		entry := elem.Value.(*lruEntry)
		if !entry.pool.InUse() {
			m.lru.Remove(elem)
			delete(m.pools, entry.key)
			entry.pool.Close()
			atomic.AddInt64(&m.stats.Evictions, 1)
			return true
		}
	}
	return false
}

// Stats returns a snapshot of pool manager metrics.
func (m *DBPoolManager) Stats() PoolStats {
	return PoolStats{
		Hits:      atomic.LoadInt64(&m.stats.Hits),
		Misses:    atomic.LoadInt64(&m.stats.Misses),
		Evictions: atomic.LoadInt64(&m.stats.Evictions),
		Opens:     atomic.LoadInt64(&m.stats.Opens),
		Errors:    atomic.LoadInt64(&m.stats.Errors),
	}
}

// Len returns the number of currently open pools.
func (m *DBPoolManager) Len() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.lru.Len()
}

// Close closes all open pools. Call on shutdown.
func (m *DBPoolManager) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, elem := range m.pools {
		elem.Value.(*lruEntry).pool.Close()
	}
	m.pools = make(map[string]*list.Element)
	m.lru.Init()
}
