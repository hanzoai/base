package network

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
)

// Segment on-disk format (".lbn" = Lux Base Network segment):
//
//   magic             [4]byte  "LBN1"
//   shard_id_len      uint16   big-endian
//   shard_id          [shard_id_len]byte
//   segment_start_seq uint64   big-endian
//   frame_count       uint32   big-endian
//   repeat frame_count times:
//     frame_len  uint32 big-endian
//     frame      [frame_len]byte  (quasar-finalised, PQ signature intact)
//   footer_crc32      uint32   big-endian, IEEE over everything above
//
// Forwards-compat: if the format changes, bump the magic to "LBN2"/…
// and branch in decodeSegment on the first 4 bytes.

const (
	segmentMagic        = "LBN1"
	segmentMagicLen     = 4
	segmentHeaderMinLen = segmentMagicLen + 2 /*shard len*/ + 8 /*start seq*/ + 4 /*count*/
	segmentFooterLen    = 4
)

// ErrSegmentCorrupt is returned when a segment fails magic or CRC
// validation on read.
var ErrSegmentCorrupt = errors.New("segment: corrupt")

// segmentBuffer is an append-only in-memory segment being built for a
// single shard. It is NOT safe for concurrent use; callers (one
// goroutine per shard) must serialise.
type segmentBuffer struct {
	shardID    string
	startSeq   uint64
	nextSeq    uint64 // expected seq of the next frame to append
	frames     [][]byte
	payloadLen int // accumulated frame bytes (for size-based flush decisions)
}

func newSegmentBuffer(shardID string, startSeq uint64) *segmentBuffer {
	return &segmentBuffer{
		shardID:  shardID,
		startSeq: startSeq,
		nextSeq:  startSeq,
	}
}

// append records a frame. Seq must be monotonically increasing and
// match nextSeq; a gap is a programmer error at the caller.
func (s *segmentBuffer) append(seq uint64, frame []byte) error {
	if s.len() > 0 && seq != s.nextSeq {
		return fmt.Errorf("segment: out-of-order append want seq %d got %d", s.nextSeq, seq)
	}
	if s.len() == 0 {
		s.startSeq = seq
		s.nextSeq = seq
	}
	// Copy so the caller can reuse its buffer.
	dup := make([]byte, len(frame))
	copy(dup, frame)
	s.frames = append(s.frames, dup)
	s.payloadLen += len(frame)
	s.nextSeq = seq + 1
	return nil
}

func (s *segmentBuffer) len() int { return len(s.frames) }

// sizeBytes is a cheap estimate of the encoded segment size: headers +
// frame_len prefixes + payload + footer. Used to decide when to flush.
func (s *segmentBuffer) sizeBytes() int {
	return segmentHeaderMinLen + len(s.shardID) + 4*len(s.frames) + s.payloadLen + segmentFooterLen
}

// encode returns the full serialized segment. CRC covers the entire
// body (everything preceding the 4-byte footer).
func (s *segmentBuffer) encode() ([]byte, error) {
	if len(s.shardID) > 0xFFFF {
		return nil, fmt.Errorf("segment: shard id too long (%d)", len(s.shardID))
	}
	if len(s.frames) > 0xFFFFFFFF {
		return nil, fmt.Errorf("segment: too many frames (%d)", len(s.frames))
	}
	buf := bytes.NewBuffer(make([]byte, 0, s.sizeBytes()))
	buf.WriteString(segmentMagic)
	_ = binary.Write(buf, binary.BigEndian, uint16(len(s.shardID)))
	buf.WriteString(s.shardID)
	_ = binary.Write(buf, binary.BigEndian, s.startSeq)
	_ = binary.Write(buf, binary.BigEndian, uint32(len(s.frames)))
	for _, f := range s.frames {
		if len(f) > 0xFFFFFFFF {
			return nil, fmt.Errorf("segment: frame too large (%d)", len(f))
		}
		_ = binary.Write(buf, binary.BigEndian, uint32(len(f)))
		buf.Write(f)
	}
	crc := crc32.ChecksumIEEE(buf.Bytes())
	_ = binary.Write(buf, binary.BigEndian, crc)
	return buf.Bytes(), nil
}

