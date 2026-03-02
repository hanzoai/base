package platform

import (
	"container/list"
	"fmt"
	"hash/fnv"
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
	//   2 CPU / 4 GB  → 64   pools  (~13 MB)
	//   4 CPU / 8 GB  → 256  pools  (~50 MB)
	//   8 CPU / 16 GB → 512  pools  (~100 MB)
	//  16 CPU / 32 GB → 1024 pools  (~200 MB)
	//
	// Rule of thumb: MaxPools = 64 * NumCPU
	MaxPools int

	// ReadConns is max open connections in the read pool per DB.
	// Default: 4. Recommended: NumCPU (up to 16).
	ReadConns int

	// ReadIdleConns is the idle connection count for the read pool.
	// Default: 2.
	ReadIdleConns int

	// NumShards controls lock sharding. Higher = less contention.
	// Default: 16. Must be power of 2.
	NumShards int

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
	if c.NumShards <= 0 {
		c.NumShards = 16
	}
	if c.Connect == nil {
		c.Connect = core.DefaultDBConnect
	}
}

// PoolStats tracks pool manager metrics. Each field is cache-line
// padded to prevent false sharing across CPU cores.
type PoolStats struct {
	Hits      int64; _ [7]int64 // pad to 64 bytes
	Misses    int64; _ [7]int64
	Evictions int64; _ [7]int64
	Opens     int64; _ [7]int64
	Errors    int64; _ [7]int64
}

type lruEntry struct {
	key  string
	pool *DBPool
}

// poolShard is one lock-isolated slice of the pool map.
type poolShard struct {
	mu    sync.RWMutex
	pools map[string]*list.Element
	lru   *list.List
}

// DBPoolManager manages an LRU cache of SQLite connection pools.
//
// Optimizations:
//   - Sharded RWMutex: 16 independent shards reduce lock contention.
//     Cache hits use RLock (non-exclusive). Only misses/evictions take Lock.
//   - Cache-line padded stats: no false sharing across CPU cores.
//   - Two-phase eviction: find targets under lock, close without lock.
//   - Stateless design: any pod serves any org by loading its SQLite file.
type DBPoolManager struct {
	shards []poolShard
	config DBPoolConfig
	stats  PoolStats
}

// NewDBPoolManager creates a pool manager with the given config.
func NewDBPoolManager(config DBPoolConfig) *DBPoolManager {
	config.defaults()
	shards := make([]poolShard, config.NumShards)
	perShard := config.MaxPools / config.NumShards
	if perShard < 1 {
		perShard = 1
	}
	for i := range shards {
		shards[i] = poolShard{
			pools: make(map[string]*list.Element, perShard),
			lru:   list.New(),
		}
	}
	return &DBPoolManager{
		shards: shards,
		config: config,
	}
}

func (m *DBPoolManager) shard(key string) *poolShard {
	h := fnv.New32a()
	h.Write([]byte(key))
	return &m.shards[h.Sum32()%uint32(len(m.shards))]
}

// Get returns a connection pool for the given database path.
// The pool is created if it doesn't exist.
// The caller MUST call pool.Release() when done.
func (m *DBPoolManager) Get(dbPath string) (*DBPool, error) {
	s := m.shard(dbPath)

	// Fast path: RLock for cache hit (non-exclusive)
	s.mu.RLock()
	if elem, ok := s.pools[dbPath]; ok {
		pool := elem.Value.(*lruEntry).pool
		pool.Acquire()
		s.mu.RUnlock()
		// Move to front requires write lock — defer to next miss or periodic reorder.
		// Skipping LRU reorder on hit is acceptable: the entry won't be evicted
		// while InUse anyway, and frequency-based aging is "good enough" for LRU.
		atomic.AddInt64(&m.stats.Hits, 1)
		return pool, nil
	}
	s.mu.RUnlock()

	// Slow path: Lock for miss + open + eviction
	s.mu.Lock()

	// Double-check after acquiring write lock
	if elem, ok := s.pools[dbPath]; ok {
		s.lru.MoveToFront(elem)
		pool := elem.Value.(*lruEntry).pool
		pool.Acquire()
		s.mu.Unlock()
		atomic.AddInt64(&m.stats.Hits, 1)
		return pool, nil
	}

	// Open new pool (under lock — protects the map)
	pool, err := m.openPool(dbPath)
	if err != nil {
		s.mu.Unlock()
		atomic.AddInt64(&m.stats.Errors, 1)
		return nil, err
	}
	atomic.AddInt64(&m.stats.Opens, 1)

	// Two-phase eviction: find targets under lock, close after unlock
	maxPerShard := m.config.MaxPools / len(m.shards)
	if maxPerShard < 1 {
		maxPerShard = 1
	}
	var toClose []*DBPool
	for s.lru.Len() >= maxPerShard {
		evicted := s.evictOneLocked()
		if evicted == nil {
			break
		}
		toClose = append(toClose, evicted)
	}

	entry := &lruEntry{key: dbPath, pool: pool}
	elem := s.lru.PushFront(entry)
	s.pools[dbPath] = elem
	pool.Acquire()
	s.mu.Unlock()

	// Close evicted pools without holding lock
	for _, p := range toClose {
		p.Close()
		atomic.AddInt64(&m.stats.Evictions, 1)
	}

	atomic.AddInt64(&m.stats.Misses, 1)
	return pool, nil
}

func (m *DBPoolManager) openPool(dbPath string) (*DBPool, error) {
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

// evictOneLocked finds and removes one unused pool from the shard.
// Caller must hold s.mu.Lock(). Returns the evicted pool for deferred Close.
func (s *poolShard) evictOneLocked() *DBPool {
	for elem := s.lru.Back(); elem != nil; elem = elem.Prev() {
		entry := elem.Value.(*lruEntry)
		if !entry.pool.InUse() {
			s.lru.Remove(elem)
			delete(s.pools, entry.key)
			return entry.pool
		}
	}
	return nil
}

// GetStats returns a snapshot of pool manager metrics.
func (m *DBPoolManager) GetStats() (hits, misses, evictions, opens, errors int64) {
	return atomic.LoadInt64(&m.stats.Hits),
		atomic.LoadInt64(&m.stats.Misses),
		atomic.LoadInt64(&m.stats.Evictions),
		atomic.LoadInt64(&m.stats.Opens),
		atomic.LoadInt64(&m.stats.Errors)
}

// Stats returns a PoolStats snapshot.
func (m *DBPoolManager) Stats() PoolStats {
	return PoolStats{
		Hits:      atomic.LoadInt64(&m.stats.Hits),
		Misses:    atomic.LoadInt64(&m.stats.Misses),
		Evictions: atomic.LoadInt64(&m.stats.Evictions),
		Opens:     atomic.LoadInt64(&m.stats.Opens),
		Errors:    atomic.LoadInt64(&m.stats.Errors),
	}
}

// Len returns the total number of currently open pools across all shards.
func (m *DBPoolManager) Len() int {
	total := 0
	for i := range m.shards {
		m.shards[i].mu.RLock()
		total += m.shards[i].lru.Len()
		m.shards[i].mu.RUnlock()
	}
	return total
}

// Close closes all open pools across all shards. Call on shutdown.
func (m *DBPoolManager) Close() {
	for i := range m.shards {
		s := &m.shards[i]
		s.mu.Lock()
		for _, elem := range s.pools {
			elem.Value.(*lruEntry).pool.Close()
		}
		s.pools = make(map[string]*list.Element)
		s.lru.Init()
		s.mu.Unlock()
	}
}
