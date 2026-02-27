package crdt

import (
	"fmt"
	"strings"
	"sync"
)

// OpType identifies an RGA operation kind.
type OpType uint8

const (
	OpInsert OpType = iota
	OpDelete
)

// RGAOp represents a single RGA operation for replication.
type RGAOp struct {
	Type      OpType    `json:"type"`
	ID        RGAID     `json:"id"`
	ParentID  RGAID     `json:"parentId"` // for insert: the ID of the node this is inserted after
	Char      rune      `json:"char"`
	Timestamp Timestamp `json:"timestamp"`
}

// RGAID uniquely identifies a character node in the RGA.
type RGAID struct {
	Seq    uint64 `json:"seq"`
	NodeID NodeID `json:"nodeId"`
}

// IsZero reports whether the ID is the zero/sentinel value.
func (id RGAID) IsZero() bool {
	return id.Seq == 0 && id.NodeID == ""
}

// String returns a human-readable ID.
func (id RGAID) String() string {
	return fmt.Sprintf("%s:%d", id.NodeID, id.Seq)
}

// After reports whether id should be ordered after other (for tie-breaking).
func (id RGAID) After(other RGAID) bool {
	if id.Seq != other.Seq {
		return id.Seq > other.Seq
	}
	return id.NodeID > other.NodeID
}

// rgaNode is an internal linked-list node in the RGA.
type rgaNode struct {
	id      RGAID
	char    rune
	deleted bool
	next    *rgaNode
}

// RGA (Replicated Growable Array) is a CRDT for collaborative text editing.
// It maintains a linked list of character nodes with unique IDs that allow
// concurrent inserts to be ordered deterministically.
type RGA struct {
	mu       sync.RWMutex
	head     *rgaNode          // sentinel head node
	index    map[string]*rgaNode // id.String() -> node for O(1) lookup
	seq      uint64
	nodeID   NodeID
	pending  []RGAOp           // pending ops for sync
}

// NewRGA creates a new RGA instance for the given node.
func NewRGA(nodeID NodeID) *RGA {
	sentinel := &rgaNode{id: RGAID{}} // zero ID sentinel
	return &RGA{
		head:   sentinel,
		index:  map[string]*rgaNode{sentinel.id.String(): sentinel},
		nodeID: nodeID,
	}
}

// nextID generates the next unique ID for this node.
func (r *RGA) nextID() RGAID {
	r.seq++
	return RGAID{Seq: r.seq, NodeID: r.nodeID}
}

// Insert inserts a character after the given position (0-based index).
// Position -1 means insert at the beginning.
// Returns the generated operation for replication.
func (r *RGA) Insert(position int, ch rune) RGAOp {
	r.mu.Lock()
	defer r.mu.Unlock()

	parentNode := r.nodeAtVisible(position)
	id := r.nextID()

	newNode := &rgaNode{id: id, char: ch}
	r.insertAfter(parentNode, newNode)

	op := RGAOp{
		Type:      OpInsert,
		ID:        id,
		ParentID:  parentNode.id,
		Char:      ch,
		Timestamp: Timestamp{Time: int64(id.Seq), NodeID: r.nodeID},
	}
	r.pending = append(r.pending, op)
	return op
}

// Delete deletes the character at the given visible position (0-based).
// Returns the generated operation for replication.
func (r *RGA) Delete(position int) (RGAOp, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	node := r.visibleNodeAt(position)
	if node == nil {
		return RGAOp{}, fmt.Errorf("position %d out of range", position)
	}

	node.deleted = true

	op := RGAOp{
		Type:      OpDelete,
		ID:        node.id,
		Timestamp: Timestamp{Time: int64(r.seq), NodeID: r.nodeID},
	}
	r.pending = append(r.pending, op)
	return op, nil
}

// ApplyOp applies a remote operation. Returns an error if the operation
// references a parent that doesn't exist (operation should be retried later).
func (r *RGA) ApplyOp(op RGAOp) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	switch op.Type {
	case OpInsert:
		// check if already applied
		if _, exists := r.index[op.ID.String()]; exists {
			return nil // idempotent
		}

		parentKey := op.ParentID.String()
		parent, ok := r.index[parentKey]
		if !ok {
			return fmt.Errorf("parent %s not found", parentKey)
		}

		newNode := &rgaNode{id: op.ID, char: op.Char}
		r.insertAfter(parent, newNode)

		// update local seq if remote is ahead
		if op.ID.Seq > r.seq {
			r.seq = op.ID.Seq
		}

	case OpDelete:
		key := op.ID.String()
		node, ok := r.index[key]
		if !ok {
			return fmt.Errorf("node %s not found for delete", key)
		}
		node.deleted = true
	}

	return nil
}

