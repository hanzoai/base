package crdt

import (
	"fmt"
	"sync"
	"time"
)

// NodeID identifies a unique node/client in the distributed system.
type NodeID = string

// Timestamp is a Lamport-style logical clock for ordering operations.
type Timestamp struct {
	Time   int64  `json:"time"`
	NodeID NodeID `json:"nodeId"`
}

// After reports whether t is causally after other.
// Ties are broken by NodeID lexicographic order.
func (t Timestamp) After(other Timestamp) bool {
	if t.Time != other.Time {
		return t.Time > other.Time
	}
	return t.NodeID > other.NodeID
}

// -------------------------------------------------------------------
// GCounter - Grow-only counter
// -------------------------------------------------------------------

// GCounter is a grow-only counter CRDT.
// Each node maintains its own count; the total is the sum of all counts.
type GCounter struct {
	mu    sync.RWMutex
	state map[NodeID]uint64
}

// NewGCounter returns a new empty GCounter.
func NewGCounter() *GCounter {
	return &GCounter{state: make(map[NodeID]uint64)}
}

// Increment adds delta to the count for nodeID.
func (g *GCounter) Increment(nodeID NodeID, delta uint64) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.state[nodeID] += delta
}

// Value returns the total count across all nodes.
func (g *GCounter) Value() uint64 {
	g.mu.RLock()
	defer g.mu.RUnlock()
	var total uint64
	for _, v := range g.state {
		total += v
	}
	return total
}

// Merge merges a remote GCounter into this one by taking the max for each node.
func (g *GCounter) Merge(other *GCounter) {
	other.mu.RLock()
	otherState := make(map[NodeID]uint64, len(other.state))
	for k, v := range other.state {
		otherState[k] = v
	}
	other.mu.RUnlock()

	g.mu.Lock()
	defer g.mu.Unlock()
	for nodeID, count := range otherState {
		if count > g.state[nodeID] {
			g.state[nodeID] = count
		}
	}
}

// State returns a copy of the internal state map.
func (g *GCounter) State() map[NodeID]uint64 {
	g.mu.RLock()
	defer g.mu.RUnlock()
	cp := make(map[NodeID]uint64, len(g.state))
	for k, v := range g.state {
		cp[k] = v
	}
	return cp
}

// -------------------------------------------------------------------
// PNCounter - Positive-Negative counter
// -------------------------------------------------------------------

// PNCounter is a counter that supports both increment and decrement
// by composing two GCounters.
type PNCounter struct {
	pos *GCounter
	neg *GCounter
}

// NewPNCounter returns a new PNCounter.
func NewPNCounter() *PNCounter {
	return &PNCounter{
		pos: NewGCounter(),
		neg: NewGCounter(),
	}
}

// Increment adds delta to the positive counter for nodeID.
func (pn *PNCounter) Increment(nodeID NodeID, delta uint64) {
	pn.pos.Increment(nodeID, delta)
}

// Decrement adds delta to the negative counter for nodeID.
func (pn *PNCounter) Decrement(nodeID NodeID, delta uint64) {
	pn.neg.Increment(nodeID, delta)
}

// Value returns positive - negative as a signed integer.
func (pn *PNCounter) Value() int64 {
	return int64(pn.pos.Value()) - int64(pn.neg.Value())
}

// Merge merges a remote PNCounter into this one.
func (pn *PNCounter) Merge(other *PNCounter) {
	pn.pos.Merge(other.pos)
	pn.neg.Merge(other.neg)
}

// -------------------------------------------------------------------
// LWWRegister - Last-Writer-Wins Register
// -------------------------------------------------------------------

// LWWRegister is a register that resolves concurrent writes by timestamp.
// The most recent write (highest timestamp) wins.
type LWWRegister struct {
	mu        sync.RWMutex
	value     any
	timestamp Timestamp
}

// NewLWWRegister returns a new empty LWWRegister.
func NewLWWRegister() *LWWRegister {
	return &LWWRegister{}
}

// Set updates the register value if the given timestamp is newer.
func (r *LWWRegister) Set(value any, ts Timestamp) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if ts.After(r.timestamp) || r.timestamp == (Timestamp{}) {
		r.value = value
		r.timestamp = ts
	}
}

// Get returns the current value and its timestamp.
func (r *LWWRegister) Get() (any, Timestamp) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.value, r.timestamp
}

// Merge merges a remote register into this one.
func (r *LWWRegister) Merge(other *LWWRegister) {
	otherVal, otherTS := other.Get()
	r.Set(otherVal, otherTS)
}

// -------------------------------------------------------------------
// ORSet - Observed-Remove Set
// -------------------------------------------------------------------

// ORSetElement tracks the unique tags for an element.
// An element is in the set if it has at least one tag not in the tombstone set.
type ORSetElement struct {
	Value any
}

