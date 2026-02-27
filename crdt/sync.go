package crdt

import (
	"bytes"
	"encoding/gob"
	"encoding/json"
	"fmt"
	"sync"
)

// SyncMessage types for the state-vector based sync protocol.
const (
	// SyncStep1 is sent by a connecting client: "here is my state vector".
	SyncStep1 = "sync_step1"
	// SyncStep2 is the server response: "here are ops you are missing".
	SyncStep2 = "sync_step2"
	// SyncUpdate is an incremental update (ops broadcast).
	SyncUpdate = "sync_update"
)

// SyncMessage is the wire format for CRDT sync messages.
type SyncMessage struct {
	Type        string       `json:"type"`
	DocID       string       `json:"docId"`
	ClientID    string       `json:"clientId,omitempty"`
	StateVector StateVersion `json:"stateVector,omitempty"`
	Ops         []Operation  `json:"ops,omitempty"`
}

// SyncBroadcastFunc is called when operations need to be broadcast to other clients.
type SyncBroadcastFunc func(docID string, excludeClient string, msg []byte)

// SyncManager handles CRDT document synchronization across clients.
type SyncManager struct {
	mu        sync.RWMutex
	docs      map[string]*Document
	broadcast SyncBroadcastFunc
}

// NewSyncManager creates a new SyncManager.
// The broadcast function is called whenever operations should be sent to other clients.
func NewSyncManager(broadcast SyncBroadcastFunc) *SyncManager {
	return &SyncManager{
		docs:      make(map[string]*Document),
		broadcast: broadcast,
	}
}

// RegisterDocument adds a document to the sync manager.
func (sm *SyncManager) RegisterDocument(id string, doc *Document) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.docs[id] = doc
}

// UnregisterDocument removes a document from the sync manager.
func (sm *SyncManager) UnregisterDocument(id string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	delete(sm.docs, id)
}

// GetDocument returns a registered document by ID.
func (sm *SyncManager) GetDocument(id string) *Document {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return sm.docs[id]
}

// GetOrCreateDocument returns an existing document or creates a new one.
func (sm *SyncManager) GetOrCreateDocument(id string, nodeID NodeID) *Document {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	if doc, ok := sm.docs[id]; ok {
		return doc
	}
	doc := NewDocument(id, nodeID)
	sm.docs[id] = doc
	return doc
}

// HandleSync processes an incoming sync message from a client and returns
// a response message (if any). This implements the state-vector sync protocol:
//
//  1. Client sends SyncStep1 with its state vector
//  2. Server responds with SyncStep2 containing ops the client is missing
//  3. Server also sends the server's state vector so the client can respond with SyncStep2
//  4. Incremental updates are broadcast as SyncUpdate
func (sm *SyncManager) HandleSync(clientID string, raw []byte) ([]byte, error) {
	var msg SyncMessage
	if err := json.Unmarshal(raw, &msg); err != nil {
		return nil, fmt.Errorf("sync unmarshal: %w", err)
	}

	sm.mu.RLock()
	doc, ok := sm.docs[msg.DocID]
	sm.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("document %q not found", msg.DocID)
	}

	switch msg.Type {
	case SyncStep1:
		return sm.handleSyncStep1(doc, clientID, msg)
	case SyncStep2:
		return sm.handleSyncStep2(doc, clientID, msg)
	case SyncUpdate:
		return sm.handleSyncUpdate(doc, clientID, msg)
	default:
		return nil, fmt.Errorf("unknown sync message type: %s", msg.Type)
	}
}

// handleSyncStep1 processes a client's state vector and responds with missing ops.
func (sm *SyncManager) handleSyncStep1(doc *Document, clientID string, msg SyncMessage) ([]byte, error) {
	ops := doc.Diff(msg.StateVector)

	response := SyncMessage{
		Type:        SyncStep2,
		DocID:       msg.DocID,
		StateVector: doc.Version(),
		Ops:         ops,
	}

	return json.Marshal(response)
}

