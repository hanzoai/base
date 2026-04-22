package network

import (
	"crypto/sha256"
	"encoding/binary"
	"sort"
	"sync"
)

// router is the consistent-hash ring over live members. For a shardID the
// ring returns a deterministic ordered list of members — the first is the
// writer, the first `replication` are the shard's subset.
//
// Member churn: rebuild is cheap (sort a small slice). In k8s the operator
// keeps the BASE_PEERS list current and we rebuild on a pod-id change.
type router struct {
	mu          sync.RWMutex
	members     []NodeID
	replication int
}

func newRouter(members []NodeID, replication int) *router {
	r := &router{replication: replication}
	r.setMembers(members)
	return r
}

func (r *router) setMembers(members []NodeID) {
	out := append([]NodeID(nil), members...)
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	r.mu.Lock()
	r.members = out
	r.mu.Unlock()
}

// ownerOf returns the writer member for shardID.
func (r *router) ownerOf(shardID string) NodeID {
	ms := r.membersFor(shardID)
	if len(ms) == 0 {
		return ""
	}
	return ms[0]
}

// membersFor returns up to `replication` members for shardID, ordered by
// rendezvous hash. Deterministic: every node computes the same list from
// the same member set.
func (r *router) membersFor(shardID string) []NodeID {
	r.mu.RLock()
	members := r.members
	n := r.replication
	r.mu.RUnlock()

	if len(members) == 0 {
		return nil
	}
	if n > len(members) {
		n = len(members)
	}

	scored := make([]scored, 0, len(members))
	for _, m := range members {
		scored = append(scored, scored0(shardID, m))
	}
	sort.Slice(scored, func(i, j int) bool { return scored[i].score < scored[j].score })

	out := make([]NodeID, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, scored[i].id)
	}
	return out
}

type scored struct {
	id    NodeID
	score uint64
}

// scored0 computes the rendezvous score (lower = preferred) as the first
// 8 bytes of SHA-256(shardID || member). Classic HRW hashing.
func scored0(shardID string, m NodeID) scored {
	h := sha256.New()
	h.Write([]byte(shardID))
	h.Write([]byte{0})
	h.Write([]byte(m))
	sum := h.Sum(nil)
	return scored{id: m, score: binary.BigEndian.Uint64(sum[:8])}
}