// ORSet is an observed-remove set CRDT.
// Elements can be added and removed without conflicts.
// Concurrent add + remove: the add wins (add-wins semantics).
type ORSet struct {
	mu     sync.RWMutex
	// elements maps a string key to a map of unique tags.
	// Each add generates a unique tag; remove deletes all known tags.
	elems  map[string]map[string]any // key -> {tag -> value}
	tagSeq uint64
	nodeID NodeID
}

// NewORSet returns a new ORSet for the given node.
func NewORSet(nodeID NodeID) *ORSet {
	return &ORSet{
		elems:  make(map[string]map[string]any),
		nodeID: nodeID,
	}
}

// Add adds an element to the set. Returns the generated unique tag.
func (s *ORSet) Add(key string, value any) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tagSeq++
	tag := fmt.Sprintf("%s:%d:%d", s.nodeID, time.Now().UnixNano(), s.tagSeq)
	if s.elems[key] == nil {
		s.elems[key] = make(map[string]any)
	}
	s.elems[key][tag] = value
	return tag
}

// Remove removes an element by key, removing all observed tags.
func (s *ORSet) Remove(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.elems, key)
}

// Contains checks if key is present in the set.
func (s *ORSet) Contains(key string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	tags := s.elems[key]
	return len(tags) > 0
}

// Elements returns all keys currently in the set with one representative value each.
func (s *ORSet) Elements() map[string]any {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make(map[string]any, len(s.elems))
	for key, tags := range s.elems {
		if len(tags) == 0 {
			continue
		}
		for _, v := range tags {
			result[key] = v
			break
		}
	}
	return result
}

// Merge merges a remote ORSet into this one (union of tags).
func (s *ORSet) Merge(other *ORSet) {
	other.mu.RLock()
	otherElems := make(map[string]map[string]any, len(other.elems))
	for k, tags := range other.elems {
		cp := make(map[string]any, len(tags))
		for t, v := range tags {
			cp[t] = v
		}
		otherElems[k] = cp
	}
	other.mu.RUnlock()

	s.mu.Lock()
	defer s.mu.Unlock()
	for key, remoteTags := range otherElems {
		if s.elems[key] == nil {
			s.elems[key] = make(map[string]any)
		}
		for tag, val := range remoteTags {
			s.elems[key][tag] = val
		}
	}
}

// RawState returns a deep copy of internal tag state for serialization.
func (s *ORSet) RawState() map[string]map[string]any {
	s.mu.RLock()
	defer s.mu.RUnlock()
	cp := make(map[string]map[string]any, len(s.elems))
	for k, tags := range s.elems {
		tagCp := make(map[string]any, len(tags))
		for t, v := range tags {
			tagCp[t] = v
		}
		cp[k] = tagCp
	}
	return cp
}

// -------------------------------------------------------------------
// MVRegister - Multi-Value Register
// -------------------------------------------------------------------

// MVEntry is a single versioned value in an MVRegister.
type MVEntry struct {
	Value     any       `json:"value"`
	Timestamp Timestamp `json:"timestamp"`
}

// MVRegister preserves all concurrent writes rather than picking one winner.
// It maintains a set of (value, timestamp) pairs. When a new value is set,
// it replaces all entries that are causally dominated.
type MVRegister struct {
	mu      sync.RWMutex
	entries []MVEntry
}

// NewMVRegister returns a new empty MVRegister.
func NewMVRegister() *MVRegister {
	return &MVRegister{}
}

// Set adds a new value, removing all entries whose timestamp is dominated by ts.
func (r *MVRegister) Set(value any, ts Timestamp) {
	r.mu.Lock()
	defer r.mu.Unlock()

	// remove dominated entries
	kept := r.entries[:0]
	for _, e := range r.entries {
		if !ts.After(e.Timestamp) {
			kept = append(kept, e)
		}
	}
	r.entries = append(kept, MVEntry{Value: value, Timestamp: ts})
}

// Get returns all concurrent values.
func (r *MVRegister) Get() []MVEntry {
	r.mu.RLock()
	defer r.mu.RUnlock()
	cp := make([]MVEntry, len(r.entries))
	copy(cp, r.entries)
	return cp
}

// Merge merges a remote MVRegister. The result is the union of non-dominated entries.
func (r *MVRegister) Merge(other *MVRegister) {
	otherEntries := other.Get()

	r.mu.Lock()
	defer r.mu.Unlock()

	// collect all entries
	all := make([]MVEntry, 0, len(r.entries)+len(otherEntries))
	all = append(all, r.entries...)
	all = append(all, otherEntries...)

	// remove dominated entries: keep only those not dominated by any other
	kept := make([]MVEntry, 0, len(all))
	for i, a := range all {
		dominated := false
		for j, b := range all {
			if i != j && b.Timestamp.After(a.Timestamp) {
				dominated = true
				break
			}
		}
		if !dominated {
			// deduplicate by exact timestamp match
			dup := false
			for _, k := range kept {
				if k.Timestamp == a.Timestamp {
					dup = true
					break
				}
			}
			if !dup {
				kept = append(kept, a)
			}
		}
	}

	r.entries = kept
}
