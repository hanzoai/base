package crdt

import (
	"bytes"
	"context"
	"encoding/gob"
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

func init() {
	// Register types for gob encoding/decoding.
	gob.Register(map[string]any{})
	gob.Register([]any{})
	gob.Register(map[NodeID]uint64{})
	gob.Register(map[string]map[string]any{})
	gob.Register(Timestamp{})
	gob.Register(MVEntry{})
	gob.Register([]MVEntry{})
	gob.Register(RGAID{})
	gob.Register(RGAOp{})
}

// StateVersion represents a document's version as a state vector.
// Maps nodeID -> max sequence number seen from that node.
type StateVersion map[NodeID]uint64

// Dominates reports whether v causally dominates other
// (every entry in other is <= the corresponding entry in v).
func (v StateVersion) Dominates(other StateVersion) bool {
	for node, seq := range other {
		if v[node] < seq {
			return false
		}
	}
	return true
}

// Merge returns a new StateVersion taking the max of each entry.
func (v StateVersion) Merge(other StateVersion) StateVersion {
	result := make(StateVersion, len(v)+len(other))
	for n, s := range v {
		result[n] = s
	}
	for n, s := range other {
		if s > result[n] {
			result[n] = s
		}
	}
	return result
}

// FieldType identifies the CRDT type of a document field.
type FieldType uint8

const (
	FieldTypeText      FieldType = iota // RGA
	FieldTypeCounter                     // PNCounter
	FieldTypeSet                         // ORSet
	FieldTypeRegister                    // LWWRegister
	FieldTypeMVRegister                  // MVRegister
)

// Operation represents a serializable CRDT operation for sync.
type Operation struct {
	Field     string    `json:"field"`
	FieldType FieldType `json:"fieldType"`
	Data      []byte    `json:"data"`
}

// DocumentSnapshot is the serializable state of a Document.
type DocumentSnapshot struct {
	ID       string                          `json:"id"`
	Version  StateVersion                    `json:"version"`
	Texts    map[string]*textSnapshot        `json:"texts,omitempty"`
	Counters map[string]*counterSnapshot     `json:"counters,omitempty"`
	Sets     map[string]*setSnapshot         `json:"sets,omitempty"`
	Regs     map[string]*registerSnapshot    `json:"registers,omitempty"`
	MVRegs   map[string]*mvRegisterSnapshot  `json:"mvRegisters,omitempty"`
}

type textSnapshot struct {
	// Nodes in linked-list order
	Nodes []textNodeSnapshot `json:"nodes"`
}

type textNodeSnapshot struct {
	ID       RGAID  `json:"id"`
	ParentID RGAID  `json:"parentId"`
	Char     rune   `json:"char"`
	Deleted  bool   `json:"deleted"`
}

type counterSnapshot struct {
	Pos map[NodeID]uint64 `json:"pos"`
	Neg map[NodeID]uint64 `json:"neg"`
}

type setSnapshot struct {
	Elems map[string]map[string]any `json:"elems"`
}

type registerSnapshot struct {
	Value     any       `json:"value"`
	Timestamp Timestamp `json:"timestamp"`
}

type mvRegisterSnapshot struct {
	Entries []MVEntry `json:"entries"`
}

// DocumentOption configures a Document at creation time.
type DocumentOption func(*Document)

// WithPrivacy sets the privacy backend for a Document.
// Default: NewPlaintextPrivacy() (zero-overhead).
func WithPrivacy(p Privacy) DocumentOption {
	return func(d *Document) { d.privacy = p }
}

// WithAutoAnchor enables periodic Merkle-root anchoring to a Lux chain.
// The goroutine starts when the Document is created and stops on Close().
// If not supplied, no goroutine runs (zero overhead).
func WithAutoAnchor(a Anchorer, interval time.Duration) DocumentOption {
	return func(d *Document) {
		d.anchorer = a
		d.anchorInterval = interval
	}
}

// AnchorStatus reports the current state of the auto-anchoring loop.
type AnchorStatus struct {
	LastHeight     uint64
	LastRoot       [32]byte
	LastAnchoredAt time.Time
	LastError      error
}

// Document is a container for multiple named CRDT fields,
// representing a collaborative document.
type Document struct {
	mu       sync.RWMutex
	id       string
	nodeID   NodeID
	version  StateVersion
	seq      uint64
	privacy  Privacy

	// anchor lifecycle
	anchorer       Anchorer
	anchorInterval time.Duration
	anchorCancel   context.CancelFunc
	anchorDone     chan struct{}
	anchorStatus   AnchorStatus
	anchorMu       sync.RWMutex

	texts   map[string]*RGA
	counters map[string]*PNCounter
	sets     map[string]*ORSet
	regs     map[string]*LWWRegister
	mvRegs   map[string]*MVRegister
}

// NewDocument creates a new Document with the given ID and owning nodeID.
// Pass WithPrivacy to select an encryption backend; default is plaintext.
func NewDocument(id string, nodeID NodeID, opts ...DocumentOption) *Document {
	d := &Document{
		id:       id,
		nodeID:   nodeID,
		version:  make(StateVersion),
		privacy:  DefaultPrivacy(),
		texts:    make(map[string]*RGA),
		counters: make(map[string]*PNCounter),
		sets:     make(map[string]*ORSet),
		regs:     make(map[string]*LWWRegister),
		mvRegs:   make(map[string]*MVRegister),
	}
	for _, o := range opts {
		o(d)
	}
	if d.anchorer != nil {
		d.startAnchorLoop()
	}
	return d
}

// startAnchorLoop spawns the background anchor goroutine.
// On consecutive failures, the retry interval grows exponentially
// (capped at 10x the base interval) and resets on success.
func (d *Document) startAnchorLoop() {
	ctx, cancel := context.WithCancel(context.Background())
	d.anchorCancel = cancel
	d.anchorDone = make(chan struct{})

	interval := d.anchorInterval
	if interval <= 0 {
		interval = 5 * time.Minute
	}
	maxBackoff := 10 * interval

	go func() {
		defer close(d.anchorDone)
		curInterval := interval
		timer := time.NewTimer(curInterval)
		defer timer.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-timer.C:
				root, err := DocumentMerkleRoot(d)
				if err != nil {
					d.anchorMu.Lock()
					d.anchorStatus.LastError = err
					d.anchorMu.Unlock()
					curInterval = anchorBackoff(curInterval, maxBackoff)
					timer.Reset(curInterval)
					continue
				}
				if err := d.anchorer.Submit(ctx, root); err != nil {
					d.anchorMu.Lock()
					d.anchorStatus.LastError = err
					d.anchorMu.Unlock()
					curInterval = anchorBackoff(curInterval, maxBackoff)
					timer.Reset(curInterval)
					continue
				}
				height, _ := d.anchorer.LatestHeight(ctx)
				d.anchorMu.Lock()
				d.anchorStatus.LastHeight = height
				d.anchorStatus.LastRoot = root
				d.anchorStatus.LastAnchoredAt = time.Now()
				d.anchorStatus.LastError = nil
				d.anchorMu.Unlock()
				// Reset to base interval on success.
				curInterval = interval
				timer.Reset(curInterval)
			}
		}
	}()
}

