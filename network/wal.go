package network

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"sync/atomic"
	"time"
)

// Frame is the unit of replication. It is the serialised form of one
// committed SQLite transaction for a shard: opaque payload bytes (the WAL
// segment delta), a monotonic sequence, a random salt, and a checksum.
//
// Frames are content-addressed by (salt, cksm) so that:
//   - duplicate submissions coalesce in the Quasar DAG,
//   - the apply callback is trivially idempotent on pod restart,
//   - any tamper on the wire is caught before apply.
//
// Bytes is the serialised on-wire form (encode output). It is populated
// lazily by decodeFrame and by the archive layer so that frame replay
// keeps the original PQ signature intact; local producers use encode()
// directly.
type Frame struct {
	ShardID   string
	Seq       uint64
	PrevSeq   uint64
	Timestamp int64
	Salt      [16]byte
	Payload   []byte
	Cksm      [32]byte
	Bytes     []byte
}

// newFrame builds a Frame, computing the checksum from the payload plus
// shardID, seq and salt so cross-shard replay is impossible.
func newFrame(shardID string, seq, prev uint64, payload []byte) Frame {
	var salt [16]byte
	if _, err := rand.Read(salt[:]); err != nil {
		// rand.Read only fails catastrophically (bad /dev/urandom) — fall
		// back to timestamp-derived salt rather than panicking in a commit
		// hook, which would block writes.
		binary.BigEndian.PutUint64(salt[:8], uint64(time.Now().UnixNano()))
	}
	f := Frame{
		ShardID:   shardID,
		Seq:       seq,
		PrevSeq:   prev,
		Timestamp: time.Now().UnixNano(),
		Salt:      salt,
		Payload:   payload,
	}
	f.Cksm = f.computeCksm()
	return f
}

// computeCksm hashes (shardID || seq || prev || salt || payload). Not a MAC
// yet — PQ-signing happens in the Quasar engine. This binds the frame bytes.
func (f Frame) computeCksm() [32]byte {
	h := sha256.New()
	h.Write([]byte(f.ShardID))
	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], f.Seq)
	h.Write(buf[:])
	binary.BigEndian.PutUint64(buf[:], f.PrevSeq)
	h.Write(buf[:])
	h.Write(f.Salt[:])
	h.Write(f.Payload)
	var out [32]byte
	copy(out[:], h.Sum(nil))
	return out
}

// Valid returns nil iff the stored checksum matches a freshly computed one.
// Callers check this before applying a frame that came off the wire.
func (f Frame) Valid() error {
	want := f.computeCksm()
	if want != f.Cksm {
		return errors.New("frame: checksum mismatch")
	}
	return nil
}

// ApplyKey is the (salt, cksm) pair used to dedupe applies.
type ApplyKey [48]byte

// ApplyKey returns the idempotency key for this frame.
func (f Frame) ApplyKey() ApplyKey {
	var k ApplyKey
	copy(k[0:16], f.Salt[:])
	copy(k[16:48], f.Cksm[:])
	return k
}

// blockID derives the quasar.Block.ID (32 bytes) from the frame checksum.
func (f Frame) blockID() [32]byte { return f.Cksm }

// encode serialises the frame for transport in quasar.Block.Data:
//
//	[ver:1][shardIDLen:2][shardID][seq:8][prev:8][ts:8][salt:16][payloadLen:4][payload][cksm:32]
//
// Compact, self-describing, no reflection.
func (f Frame) encode() []byte {
	sid := []byte(f.ShardID)
	buf := make([]byte, 0, 1+2+len(sid)+8+8+8+16+4+len(f.Payload)+32)
	buf = append(buf, 1) // version
	buf = appendU16(buf, uint16(len(sid)))
	buf = append(buf, sid...)
	buf = appendU64(buf, f.Seq)
	buf = appendU64(buf, f.PrevSeq)
	buf = appendU64(buf, uint64(f.Timestamp))
	buf = append(buf, f.Salt[:]...)
	buf = appendU32(buf, uint32(len(f.Payload)))
	buf = append(buf, f.Payload...)
	buf = append(buf, f.Cksm[:]...)
	return buf
}

