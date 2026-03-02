package cache

import (
	"testing"
	"time"
)

func TestNewLRU_BasicOps(t *testing.T) {
	c := NewLRU[string, int](3)

	c.Put("a", 1)
	c.Put("b", 2)
	c.Put("c", 3)

	if v, ok := c.Get("a"); !ok || v != 1 {
		t.Fatalf("Get(a) = %d, %v; want 1, true", v, ok)
	}
	if c.Len() != 3 {
		t.Fatalf("Len() = %d; want 3", c.Len())
	}

	// Exceed capacity — "b" is LRU (we accessed "a" above).
	c.Put("d", 4)
	if _, ok := c.Get("b"); ok {
		t.Fatal("Get(b) should miss after eviction")
	}
	if c.Len() != 3 {
		t.Fatalf("Len() = %d; want 3 after eviction", c.Len())
	}
}

func TestNewLRU_Evict(t *testing.T) {
	c := NewLRU[string, int](10)
	c.Put("x", 42)

	c.Evict("x")
	if _, ok := c.Get("x"); ok {
		t.Fatal("Get(x) should miss after Evict")
	}
}

func TestNewLRU_Flush(t *testing.T) {
	c := NewLRU[string, int](10)
	c.Put("a", 1)
	c.Put("b", 2)

	c.Flush()
	if c.Len() != 0 {
		t.Fatalf("Len() = %d after Flush; want 0", c.Len())
	}
}

func TestEmpty(t *testing.T) {
	c := Empty[string, int]()

	c.Put("a", 1)
	if _, ok := c.Get("a"); ok {
		t.Fatal("Empty cache should always miss")
	}
	if c.Len() != 0 {
		t.Fatalf("Empty Len() = %d; want 0", c.Len())
	}
	if c.PortionFilled() != 0 {
		t.Fatalf("Empty PortionFilled() = %f; want 0", c.PortionFilled())
	}
}

func TestTTL_BasicOps(t *testing.T) {
	c := NewTTL[string, int](100, time.Hour)

	c.Put("a", 1)
	c.Put("b", 2)

	if v, ok := c.Get("a"); !ok || v != 1 {
		t.Fatalf("Get(a) = %d, %v; want 1, true", v, ok)
	}
	if v, ok := c.Get("b"); !ok || v != 2 {
		t.Fatalf("Get(b) = %d, %v; want 2, true", v, ok)
	}
	if c.Len() != 2 {
		t.Fatalf("Len() = %d; want 2", c.Len())
	}
}

func TestTTL_Expiration(t *testing.T) {
	c := NewTTL[string, int](100, 10*time.Millisecond)

	c.Put("a", 1)

	if _, ok := c.Get("a"); !ok {
		t.Fatal("Get(a) should hit immediately after Put")
	}

	time.Sleep(20 * time.Millisecond)

	if _, ok := c.Get("a"); ok {
		t.Fatal("Get(a) should miss after TTL expires")
	}

	// Evicted on read — len should be 0.
	if c.Len() != 0 {
		t.Fatalf("Len() = %d after expiration; want 0", c.Len())
	}
}

func TestTTL_PutResetsExpiry(t *testing.T) {
	c := NewTTL[string, int](100, 50*time.Millisecond)

	c.Put("a", 1)
	time.Sleep(30 * time.Millisecond)

	// Re-put before expiry to reset TTL.
	c.Put("a", 2)
	time.Sleep(30 * time.Millisecond)

	// 60ms total, but TTL was reset at 30ms, so 30ms since last Put.
	if v, ok := c.Get("a"); !ok || v != 2 {
		t.Fatalf("Get(a) = %d, %v; want 2, true (TTL should have been reset)", v, ok)
	}
}

func TestTTL_Evict(t *testing.T) {
	c := NewTTL[string, int](100, time.Hour)
	c.Put("a", 1)

	c.Evict("a")
	if _, ok := c.Get("a"); ok {
		t.Fatal("Get(a) should miss after Evict")
	}
}

func TestTTL_Flush(t *testing.T) {
	c := NewTTL[string, int](100, time.Hour)
	c.Put("a", 1)
	c.Put("b", 2)

	c.Flush()
	if c.Len() != 0 {
		t.Fatalf("Len() = %d after Flush; want 0", c.Len())
	}
}

func TestTTL_LRUEviction(t *testing.T) {
	c := NewTTL[string, int](2, time.Hour)

	c.Put("a", 1)
	c.Put("b", 2)
	c.Put("c", 3) // evicts "a" (LRU)

	if _, ok := c.Get("a"); ok {
		t.Fatal("Get(a) should miss after LRU eviction")
	}
	if v, ok := c.Get("b"); !ok || v != 2 {
		t.Fatalf("Get(b) = %d, %v; want 2, true", v, ok)
	}
	if v, ok := c.Get("c"); !ok || v != 3 {
		t.Fatalf("Get(c) = %d, %v; want 3, true", v, ok)
	}
}

func TestTTL_MissReturnsZero(t *testing.T) {
	c := NewTTL[string, int](10, time.Hour)

	v, ok := c.Get("nonexistent")
	if ok {
		t.Fatal("Get(nonexistent) should miss")
	}
	if v != 0 {
		t.Fatalf("Get(nonexistent) value = %d; want 0", v)
	}
}
