// Copyright (C) 2020-2026, Hanzo AI Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package waitlist

import (
	"sync"
	"time"
)

// slidingLimiter is a tiny in-process sliding-window limiter keyed by
// arbitrary string (IP, email, etc.). It's intentionally minimal: O(N)
// in the count per key, GC on read, no background goroutine. For Base
// instances handling >100 req/s the host should put a real proxy in
// front; this limiter exists to keep a single dev / small SaaS box safe.
type slidingLimiter struct {
	mu     sync.Mutex
	limit  int
	window time.Duration
	hits   map[string][]time.Time
	now    func() time.Time // injectable for tests
}

func newSlidingLimiter(limit int, window time.Duration) *slidingLimiter {
	return &slidingLimiter{
		limit:  limit,
		window: window,
		hits:   make(map[string][]time.Time),
		now:    time.Now,
	}
}

// allow returns true if the key is under the limit and records the hit.
// It also opportunistically GCs the key's slice.
func (l *slidingLimiter) allow(key string) bool {
	if l == nil || l.limit <= 0 {
		return true
	}
	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.now()
	cutoff := now.Add(-l.window)
	hits := l.hits[key]

	// Drop entries older than the window.
	i := 0
	for ; i < len(hits); i++ {
		if hits[i].After(cutoff) {
			break
		}
	}
	if i > 0 {
		hits = hits[i:]
	}

	if len(hits) >= l.limit {
		l.hits[key] = hits
		return false
	}

	l.hits[key] = append(hits, now)
	return true
}