// decodeFrame parses the output of Frame.encode. Guards against truncation
// and mis-framing. The raw input buffer is retained on f.Bytes so archive
// replay can forward the exact wire bytes without re-encoding.
func decodeFrame(b []byte) (Frame, error) {
	if len(b) < 1+2+8+8+8+16+4+32 {
		return Frame{}, errors.New("frame: buffer too short")
	}
	var f Frame
	p := 0
	if b[p] != 1 {
		return Frame{}, fmt.Errorf("frame: unknown version %d", b[p])
	}
	p++
	sidLen := binary.BigEndian.Uint16(b[p:])
	p += 2
	if p+int(sidLen) > len(b) {
		return Frame{}, errors.New("frame: shardID out of bounds")
	}
	f.ShardID = string(b[p : p+int(sidLen)])
	p += int(sidLen)
	if p+8+8+8+16+4 > len(b) {
		return Frame{}, errors.New("frame: header truncated")
	}
	f.Seq = binary.BigEndian.Uint64(b[p:])
	p += 8
	f.PrevSeq = binary.BigEndian.Uint64(b[p:])
	p += 8
	f.Timestamp = int64(binary.BigEndian.Uint64(b[p:]))
	p += 8
	copy(f.Salt[:], b[p:p+16])
	p += 16
	plen := binary.BigEndian.Uint32(b[p:])
	p += 4
	if p+int(plen)+32 > len(b) {
		return Frame{}, errors.New("frame: payload out of bounds")
	}
	f.Payload = append([]byte(nil), b[p:p+int(plen)]...)
	p += int(plen)
	copy(f.Cksm[:], b[p:p+32])
	f.Bytes = append([]byte(nil), b...)
	return f, nil
}

func appendU16(b []byte, v uint16) []byte { var t [2]byte; binary.BigEndian.PutUint16(t[:], v); return append(b, t[:]...) }
func appendU32(b []byte, v uint32) []byte { var t [4]byte; binary.BigEndian.PutUint32(t[:], v); return append(b, t[:]...) }
func appendU64(b []byte, v uint64) []byte { var t [8]byte; binary.BigEndian.PutUint64(t[:], v); return append(b, t[:]...) }

// HookRegisterer is the narrow surface we need from a SQLite driver
// connection: the method modernc.org/sqlite exposes on its *conn. Accepting
// it through a tiny interface keeps this package decoupled from the
// concrete driver type and test-friendly.
type HookRegisterer interface {
	RegisterCommitHook(func() int32)
}

// walSource captures committed bytes for a shard. Production hooks it to the
// live SQLite WAL; tests inject a function that returns deterministic
// payloads. Keeping it narrow lets us ship the consensus plane today and
// wire the WAL-file scraper in a follow-up without touching the API.
type walSource interface {
	// CapturePayload returns the bytes that represent the just-committed
	// transaction for this shard. Called from inside the commit hook, so it
	// must be non-blocking and cheap. Returning a non-nil error aborts the
	// commit with SQLITE_BUSY via the hook's int32 return value.
	CapturePayload(shardID string) ([]byte, error)
}

// shardState holds the monotonic seq counter for the local writer.
type shardWriter struct {
	shardID string
	src     walSource

	seq     atomic.Uint64
	prevSeq atomic.Uint64
}

// submit builds the frame and routes it to the shard's Quasar engine.
func (w *shardWriter) buildFrame() (Frame, error) {
	payload, err := w.src.CapturePayload(w.shardID)
	if err != nil {
		return Frame{}, err
	}
	prev := w.prevSeq.Load()
	seq := w.seq.Add(1)
	w.prevSeq.Store(seq)
	return newFrame(w.shardID, seq, prev, payload), nil
}

// InstallWALHook installs the commit hook on a raw SQLite conn for the given
// shard. rawConn must implement HookRegisterer (modernc.org/sqlite *conn
// satisfies this). A no-op source is used unless one has been registered via
// SetWALSource — SQLite-delta capture is a separate agent's scope.
//
// Returns an error when:
//   - the network is not running,
//   - rawConn does not expose a commit hook (wrong driver).
func (n *node) InstallWALHook(rawConn any, shardID string) error {
	hook, ok := rawConn.(HookRegisterer)
	if !ok {
		return fmt.Errorf("network: connection type %T has no commit hook", rawConn)
	}

	s, err := n.shard(shardID)
	if err != nil {
		return err
	}

	w := &shardWriter{shardID: shardID, src: n.walSrc}
	hook.RegisterCommitHook(func() int32 {
		f, err := w.buildFrame()
		if err != nil {
			n.metrics.WALHookErrors.Inc()
			return 1 // non-zero aborts commit; safer than silently losing writes
		}
		if err := s.submitLocal(f); err != nil {
			n.metrics.WALHookErrors.Inc()
			return 1
		}
		// Fan out to peers via transport. Best-effort — Quasar tolerates
		// duplicate submissions and will converge once peers see it.
		_ = n.transport.Publish(Envelope{ShardID: shardID, Frame: f})
		n.metrics.WALBytes.Add(float64(len(f.Payload)))
		return 0
	})
	return nil
}

// SetWALSource installs a capture function used by all future
// InstallWALHook calls. When unset, frames carry an empty payload (useful
// for exercising the consensus plane in isolation, e.g. integration tests).
func SetWALSource(n Network, src walSource) {
	nn, ok := n.(*node)
	if !ok {
		return
	}
	nn.mu.Lock()
	nn.walSrc = src
	nn.mu.Unlock()
}

// nopSource is the standalone capture — an empty payload. Still safe: the
// frame carries shardID+seq+salt+cksm for routing and idempotency.
type nopSource struct{}

func (nopSource) CapturePayload(string) ([]byte, error) { return nil, nil }
