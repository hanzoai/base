package platform

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/hanzoai/base/core"
)

func testPoolManager(t *testing.T, maxPools int) (*DBPoolManager, string) {
	t.Helper()
	dir := t.TempDir()
	m := NewDBPoolManager(DBPoolConfig{
		MaxPools:  maxPools,
		ReadConns: 4,
		Connect:   core.DefaultDBConnect,
	})
	t.Cleanup(func() { m.Close() })
	return m, dir
}

func dbPath(dir string, i int) string {
	return filepath.Join(dir, fmt.Sprintf("org_%d", i), "data.db")
}

func TestPoolManager_GetAndRelease(t *testing.T) {
	m, dir := testPoolManager(t, 10)

	p, err := m.Get(dbPath(dir, 1))
	if err != nil {
		t.Fatal(err)
	}
	if p.Path == "" {
		t.Fatal("pool path is empty")
	}
	if !p.InUse() {
		t.Fatal("pool should be in use after Get")
	}

	// Verify we can execute SQL on both pools
	_, err = p.Concurrent.NewQuery("CREATE TABLE IF NOT EXISTS test (id INTEGER PRIMARY KEY, val TEXT)").Execute()
	if err != nil {
		t.Fatalf("concurrent exec: %v", err)
	}
	_, err = p.Nonconcurrent.NewQuery("INSERT INTO test (val) VALUES ('hello')").Execute()
	if err != nil {
		t.Fatalf("nonconcurrent exec: %v", err)
	}

	p.Release()
	if p.InUse() {
		t.Fatal("pool should not be in use after Release")
	}

	// Second Get should be a cache hit
	p2, err := m.Get(dbPath(dir, 1))
	if err != nil {
		t.Fatal(err)
	}
	defer p2.Release()

	stats := m.Stats()
	if stats.Hits != 1 {
		t.Fatalf("expected 1 hit, got %d", stats.Hits)
	}
	if stats.Misses != 1 {
		t.Fatalf("expected 1 miss, got %d", stats.Misses)
	}
}

func TestPoolManager_LRUEviction(t *testing.T) {
	// Use 1 shard to make eviction deterministic
	dir := t.TempDir()
	m := NewDBPoolManager(DBPoolConfig{
		MaxPools:  3,
		ReadConns: 4,
		NumShards: 1, // single shard for deterministic eviction
		Connect:   core.DefaultDBConnect,
	})
	t.Cleanup(func() { m.Close() })

	// Open 3 pools and write data so the DB files exist on disk
	for i := 0; i < 3; i++ {
		p, err := m.Get(dbPath(dir, i))
		if err != nil {
			t.Fatal(err)
		}
		p.Nonconcurrent.NewQuery("CREATE TABLE IF NOT EXISTS t (id INTEGER)").Execute()
		p.Release()
	}
	if m.Len() != 3 {
		t.Fatalf("expected 3 pools, got %d", m.Len())
	}

	// Opening a 4th should evict the LRU (org_0)
	p, err := m.Get(dbPath(dir, 99))
	if err != nil {
		t.Fatal(err)
	}
	p.Release()

	if m.Len() != 3 {
		t.Fatalf("expected 3 pools after eviction, got %d", m.Len())
	}
	if m.Stats().Evictions < 1 {
		t.Fatalf("expected at least 1 eviction, got %d", m.Stats().Evictions)
	}

	// The evicted pool's DB file should still exist on disk
	if _, err := os.Stat(dbPath(dir, 0)); os.IsNotExist(err) {
		t.Fatal("evicted DB file was deleted (shouldn't be)")
	}
}

func TestPoolManager_NoEvictInUse(t *testing.T) {
	dir := t.TempDir()
	m := NewDBPoolManager(DBPoolConfig{MaxPools: 2, ReadConns: 4, NumShards: 1, Connect: core.DefaultDBConnect})
	t.Cleanup(func() { m.Close() })

	// Open 2 pools and hold references
	p0, _ := m.Get(dbPath(dir, 0))
	p1, _ := m.Get(dbPath(dir, 1))

	// Opening a 3rd should NOT evict because both are in use
	p2, err := m.Get(dbPath(dir, 2))
	if err != nil {
		t.Fatal(err)
	}

	// Should be over-capacity temporarily
	if m.Len() != 3 {
		t.Fatalf("expected 3 pools (over-capacity), got %d", m.Len())
	}

	p0.Release()
	p1.Release()
	p2.Release()
}

func TestPoolManager_ConcurrentAccess(t *testing.T) {
	m, dir := testPoolManager(t, 64)
	var wg sync.WaitGroup
	var errors int64

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			path := dbPath(dir, id%20) // 20 unique DBs, 100 goroutines
			p, err := m.Get(path)
			if err != nil {
				atomic.AddInt64(&errors, 1)
				return
			}
			// Simulate work — read + write
			p.Concurrent.NewQuery("SELECT 1").Execute()
			p.Nonconcurrent.NewQuery("CREATE TABLE IF NOT EXISTS t (id INTEGER)").Execute()
			p.Release()
		}(i)
	}
	wg.Wait()

	if errors > 0 {
		t.Fatalf("%d goroutines failed to get pools", errors)
	}

	stats := m.Stats()
	t.Logf("stats: hits=%d misses=%d opens=%d evictions=%d",
		stats.Hits, stats.Misses, stats.Opens, stats.Evictions)

	if stats.Misses+stats.Hits != 100 {
		t.Fatalf("expected 100 total ops, got %d", stats.Misses+stats.Hits)
	}
}