// anchorBackoff doubles the current interval, capped at max.
func anchorBackoff(cur, max time.Duration) time.Duration {
	next := cur * 2
	if next > max {
		next = max
	}
	return next
}

// Privacy returns the document's privacy backend.
func (d *Document) Privacy() Privacy { return d.privacy }

// Close stops the auto-anchor goroutine (if running) and waits for it
// to exit. Safe to call multiple times or on a Document with no anchorer.
func (d *Document) Close() {
	if d.anchorCancel != nil {
		d.anchorCancel()
		<-d.anchorDone
	}
}

// AnchorStatus returns the current state of the auto-anchor loop.
// Returns a zero value if no anchorer is configured.
func (d *Document) AnchorStatus() AnchorStatus {
	d.anchorMu.RLock()
	defer d.anchorMu.RUnlock()
	return d.anchorStatus
}

// ID returns the document identifier.
func (d *Document) ID() string {
	return d.id
}

// Version returns a computed state version that merges the document's
// own version with state vectors from all text fields (RGAs).
func (d *Document) Version() StateVersion {
	d.mu.RLock()
	defer d.mu.RUnlock()
	cp := make(StateVersion, len(d.version))
	for k, v := range d.version {
		cp[k] = v
	}
	// merge in RGA state vectors
	for _, rga := range d.texts {
		sv := rga.StateVector()
		for node, seq := range sv {
			if seq > cp[node] {
				cp[node] = seq
			}
		}
	}
	return cp
}

