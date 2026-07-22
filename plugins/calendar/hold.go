package calendar

import (
	"sync"
	"time"

	"github.com/hanzoai/base/tools/security"
)

// holds is a small TTL registry of advisory slot reservations. A booker "reserves"
// a slot the moment they pick it (Cal's useReserveSlot) so it disappears from other
// bookers' availability listings while they fill in the form. Holds are process-
// local and best-effort UX only: the authoritative anti-double-book is the
// transactional isOpenSlot re-check plus the partial unique index in book(), so a
// missed, expired or lost hold can never cause a double-booking. The map is bounded
// and self-pruning so it can't grow without limit under reservation spam.
type holds struct {
	mu    sync.Mutex
	ttl   time.Duration
	byUID map[string]held
}

type held struct {
	eventTypeID string
	start       time.Time
	exp         time.Time
}

func newHolds(ttl time.Duration) *holds {
	return &holds{ttl: ttl, byUID: map[string]held{}}
}

// reserve records a hold on (eventTypeID, start) and returns its opaque uid.
func (h *holds) reserve(eventTypeID string, start time.Time) string {
	uid := security.RandomString(20)
	now := time.Now()
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.byUID) >= 8192 {
		h.prune(now)
	}
	h.byUID[uid] = held{eventTypeID: eventTypeID, start: start.UTC(), exp: now.Add(h.ttl)}
	return uid
}

// release drops a hold by its reservation uid (Cal's useDeleteSelectedSlot).
func (h *holds) release(uid string) {
	if uid == "" {
		return
	}
	h.mu.Lock()
	delete(h.byUID, uid)
	h.mu.Unlock()
}

// releaseStart drops every hold on (eventTypeID, start) — the slot just got booked.
func (h *holds) releaseStart(eventTypeID string, start time.Time) {
	start = start.UTC()
	h.mu.Lock()
	defer h.mu.Unlock()
	for uid, v := range h.byUID {
		if v.eventTypeID == eventTypeID && v.start.Equal(start) {
			delete(h.byUID, uid)
		}
	}
}

// activeStarts returns the currently-held (non-expired) slot starts for an event
// type, pruning expired entries as it goes.
func (h *holds) activeStarts(eventTypeID string) []time.Time {
	now := time.Now()
	h.mu.Lock()
	defer h.mu.Unlock()
	var out []time.Time
	for uid, v := range h.byUID {
		if now.After(v.exp) {
			delete(h.byUID, uid)
			continue
		}
		if v.eventTypeID == eventTypeID {
			out = append(out, v.start)
		}
	}
	return out
}

// prune drops expired holds so the map can't grow unbounded.
func (h *holds) prune(now time.Time) {
	for uid, v := range h.byUID {
		if now.After(v.exp) {
			delete(h.byUID, uid)
		}
	}
}