// objectKey is the storage-layer name for a flushed segment.
// Layout (per docs/NETWORK.md): <svc>/<shard>/<seq-prefix>/<segment>.lbn
// seq-prefix bucketises seqs in 1M-frame groups to keep per-prefix
// object counts sane for listing.
func objectKey(svcPrefix, shardID string, startSeq uint64) string {
	const bucketSize = 1_000_000
	return fmt.Sprintf("%s/%s/%016d/%020d.lbn",
		svcPrefix, shardID, startSeq/bucketSize, startSeq)
}

// decodedSegment is a parsed segment ready for iteration.
type decodedSegment struct {
	ShardID  string
	StartSeq uint64
	Frames   [][]byte
}

// decodeSegment parses bytes into a decodedSegment, validating magic
// and CRC. Any malformed input returns ErrSegmentCorrupt wrapped with
// context.
func decodeSegment(b []byte) (*decodedSegment, error) {
	if len(b) < segmentHeaderMinLen+segmentFooterLen {
		return nil, fmt.Errorf("%w: too short (%d)", ErrSegmentCorrupt, len(b))
	}
	if string(b[:segmentMagicLen]) != segmentMagic {
		// Forwards-compat: future LBNn decoders branch here on magic.
		return nil, fmt.Errorf("%w: bad magic %q", ErrSegmentCorrupt, string(b[:segmentMagicLen]))
	}
	body := b[:len(b)-segmentFooterLen]
	wantCRC := binary.BigEndian.Uint32(b[len(b)-segmentFooterLen:])
	if got := crc32.ChecksumIEEE(body); got != wantCRC {
		return nil, fmt.Errorf("%w: crc mismatch (got %08x want %08x)", ErrSegmentCorrupt, got, wantCRC)
	}

	r := bytes.NewReader(body[segmentMagicLen:])
	var shardLen uint16
	if err := binary.Read(r, binary.BigEndian, &shardLen); err != nil {
		return nil, fmt.Errorf("%w: shard len: %v", ErrSegmentCorrupt, err)
	}
	shard := make([]byte, shardLen)
	if _, err := io.ReadFull(r, shard); err != nil {
		return nil, fmt.Errorf("%w: shard id: %v", ErrSegmentCorrupt, err)
	}
	var startSeq uint64
	if err := binary.Read(r, binary.BigEndian, &startSeq); err != nil {
		return nil, fmt.Errorf("%w: start seq: %v", ErrSegmentCorrupt, err)
	}
	var count uint32
	if err := binary.Read(r, binary.BigEndian, &count); err != nil {
		return nil, fmt.Errorf("%w: frame count: %v", ErrSegmentCorrupt, err)
	}
	frames := make([][]byte, count)
	for i := uint32(0); i < count; i++ {
		var fl uint32
		if err := binary.Read(r, binary.BigEndian, &fl); err != nil {
			return nil, fmt.Errorf("%w: frame %d len: %v", ErrSegmentCorrupt, i, err)
		}
		if int(fl) > r.Len() {
			return nil, fmt.Errorf("%w: frame %d len %d > remaining %d", ErrSegmentCorrupt, i, fl, r.Len())
		}
		f := make([]byte, fl)
		if _, err := io.ReadFull(r, f); err != nil {
			return nil, fmt.Errorf("%w: frame %d body: %v", ErrSegmentCorrupt, i, err)
		}
		frames[i] = f
	}
	if r.Len() != 0 {
		return nil, fmt.Errorf("%w: %d trailing bytes", ErrSegmentCorrupt, r.Len())
	}
	return &decodedSegment{
		ShardID:  string(shard),
		StartSeq: startSeq,
		Frames:   frames,
	}, nil
}