// bumpVersion increments the local sequence and updates the state version.
func (d *Document) bumpVersion() {
	d.seq++
	d.version[d.nodeID] = d.seq
}

// GetText returns the RGA text field with the given name, creating it if needed.
func (d *Document) GetText(field string) *RGA {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.texts[field] == nil {
		d.texts[field] = NewRGA(d.nodeID)
	}
	return d.texts[field]
}

// GetCounter returns the PNCounter field with the given name, creating it if needed.
func (d *Document) GetCounter(field string) *PNCounter {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.counters[field] == nil {
		d.counters[field] = NewPNCounter()
	}
	return d.counters[field]
}

// GetSet returns the ORSet field with the given name, creating it if needed.
func (d *Document) GetSet(field string) *ORSet {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.sets[field] == nil {
		d.sets[field] = NewORSet(d.nodeID)
	}
	return d.sets[field]
}

// GetRegister returns the LWWRegister field with the given name, creating it if needed.
func (d *Document) GetRegister(field string) *LWWRegister {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.regs[field] == nil {
		d.regs[field] = NewLWWRegister()
	}
	return d.regs[field]
}

// GetMVRegister returns the MVRegister field with the given name, creating it if needed.
func (d *Document) GetMVRegister(field string) *MVRegister {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.mvRegs[field] == nil {
		d.mvRegs[field] = NewMVRegister()
	}
	return d.mvRegs[field]
}

// Merge merges a remote Document into this one.
func (d *Document) Merge(other *Document) {
	other.mu.RLock()
	defer other.mu.RUnlock()
	d.mu.Lock()
	defer d.mu.Unlock()

	// merge text fields
	for name, otherRGA := range other.texts {
		if d.texts[name] == nil {
			d.texts[name] = NewRGA(d.nodeID)
		}
		// ignore merge error for convergence
		_ = d.texts[name].Merge(otherRGA)
	}

	// merge counters
	for name, otherCtr := range other.counters {
		if d.counters[name] == nil {
			d.counters[name] = NewPNCounter()
		}
		d.counters[name].Merge(otherCtr)
	}

	// merge sets
	for name, otherSet := range other.sets {
		if d.sets[name] == nil {
			d.sets[name] = NewORSet(d.nodeID)
		}
		d.sets[name].Merge(otherSet)
	}

	// merge registers
	for name, otherReg := range other.regs {
		if d.regs[name] == nil {
			d.regs[name] = NewLWWRegister()
		}
		d.regs[name].Merge(otherReg)
	}

	// merge multi-value registers
	for name, otherMV := range other.mvRegs {
		if d.mvRegs[name] == nil {
			d.mvRegs[name] = NewMVRegister()
		}
		d.mvRegs[name].Merge(otherMV)
	}

	// merge version vectors
	d.version = d.version.Merge(other.version)
}

// Encode serializes the document to bytes. For non-plaintext backends,
// the snapshot is sealed through the privacy backend so no plaintext
// leaks into the output.
func (d *Document) Encode() ([]byte, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	snap := d.snapshot()

	var buf bytes.Buffer
	enc := gob.NewEncoder(&buf)
	if err := enc.Encode(snap); err != nil {
		return nil, fmt.Errorf("document encode: %w", err)
	}
	plaintext := buf.Bytes()

	// If the privacy backend is non-plaintext, seal the snapshot.
	if d.privacy.Name() != "plaintext/v1" {
		op := Operation{
			Field:     "__snapshot__",
			FieldType: FieldTypeRegister,
			Data:      plaintext,
		}
		sealed, err := d.privacy.EncryptOp(op)
		if err != nil {
			return nil, fmt.Errorf("document encode: seal snapshot: %w", err)
		}
		// Wrap in an OpEnvelope so Decode knows it is sealed.
		env := OpEnvelope{PrivacyTag: d.privacy.Name(), Ciphertext: sealed}
		envBytes, err := json.Marshal(env)
		if err != nil {
			return nil, fmt.Errorf("document encode: marshal envelope: %w", err)
		}
		return envBytes, nil
	}

	return plaintext, nil
}

