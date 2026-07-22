package scheduling

import (
	"errors"
	"strings"
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
