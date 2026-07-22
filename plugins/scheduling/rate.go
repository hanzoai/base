package scheduling

import (
	"errors"
	"sync"
	"time"

	"github.com/hanzoai/base/core"
)

// errSlotTaken signals a lost availability race inside the booking transaction.
var errSlotTaken = errors.New("slot taken")

// limiter is a fixed-window per-key request counter — a minimal throttle bounding
// abuse of the unauthenticated /book route (keyed per client IP and per host).
type limiter struct {
	mu     sync.Mutex
	window time.Duration
	max    int
	hits   map[string]*window
}

type window struct {
	n     int
	start time.Time
}

func newLimiter(w time.Duration, max int) *limiter {
	return &limiter{window: w, max: max, hits: map[string]*window{}}
}

// allow records a hit for key and reports whether it is within the window budget.
func (l *limiter) allow(key string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	c := l.hits[key]
	if c == nil || now.Sub(c.start) >= l.window {
		if len(l.hits) >= 8192 {
			l.prune(now)
		}
		l.hits[key] = &window{n: 1, start: now}
		return true
	}
	if c.n >= l.max {
		return false
	}
	c.n++
	return true
}

// prune drops expired windows so the map can't grow unbounded.
func (l *limiter) prune(now time.Time) {
	for k, c := range l.hits {
		if now.Sub(c.start) >= l.window {
			delete(l.hits, k)
		}
	}
}

// allow gates a booking request: within both the per-IP and per-host budgets.
func (p *plugin) allow(e *core.RequestEvent, hostKey string) bool {
	now := time.Now()
	return p.ipLimit.allow("ip:"+e.RealIP(), now) && p.hostLimit.allow(hostKey, now)
}