// Decode deserializes a document from bytes. Pass the same DocumentOptions
// (especially WithPrivacy) used when the document was created so sealed
// snapshots can be unsealed.
func Decode(data []byte, nodeID NodeID, opts ...DocumentOption) (*Document, error) {
	// Probe for a sealed envelope by attempting JSON unmarshal.
	var env OpEnvelope
	if err := json.Unmarshal(data, &env); err == nil && env.PrivacyTag != "" && env.PrivacyTag != "plaintext/v1" {
		// Sealed snapshot. We need a privacy backend to unseal.
		probe := &Document{privacy: DefaultPrivacy()}
		for _, o := range opts {
			o(probe)
		}
		if probe.privacy.Name() != env.PrivacyTag {
			return nil, fmt.Errorf("document decode: snapshot sealed with %q but privacy backend is %q",
				env.PrivacyTag, probe.privacy.Name())
		}
		op, err := probe.privacy.DecryptOp(env.Ciphertext)
		if err != nil {
			return nil, fmt.Errorf("document decode: unseal snapshot: %w", err)
		}
		data = op.Data
	}

	var snap DocumentSnapshot
	dec := gob.NewDecoder(bytes.NewReader(data))
	if err := dec.Decode(&snap); err != nil {
		return nil, fmt.Errorf("document decode: %w", err)
	}

	doc := NewDocument(snap.ID, nodeID, opts...)
	doc.version = snap.Version

	// restore text fields
	for name, ts := range snap.Texts {
		rga := NewRGA(nodeID)
		for _, n := range ts.Nodes {
			op := RGAOp{
				Type:     OpInsert,
				ID:       n.ID,
				ParentID: n.ParentID,
				Char:     n.Char,
			}
			_ = rga.ApplyOp(op)
			if n.Deleted {
				_ = rga.ApplyOp(RGAOp{Type: OpDelete, ID: n.ID})
			}
		}
		doc.texts[name] = rga
	}

	// restore counters
	for name, cs := range snap.Counters {
		pn := NewPNCounter()
		for nid, val := range cs.Pos {
			pn.pos.mu.Lock()
			pn.pos.state[nid] = val
			pn.pos.mu.Unlock()
		}
		for nid, val := range cs.Neg {
			pn.neg.mu.Lock()
			pn.neg.state[nid] = val
			pn.neg.mu.Unlock()
		}
		doc.counters[name] = pn
	}

	// restore sets
	for name, ss := range snap.Sets {
		s := NewORSet(nodeID)
		s.mu.Lock()
		s.elems = ss.Elems
		s.mu.Unlock()
		doc.sets[name] = s
	}

	// restore registers
	for name, rs := range snap.Regs {
		reg := NewLWWRegister()
		reg.Set(rs.Value, rs.Timestamp)
		doc.regs[name] = reg
	}

	// restore mv registers
	for name, ms := range snap.MVRegs {
		mv := NewMVRegister()
		mv.mu.Lock()
		mv.entries = ms.Entries
		mv.mu.Unlock()
		doc.mvRegs[name] = mv
	}

	return doc, nil
}