// handleSyncStep2 processes ops from a client that we are missing.
func (sm *SyncManager) handleSyncStep2(doc *Document, clientID string, msg SyncMessage) ([]byte, error) {
	if len(msg.Ops) == 0 {
		return nil, nil
	}

	if err := sm.applyOps(doc, msg.Ops); err != nil {
		return nil, fmt.Errorf("apply ops: %w", err)
	}

	sm.broadcastOps(msg.DocID, clientID, msg.Ops)
	return nil, nil
}

// handleSyncUpdate processes an incremental update from a client.
func (sm *SyncManager) handleSyncUpdate(doc *Document, clientID string, msg SyncMessage) ([]byte, error) {
	if len(msg.Ops) == 0 {
		return nil, nil
	}

	if err := sm.applyOps(doc, msg.Ops); err != nil {
		return nil, fmt.Errorf("apply ops: %w", err)
	}

	sm.broadcastOps(msg.DocID, clientID, msg.Ops)
	return nil, nil
}

// applyOps applies a slice of operations to a document.
func (sm *SyncManager) applyOps(doc *Document, ops []Operation) error {
	for _, op := range ops {
		switch op.FieldType {
		case FieldTypeText:
			var rgaOps []RGAOp
			if err := gobDecode(op.Data, &rgaOps); err != nil {
				return fmt.Errorf("decode text ops for field %q: %w", op.Field, err)
			}
			rga := doc.GetText(op.Field)
			for _, rgaOp := range rgaOps {
				if err := rga.ApplyOp(rgaOp); err != nil {
					// missing parent means out-of-order; skip for now
					continue
				}
			}

		case FieldTypeCounter:
			var snap counterSnapshot
			if err := gobDecode(op.Data, &snap); err != nil {
				return fmt.Errorf("decode counter for field %q: %w", op.Field, err)
			}
			ctr := doc.GetCounter(op.Field)
			remote := NewPNCounter()
			remote.pos.mu.Lock()
			remote.pos.state = snap.Pos
			remote.pos.mu.Unlock()
			remote.neg.mu.Lock()
			remote.neg.state = snap.Neg
			remote.neg.mu.Unlock()
			ctr.Merge(remote)

		case FieldTypeSet:
			var snap setSnapshot
			if err := gobDecode(op.Data, &snap); err != nil {
				return fmt.Errorf("decode set for field %q: %w", op.Field, err)
			}
			s := doc.GetSet(op.Field)
			remote := NewORSet("remote")
			remote.mu.Lock()
			remote.elems = snap.Elems
			remote.mu.Unlock()
			s.Merge(remote)

		case FieldTypeRegister:
			var snap registerSnapshot
			if err := gobDecode(op.Data, &snap); err != nil {
				return fmt.Errorf("decode register for field %q: %w", op.Field, err)
			}
			reg := doc.GetRegister(op.Field)
			reg.Set(snap.Value, snap.Timestamp)

		case FieldTypeMVRegister:
			var snap mvRegisterSnapshot
			if err := gobDecode(op.Data, &snap); err != nil {
				return fmt.Errorf("decode mv register for field %q: %w", op.Field, err)
			}
			mv := doc.GetMVRegister(op.Field)
			remote := NewMVRegister()
			remote.mu.Lock()
			remote.entries = snap.Entries
			remote.mu.Unlock()
			mv.Merge(remote)
		}
	}
	return nil
}

// BroadcastOps broadcasts operations to all clients connected to a document.
func (sm *SyncManager) BroadcastOps(docID string, ops []Operation) {
	sm.broadcastOps(docID, "", ops)
}

func (sm *SyncManager) broadcastOps(docID string, excludeClient string, ops []Operation) {
	if sm.broadcast == nil || len(ops) == 0 {
		return
	}

	msg := SyncMessage{
		Type:  SyncUpdate,
		DocID: docID,
		Ops:   ops,
	}

	data, err := json.Marshal(msg)
	if err != nil {
		return
	}

	sm.broadcast(docID, excludeClient, data)
}

// Documents returns a list of all registered document IDs.
func (sm *SyncManager) Documents() []string {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	ids := make([]string, 0, len(sm.docs))
	for id := range sm.docs {
		ids = append(ids, id)
	}
	return ids
}

// gobDecode decodes gob-encoded data into v.
func gobDecode(data []byte, v any) error {
	return gob.NewDecoder(bytes.NewReader(data)).Decode(v)
}