// insertAfter inserts newNode after parent, respecting ordering among siblings.
// Among children of the same parent, nodes with higher IDs come first (left).
func (r *RGA) insertAfter(parent, newNode *rgaNode) {
	// find the correct position: skip nodes that have higher IDs
	// (they were inserted after the same parent but should appear first).
	cursor := parent
	for cursor.next != nil && cursor.next.id.After(newNode.id) {
		cursor = cursor.next
	}

	newNode.next = cursor.next
	cursor.next = newNode
	r.index[newNode.id.String()] = newNode
}

// nodeAtVisible returns the node at the given visible position.
// Position -1 returns the sentinel head.
func (r *RGA) nodeAtVisible(pos int) *rgaNode {
	if pos < 0 {
		return r.head
	}

	node := r.visibleNodeAt(pos)
	if node == nil {
		// past end: find last node
		last := r.head
		for last.next != nil {
			last = last.next
		}
		return last
	}
	return node
}

// visibleNodeAt returns the node at the given 0-based visible position.
func (r *RGA) visibleNodeAt(pos int) *rgaNode {
	idx := -1
	for n := r.head.next; n != nil; n = n.next {
		if !n.deleted {
			idx++
			if idx == pos {
				return n
			}
		}
	}
	return nil
}

// ToString returns the current visible text content.
func (r *RGA) ToString() string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var sb strings.Builder
	for n := r.head.next; n != nil; n = n.next {
		if !n.deleted {
			sb.WriteRune(n.char)
		}
	}
	return sb.String()
}

// Length returns the number of visible characters.
func (r *RGA) Length() int {
	r.mu.RLock()
	defer r.mu.RUnlock()

	count := 0
	for n := r.head.next; n != nil; n = n.next {
		if !n.deleted {
			count++
		}
	}
	return count
}

// Operations returns pending operations and clears the pending buffer.
func (r *RGA) Operations() []RGAOp {
	r.mu.Lock()
	defer r.mu.Unlock()

	ops := r.pending
	r.pending = nil
	return ops
}

// Merge applies all operations from a remote RGA.
// This is a convenience method that replays operations.
func (r *RGA) Merge(other *RGA) error {
	other.mu.RLock()
	// Collect all nodes from other in order
	var ops []RGAOp
	for n := other.head.next; n != nil; n = n.next {
		op := RGAOp{
			Type: OpInsert,
			ID:   n.id,
			Char: n.char,
		}
		// find parent: the previous node in the linked list
		prev := other.head
		for p := other.head; p.next != nil && p.next != n; p = p.next {
			prev = p.next
		}
		op.ParentID = prev.id
		ops = append(ops, op)

		if n.deleted {
			ops = append(ops, RGAOp{
				Type: OpDelete,
				ID:   n.id,
			})
		}
	}
	other.mu.RUnlock()

	for _, op := range ops {
		if err := r.ApplyOp(op); err != nil {
			return err
		}
	}
	return nil
}

// InsertText inserts a string starting at the given position.
// Returns the operations generated.
func (r *RGA) InsertText(position int, text string) []RGAOp {
	var ops []RGAOp
	for i, ch := range text {
		op := r.Insert(position+i, ch)
		ops = append(ops, op)
	}
	return ops
}

// StateVector returns a map of nodeID -> max sequence seen, used for sync.
func (r *RGA) StateVector() map[NodeID]uint64 {
	r.mu.RLock()
	defer r.mu.RUnlock()

	sv := make(map[NodeID]uint64)
	for n := r.head.next; n != nil; n = n.next {
		if n.id.Seq > sv[n.id.NodeID] {
			sv[n.id.NodeID] = n.id.Seq
		}
	}
	return sv
}

// OpsSince returns all operations for nodes whose sequence is greater than
// the values in the provided state vector.
func (r *RGA) OpsSince(sv map[NodeID]uint64) []RGAOp {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var ops []RGAOp
	for n := r.head.next; n != nil; n = n.next {
		maxSeen := sv[n.id.NodeID]
		if n.id.Seq > maxSeen {
			// find the parent (previous node)
			prev := r.head
			for p := r.head; p.next != nil && p.next != n; p = p.next {
				prev = p.next
			}

			ops = append(ops, RGAOp{
				Type:     OpInsert,
				ID:       n.id,
				ParentID: prev.id,
				Char:     n.char,
			})

			if n.deleted {
				ops = append(ops, RGAOp{
					Type: OpDelete,
					ID:   n.id,
				})
			}
		}
	}
	return ops
}