// snapshot captures a serializable snapshot of the document (caller must hold mu).
func (d *Document) snapshot() DocumentSnapshot {
	snap := DocumentSnapshot{
		ID:      d.id,
		Version: make(StateVersion, len(d.version)),
	}
	for k, v := range d.version {
		snap.Version[k] = v
	}

	// snapshot texts
	if len(d.texts) > 0 {
		snap.Texts = make(map[string]*textSnapshot, len(d.texts))
		for name, rga := range d.texts {
			rga.mu.RLock()
			ts := &textSnapshot{}
			prev := rga.head
			for n := rga.head.next; n != nil; n = n.next {
				ts.Nodes = append(ts.Nodes, textNodeSnapshot{
					ID:       n.id,
					ParentID: prev.id,
					Char:     n.char,
					Deleted:  n.deleted,
				})
				prev = n
			}
			rga.mu.RUnlock()
			snap.Texts[name] = ts
		}
	}

	// snapshot counters
	if len(d.counters) > 0 {
		snap.Counters = make(map[string]*counterSnapshot, len(d.counters))
		for name, pn := range d.counters {
			snap.Counters[name] = &counterSnapshot{
				Pos: pn.pos.State(),
				Neg: pn.neg.State(),
			}
		}
	}

	// snapshot sets
	if len(d.sets) > 0 {
		snap.Sets = make(map[string]*setSnapshot, len(d.sets))
		for name, s := range d.sets {
			snap.Sets[name] = &setSnapshot{Elems: s.RawState()}
		}
	}

	// snapshot registers
	if len(d.regs) > 0 {
		snap.Regs = make(map[string]*registerSnapshot, len(d.regs))
		for name, r := range d.regs {
			val, ts := r.Get()
			snap.Regs[name] = &registerSnapshot{Value: val, Timestamp: ts}
		}
	}

	// snapshot mv registers
	if len(d.mvRegs) > 0 {
		snap.MVRegs = make(map[string]*mvRegisterSnapshot, len(d.mvRegs))
		for name, mv := range d.mvRegs {
			snap.MVRegs[name] = &mvRegisterSnapshot{Entries: mv.Get()}
		}
	}

	return snap
}

// Diff returns operations representing changes since the given state version.
// Currently operates at text-field granularity using RGA state vectors.
func (d *Document) Diff(since StateVersion) []Operation {
	d.mu.RLock()
	defer d.mu.RUnlock()

	var ops []Operation

	for name, rga := range d.texts {
		rgaOps := rga.OpsSince(since)
		if len(rgaOps) == 0 {
			continue
		}
		data, err := encodeGob(rgaOps)
		if err != nil {
			continue
		}
		ops = append(ops, Operation{
			Field:     name,
			FieldType: FieldTypeText,
			Data:      data,
		})
	}

	// For non-text fields, if the version is not dominated we send full state.
	// This is simpler and correct; delta sync for counters/sets can be added later.
	if !d.version.Dominates(since) || len(since) == 0 {
		for name, pn := range d.counters {
			data, err := encodeGob(&counterSnapshot{
				Pos: pn.pos.State(),
				Neg: pn.neg.State(),
			})
			if err != nil {
				continue
			}
			ops = append(ops, Operation{
				Field:     name,
				FieldType: FieldTypeCounter,
				Data:      data,
			})
		}

		for name, s := range d.sets {
			data, err := encodeGob(&setSnapshot{Elems: s.RawState()})
			if err != nil {
				continue
			}
			ops = append(ops, Operation{
				Field:     name,
				FieldType: FieldTypeSet,
				Data:      data,
			})
		}

		for name, r := range d.regs {
			val, ts := r.Get()
			data, err := encodeGob(&registerSnapshot{Value: val, Timestamp: ts})
			if err != nil {
				continue
			}
			ops = append(ops, Operation{
				Field:     name,
				FieldType: FieldTypeRegister,
				Data:      data,
			})
		}

		for name, mv := range d.mvRegs {
			data, err := encodeGob(&mvRegisterSnapshot{Entries: mv.Get()})
			if err != nil {
				continue
			}
			ops = append(ops, Operation{
				Field:     name,
				FieldType: FieldTypeMVRegister,
				Data:      data,
			})
		}
	}

	return ops
}

// OpEnvelope is the wire/persistence format for a single CRDT operation.
// It carries the privacy backend tag and the ciphertext (output of
// Privacy.EncryptOp). Replicas that receive an envelope whose tag does
// not match their own backend MUST reject it.
type OpEnvelope struct {
	PrivacyTag string `json:"privacyTag"`
	Ciphertext []byte `json:"ct"`
}

