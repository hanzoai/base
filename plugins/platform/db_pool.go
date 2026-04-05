package platform

import (
	"container/list"
	"fmt"
	"os"
	"sync"
	"sync/atomic"

	"github.com/hanzoai/base/core"
	"github.com/pocketbase/dbx"
)

// DBPool holds a dual-pool connection pair for one SQLite database.
// Reads use the concurrent pool (many open conns), writes use the
// nonconcurrent pool (single conn) to avoid SQLITE_BUSY.
type DBPool struct {
	Path          string
	Concurrent    *dbx.DB // reads — MaxOpenConns=20
	Nonconcurrent *dbx.DB // writes — MaxOpenConns=1
	refCount      int64
}

func (p *DBPool) Acquire() { atomic.AddInt64(&p.refCount, 1) }
func (p *DBPool) Release() { atomic.AddInt64(&p.refCount, -1) }
func (p *DBPool) InUse() bool { return atomic.LoadInt64(&p.refCount) > 0 }

func (p *DBPool) Close() error {
	var errs []error
	if p.Concurrent != nil {
		if err := p.Concurrent.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	if p.Nonconcurrent != nil {
		if err := p.Nonconcurrent.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("close pool %s: %v", p.Path, errs)
	}
	return nil
}

// DBPoolManager manages an LRU cache of SQLite connection pools.
// Any service instance can serve any org/user by loading the right
// SQLite file — services are fully stateless.
type DBPoolManager struct {
	mu       sync.Mutex
	pools    map[string]*list.Element // key → LRU element
	lru      *list.List
	maxPools int
	connect  core.DBConnectFunc
}

type lruEntry struct {
	key  string
	pool *DBPool
}

// NewDBPoolManager creates a pool manager with the given capacity.
// connect is the SQLite connection function (typically core.DefaultDBConnect).
func NewDBPoolManager(maxPools int, connect core.DBConnectFunc) *DBPoolManager {
	if maxPools <= 0 {
		maxPools = 256
	}
	return &DBPoolManager{
		pools:    make(map[string]*list.Element),
		lru:      list.New(),
		maxPools: maxPools,
		connect:  connect,
	}
}

// Get returns a connection pool for the given database path.
// If no pool exists, one is created. The caller must call pool.Release()
// when done to allow eviction.
func (m *DBPoolManager) Get(dbPath string) (*DBPool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Hit: move to front of LRU
	if elem, ok := m.pools[dbPath]; ok {
		m.lru.MoveToFront(elem)
		pool := elem.Value.(*lruEntry).pool
		pool.Acquire()
		return pool, nil
	}

	// Miss: open new pool
	pool, err := m.openPool(dbPath)
	if err != nil {
		return nil, err
	}

	// Evict if at capacity
	for m.lru.Len() >= m.maxPools {
		if !m.evictOne() {
			break // all pools in use, allow over-capacity temporarily
		}
	}

	entry := &lruEntry{key: dbPath, pool: pool}
	elem := m.lru.PushFront(entry)
	m.pools[dbPath] = elem

	pool.Acquire()
	return pool, nil
}

func (m *DBPoolManager) openPool(dbPath string) (*DBPool, error) {
	// Ensure directory exists
	if err := os.MkdirAll(dbPath[:len(dbPath)-len("/"+dbPath[1+findLastSlash(dbPath):])], 0700); err != nil {
		// fallback: try opening anyway
	}

	concurrent, err := m.connect(dbPath)
	if err != nil {
		return nil, fmt.Errorf("open concurrent db %s: %w", dbPath, err)
	}
	concurrent.DB().SetMaxOpenConns(20)
	concurrent.DB().SetMaxIdleConns(5)

	nonconcurrent, err := m.connect(dbPath)
	if err != nil {
		concurrent.Close()
		return nil, fmt.Errorf("open nonconcurrent db %s: %w", dbPath, err)
	}
	nonconcurrent.DB().SetMaxOpenConns(1)
	nonconcurrent.DB().SetMaxIdleConns(1)

	return &DBPool{
		Path:          dbPath,
		Concurrent:    concurrent,
		Nonconcurrent: nonconcurrent,
	}, nil
}

func findLastSlash(s string) int {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == '/' {
			return i
		}
	}
	return -1
}

func (m *DBPoolManager) evictOne() bool {
	for elem := m.lru.Back(); elem != nil; elem = elem.Prev() {
		entry := elem.Value.(*lruEntry)
		if !entry.pool.InUse() {
			m.lru.Remove(elem)
			delete(m.pools, entry.key)
			entry.pool.Close()
			return true
		}
	}
	return false
}

// Stats returns the number of open pools.
func (m *DBPoolManager) Stats() (open, capacity int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.lru.Len(), m.maxPools
}

// Close closes all open pools.
func (m *DBPoolManager) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, elem := range m.pools {
		elem.Value.(*lruEntry).pool.Close()
	}
	m.pools = make(map[string]*list.Element)
	m.lru.Init()
}
