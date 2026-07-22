package calendar

import (
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/hanzoai/base/core"
)

// errSlotTaken signals a lost availability race inside the booking transaction.
var errSlotTaken = errors.New("slot taken")

// limiterMaxKeys is the HARD cap on distinct keys a limiter retains. allow() never
// grows the map past it: when full it prunes expired windows and, if still full,
// evicts the oldest before inserting. This bounds memory when an attacker rotates
// keys (X-Forwarded-For or owner) faster than the window expires — pruning expired
// windows alone does not (that was the bug).
const limiterMaxKeys = 8192

// limiter is a fixed-window per-key request counter — a minimal throttle bounding
// abuse of the unauthenticated routes (keyed per client IP, per host, per handle).
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
// The key map is hard-bounded at limiterMaxKeys.
func (l *limiter) allow(key string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if c := l.hits[key]; c != nil && now.Sub(c.start) < l.window {
		if c.n >= l.max {
			return false
		}
		c.n++
		return true
	}
	// New key or a fresh window: hard-bound the map before inserting so key rotation
	// can't grow it without limit.
	if len(l.hits) >= limiterMaxKeys {
		l.prune(now)
		if len(l.hits) >= limiterMaxKeys {
			l.evictOldest()
		}
	}
	l.hits[key] = &window{n: 1, start: now}
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

// evictOldest drops the single oldest window, keeping the map within its hard cap
// when it is full of non-expired keys (active key-rotation attack).
func (l *limiter) evictOldest() {
	var victim string
	var oldest time.Time
	first := true
	for k, c := range l.hits {
		if first || c.start.Before(oldest) {
			oldest, victim, first = c.start, k, false
		}
	}
	if !first {
		delete(l.hits, victim)
	}
}

// clientIP returns the caller's address for rate-limit keying. Behind the ingress,
// e.RealIP() falls back to the direct socket peer (the ingress pod) unless Base's
// TrustedProxy headers are configured — which would key every booker to one IP and
// defeat the per-IP budget. So prefer the forwarded client address the ingress sets.
// This header is spoofable on its own, which is why the per-host budget is the hard
// bound; the per-IP budget is defense-in-depth for honest clients.
func clientIP(e *core.RequestEvent) string {
	if xff := e.Request.Header.Get("X-Forwarded-For"); xff != "" {
		if i := strings.IndexByte(xff, ','); i >= 0 {
			xff = xff[:i]
		}
		if ip := strings.TrimSpace(xff); ip != "" {
			return ip
		}
	}
	if xr := strings.TrimSpace(e.Request.Header.Get("X-Real-IP")); xr != "" {
		return xr
	}
	return e.RealIP()
}

// allow gates a booking request: within both the per-IP and per-host budgets.
func (p *plugin) allow(e *core.RequestEvent, hostKey string) bool {
	now := time.Now()
	return p.ipLimit.allow("ip:"+clientIP(e), now) && p.hostLimit.allow(hostKey, now)
}

// allowRead gates a public read/reserve by a lightweight per-IP budget. The per-IP
// key is spoofable via X-Forwarded-For, so it is defense-in-depth for honest
// clients; the hard bounds on the read path are the clamped slot window, the capped
// slot generation and the hard-bounded maps.
func (p *plugin) allowRead(e *core.RequestEvent) bool {
	return p.readIPLimit.allow("ip:"+clientIP(e), time.Now())
}

// allowReadHandle additionally applies a non-spoofable per-handle budget, used by
// the handle-scoped reads (public event, slots).
func (p *plugin) allowReadHandle(e *core.RequestEvent, handle string) bool {
	now := time.Now()
	return p.readIPLimit.allow("ip:"+clientIP(e), now) && p.readHandleLimit.allow("handle:"+handle, now)
}