// ErrPrivacyMismatch is returned when an OpEnvelope's PrivacyTag does
// not match the document's privacy backend.
var ErrPrivacyMismatch = fmt.Errorf("crdt: privacy backend mismatch")

// ErrDocIDReplay is returned when OpenOps unwraps an envelope whose
// authenticated docID field does not match the current document.
var ErrDocIDReplay = fmt.Errorf("crdt: envelope bound to a different document")

// docIDSentinel marks a payload as carrying a bound docID header.
// Every payload produced by SealOps starts with this sentinel.
const docIDSentinel = "\x00crdt-docid-v1\x00"

// wrapDocID prepends [sentinel][u16 BE len][docID][original] so that
// the docID travels inside the authenticated payload.
func wrapDocID(docID string, data []byte) []byte {
	buf := make([]byte, 0, len(docIDSentinel)+2+len(docID)+len(data))
	buf = append(buf, docIDSentinel...)
	n := len(docID)
	buf = append(buf, byte(n>>8), byte(n))
	buf = append(buf, docID...)
	buf = append(buf, data...)
	return buf
}

// unwrapDocID reverses wrapDocID, returning the original data and an
// error if the envelope is bound to a different document.
func unwrapDocID(expectedID string, blob []byte) ([]byte, error) {
	sent := []byte(docIDSentinel)
	if len(blob) < len(sent)+2 || !bytes.HasPrefix(blob, sent) {
		return nil, ErrDocIDReplay
	}
	rest := blob[len(sent):]
	n := int(rest[0])<<8 | int(rest[1])
	rest = rest[2:]
	if len(rest) < n {
		return nil, ErrDocIDReplay
	}
	gotID := string(rest[:n])
	if gotID != expectedID {
		return nil, fmt.Errorf("%w: envelope bound to %q, doc is %q", ErrDocIDReplay, gotID, expectedID)
	}
	return rest[n:], nil
}

// SealOps encrypts a slice of plaintext Operations into OpEnvelopes
// using the document's privacy backend. Each op's Data is prefixed
// with the document ID inside the authenticated payload; OpenOps
// refuses envelopes whose bound docID differs.
func (d *Document) SealOps(ops []Operation) ([]OpEnvelope, error) {
	tag := d.privacy.Name()
	envs := make([]OpEnvelope, len(ops))
	for i, op := range ops {
		bound := Operation{
			Field:     op.Field,
			FieldType: op.FieldType,
			Data:      wrapDocID(d.ID(), op.Data),
		}
		ct, err := d.privacy.EncryptOp(bound)
		if err != nil {
			return nil, fmt.Errorf("crdt: seal op %d: %w", i, err)
		}
		envs[i] = OpEnvelope{PrivacyTag: tag, Ciphertext: ct}
	}
	return envs, nil
}

// OpenOps decrypts a slice of OpEnvelopes into plaintext Operations.
// Returns ErrPrivacyMismatch if any envelope's tag differs from the
// document's backend, or ErrDocIDReplay if the authenticated docID
// inside the envelope does not match this document.
func (d *Document) OpenOps(envs []OpEnvelope) ([]Operation, error) {
	tag := d.privacy.Name()
	ops := make([]Operation, len(envs))
	for i, env := range envs {
		if env.PrivacyTag != tag {
			return nil, fmt.Errorf("%w: got %q, want %q", ErrPrivacyMismatch, env.PrivacyTag, tag)
		}
		bound, err := d.privacy.DecryptOp(env.Ciphertext)
		if err != nil {
			return nil, fmt.Errorf("crdt: open op %d: %w", i, err)
		}
		data, err := unwrapDocID(d.ID(), bound.Data)
		if err != nil {
			return nil, fmt.Errorf("crdt: open op %d: %w", i, err)
		}
		ops[i] = Operation{
			Field:     bound.Field,
			FieldType: bound.FieldType,
			Data:      data,
		}
	}
	return ops, nil
}

func encodeGob(v any) ([]byte, error) {
	var buf bytes.Buffer
	if err := gob.NewEncoder(&buf).Encode(v); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