func TestPoolManager_Close(t *testing.T) {
	m, dir := testPoolManager(t, 10)

	for i := 0; i < 5; i++ {
		p, _ := m.Get(dbPath(dir, i))
		p.Release()
	}
	if m.Len() != 5 {
		t.Fatalf("expected 5 pools, got %d", m.Len())
	}

	m.Close()
	if m.Len() != 0 {
		t.Fatalf("expected 0 pools after Close, got %d", m.Len())
	}
}

func TestPoolManager_DataPersists(t *testing.T) {
	m, dir := testPoolManager(t, 10)
	path := dbPath(dir, 1)

	// Write data
	p, _ := m.Get(path)
	p.Nonconcurrent.NewQuery("CREATE TABLE kv (k TEXT PRIMARY KEY, v TEXT)").Execute()
	p.Nonconcurrent.NewQuery("INSERT INTO kv (k, v) VALUES ('foo', 'bar')").Execute()
	p.Release()

	// Force evict by closing the manager
	m.Close()

	// Re-open from same path — data should persist
	m2 := NewDBPoolManager(DBPoolConfig{MaxPools: 10, Connect: core.DefaultDBConnect})
	defer m2.Close()

	p2, err := m2.Get(path)
	if err != nil {
		t.Fatal(err)
	}
	defer p2.Release()

	var val string
	row := p2.Concurrent.NewQuery("SELECT v FROM kv WHERE k = 'foo'").Row(&val)
	if row != nil {
		t.Fatalf("query failed: %v", row)
	}
	if val != "bar" {
		t.Fatalf("expected 'bar', got %q", val)
	}
}

// --- Benchmarks ---

func BenchmarkPoolManager_CacheHit(b *testing.B) {
	dir := b.TempDir()
	m := NewDBPoolManager(DBPoolConfig{MaxPools: 256, Connect: core.DefaultDBConnect})
	defer m.Close()

	path := filepath.Join(dir, "bench", "data.db")
	p, _ := m.Get(path)
	p.Release()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		p, _ := m.Get(path)
		p.Release()
	}
}

func BenchmarkPoolManager_CacheMiss(b *testing.B) {
	dir := b.TempDir()
	m := NewDBPoolManager(DBPoolConfig{MaxPools: b.N + 1, Connect: core.DefaultDBConnect})
	defer m.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		path := filepath.Join(dir, fmt.Sprintf("org_%d", i), "data.db")
		p, err := m.Get(path)
		if err != nil {
			b.Fatal(err)
		}
		p.Release()
	}
}

func BenchmarkPoolManager_ConcurrentHit(b *testing.B) {
	dir := b.TempDir()
	m := NewDBPoolManager(DBPoolConfig{MaxPools: 256, ReadConns: 8, Connect: core.DefaultDBConnect})
	defer m.Close()

	// Pre-populate 10 pools
	paths := make([]string, 10)
	for i := range paths {
		paths[i] = filepath.Join(dir, fmt.Sprintf("org_%d", i), "data.db")
		p, _ := m.Get(paths[i])
		// Create a table so queries work
		p.Nonconcurrent.NewQuery("CREATE TABLE IF NOT EXISTS kv (k TEXT, v TEXT)").Execute()
		p.Release()
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			path := paths[i%10]
			p, _ := m.Get(path)
			p.Concurrent.NewQuery("SELECT 1").Execute()
			p.Release()
			i++
		}
	})
}

func BenchmarkPoolManager_ReadWrite(b *testing.B) {
	dir := b.TempDir()
	m := NewDBPoolManager(DBPoolConfig{MaxPools: 64, ReadConns: 4, Connect: core.DefaultDBConnect})
	defer m.Close()

	path := filepath.Join(dir, "rw_bench", "data.db")
	p, _ := m.Get(path)
	p.Nonconcurrent.NewQuery("CREATE TABLE kv (k TEXT PRIMARY KEY, v TEXT)").Execute()
	p.Release()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			p, _ := m.Get(path)
			if i%5 == 0 {
				// 20% writes
				p.Nonconcurrent.NewQuery(fmt.Sprintf("INSERT OR REPLACE INTO kv (k, v) VALUES ('k%d', 'v%d')", i, i)).Execute()
			} else {
				// 80% reads
				var v string
				p.Concurrent.NewQuery("SELECT v FROM kv WHERE k = 'k0'").Row(&v)
			}
			p.Release()
			i++
		}
	})
}

func BenchmarkPoolManager_ManyOrgs(b *testing.B) {
	dir := b.TempDir()
	m := NewDBPoolManager(DBPoolConfig{MaxPools: 128, Connect: core.DefaultDBConnect})
	defer m.Close()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			// Simulate 500 orgs hitting a 128-pool cache → forces eviction
			path := filepath.Join(dir, fmt.Sprintf("org_%d", i%500), "data.db")
			p, err := m.Get(path)
			if err != nil {
				b.Fatal(err)
			}
			p.Release()
			i++
		}
	})
}
