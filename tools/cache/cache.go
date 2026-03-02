// Package cache provides caching primitives for Hanzo Base, built on
// github.com/luxfi/cache. It re-exports the Cacher interface and LRU
// implementation, and adds a TTL wrapper for time-based expiration.
package cache

import (
	"sync"
	"time"

	lrucache "github.com/luxfi/cache"
)

// Cacher is the cache interface from luxfi/cache.
type Cacher[K comparable, V any] = lrucache.Cacher[K, V]

// NewLRU creates a bounded LRU cache. Thread-safe.
func NewLRU[K comparable, V any](size int) *lrucache.LRU[K, V] {
	return lrucache.NewLRU[K, V](size)
}

// Empty returns a no-op cache that stores nothing.
func Empty[K comparable, V any]() *lrucache.Empty[K, V] {
	return &lrucache.Empty[K, V]{}
}

// ttlEntry wraps a value with its expiration time.
type ttlEntry[V any] struct {
	val       V
	expiresAt time.Time
}

// TTL wraps a Cacher and adds time-based expiration. Entries older than
// the configured TTL are evicted lazily on Get. Put resets the TTL.
//
// The underlying cache controls eviction policy (e.g. LRU) and size bounds.
// TTL only adds the time dimension.
type TTL[K comparable, V any] struct {
	inner lrucache.Cacher[K, ttlEntry[V]]
	ttl   time.Duration
	mu    sync.Mutex
}

// NewTTL creates a TTL-aware LRU cache with the given max size and TTL.
func NewTTL[K comparable, V any](size int, ttl time.Duration) *TTL[K, V] {
	return &TTL[K, V]{
		inner: lrucache.NewLRU[K, ttlEntry[V]](size),
		ttl:   ttl,
	}
}

// Put inserts a value with the current TTL.
func (c *TTL[K, V]) Put(key K, value V) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.inner.Put(key, ttlEntry[V]{val: value, expiresAt: time.Now().Add(c.ttl)})
}

// Get returns the value if it exists and has not expired.
// Expired entries are evicted immediately.
func (c *TTL[K, V]) Get(key K) (V, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.inner.Get(key)
	if !ok {
		var zero V
		return zero, false
	}
	if time.Now().After(entry.expiresAt) {
		c.inner.Evict(key)
		var zero V
		return zero, false
	}
	return entry.val, true
}

// Evict removes a specific key.
func (c *TTL[K, V]) Evict(key K) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.inner.Evict(key)
}

// Flush removes all entries.
func (c *TTL[K, V]) Flush() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.inner.Flush()
}

// Len returns the number of entries (including expired ones not yet evicted).
func (c *TTL[K, V]) Len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.inner.Len()
}

// PortionFilled returns the fraction of the underlying cache that is occupied.
func (c *TTL[K, V]) PortionFilled() float64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.inner.PortionFilled()
}
