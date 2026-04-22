package network

import (
	"bytes"
	"crypto/ed25519"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
)

// Segment on-disk format (".lbn" = Lux Base Network segment).
//
// Version 2 ("LBN2") — authenticated:
//
//	magic             [4]byte   "LBN2"
//	shard_id_len      uint16    big-endian
//	shard_id          [shard_id_len]byte
//	segment_start_seq uint64    big-endian
//	frame_count       uint32    big-endian
//	repeat frame_count times:
//	  frame_len       uint32    big-endian
//	  frame           [frame_len]byte  (quasar-finalised, PQ signature intact)
//	footer_crc32      uint32    big-endian, IEEE over everything above
//	pubkey            [32]byte  Ed25519 public key that signed this segment
//	signature         [64]byte  Ed25519 signature over body||crc||pubkey
//
// The signature binds (shardID, startSeq, frame_count, every frame) + the
// integrity-covering CRC. Attackers with bucket write access cannot forge
// a valid segment without the archive role's private key.
//
// Version 1 ("LBN1") existed only during dev and is rejected by readers
// to prevent downgrade attacks. Future format changes MUST bump the magic
// to "LBN3"/... and version-detect on the first 4 bytes.

const (
	segmentMagicV1      = "LBN1"
	segmentMagic        = "LBN2"
	segmentMagicLen     = 4
	segmentHeaderMinLen = segmentMagicLen + 2 /*shard len*/ + 8 /*start seq*/ + 4 /*count*/
	segmentFooterCRCLen = 4
	segmentPubKeyLen    = ed25519.PublicKeySize
	segmentSigLen       = ed25519.SignatureSize
	segmentFooterLen    = segmentFooterCRCLen + segmentPubKeyLen + segmentSigLen
)

// ErrSegmentCorrupt is returned when a segment fails magic, CRC, or
// signature validation on read.
var ErrSegmentCorrupt = errors.New("segment: corrupt")

// ErrSegmentUnsigned is returned when a reader has no verifier (nil)
// or a writer has no signer (nil). Fail-closed — never decode/encode
// an unauthenticated segment.
var ErrSegmentUnsigned = errors.New("segment: no verifier configured")

// segmentSigner signs a segment's body+crc with an Ed25519 key. The
// archive-writer owns one per process; the public key bytes are embedded
// in every segment footer so readers with the matching trust root can
// verify.
type segmentSigner struct {
	priv ed25519.PrivateKey
	pub  ed25519.PublicKey
}

func newSegmentSigner(priv ed25519.PrivateKey) *segmentSigner {
	if len(priv) != ed25519.PrivateKeySize {
		return nil
	}
	return &segmentSigner{priv: priv, pub: priv.Public().(ed25519.PublicKey)}
}

// segmentVerifier is the reader's trust policy: does this public key
// match the key we expect for this archive? A nil verifier rejects
// every segment — fail-closed.
type segmentVerifier struct {
	// allowed is the set of Ed25519 public keys we accept. Keyed by
	// string(pubkey) for O(1) lookup.
	allowed map[string]struct{}
}

// newSegmentVerifier builds a verifier that accepts segments signed by
// any of the supplied public keys. Empty pubs → fail-closed.
func newSegmentVerifier(pubs ...ed25519.PublicKey) *segmentVerifier {
	v := &segmentVerifier{allowed: make(map[string]struct{}, len(pubs))}
	for _, p := range pubs {
		if len(p) == ed25519.PublicKeySize {
			v.allowed[string(p)] = struct{}{}
		}
	}
	return v
}

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

// encode returns the full serialized segment. CRC covers the body
// (magic through last frame). The Ed25519 signature covers body||crc||pubkey.
// encode returns an error if the signer is nil — we never ship an
// unsigned segment.
func (s *segmentBuffer) encode(signer *segmentSigner) ([]byte, error) {
	if signer == nil {
		return nil, ErrSegmentUnsigned
	}
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
	// The signature covers everything written so far (body + crc) plus
	// the pubkey bytes we're about to append — that binds the key to
	// this particular segment and prevents a forged segment from
	// claiming a different key.
	buf.Write(signer.pub)
	sig := ed25519.Sign(signer.priv, buf.Bytes())
	buf.Write(sig)
	return buf.Bytes(), nil
}

// objectKey is the storage-layer name for a flushed segment.
// Layout: <svc>/<shard>/<seq-prefix>/<startSeq>-<nanos>.lbn
// seq-prefix bucketises seqs in 1M-frame groups to keep per-prefix
// object counts sane for listing. The nanos suffix is assigned once per
// rotate so two concurrent flushes of the same seq range (after a
// crash-then-restart) land at different keys and never overwrite each
// other. Readers deduplicate by (startSeq, frameIndex).
func objectKey(svcPrefix, shardID string, startSeq uint64, nanos int64) string {
	const bucketSize = 1_000_000
	return fmt.Sprintf("%s/%s/%016d/%020d-%020d.lbn",
		svcPrefix, shardID, startSeq/bucketSize, startSeq, nanos)
}

// parseObjectKey extracts (startSeq, nanos) from an objectKey-produced
// path. Returns false if the layout doesn't match — callers filter
// unknown keys before calling.
func parseObjectKey(key string) (startSeq uint64, nanos int64, ok bool) {
	base := key
	if idx := lastByte(key, '/'); idx >= 0 {
		base = key[idx+1:]
	}
	if !endsWith(base, ".lbn") {
		return 0, 0, false
	}
	base = base[:len(base)-len(".lbn")]
	// Expected layout: "<20-digit-startSeq>-<20-digit-nanos>"
	dash := firstByte(base, '-')
	if dash < 0 {
		return 0, 0, false
	}
	var err error
	startSeq, err = parseUnsigned(base[:dash])
	if err != nil {
		return 0, 0, false
	}
	n, err := parseUnsigned(base[dash+1:])
	if err != nil {
		return 0, 0, false
	}
	return startSeq, int64(n), true
}

// decodedSegment is a parsed segment ready for iteration.
type decodedSegment struct {
	ShardID  string
	StartSeq uint64
	Frames   [][]byte
}

// decodeSegment parses bytes into a decodedSegment, validating magic,
// CRC, and Ed25519 signature. A nil verifier rejects every segment
// (fail-closed). Any malformed input returns ErrSegmentCorrupt wrapped
// with context.
func decodeSegment(b []byte, v *segmentVerifier) (*decodedSegment, error) {
	if v == nil {
		return nil, ErrSegmentUnsigned
	}
	if len(b) < segmentHeaderMinLen+segmentFooterLen {
		return nil, fmt.Errorf("%w: too short (%d)", ErrSegmentCorrupt, len(b))
	}
	magic := string(b[:segmentMagicLen])
	switch magic {
	case segmentMagic:
		// Proceed.
	case segmentMagicV1:
		// LBN1 was unauthenticated. Reject outright: we never trust
		// the older format (development-only) in any post-fix binary.
		return nil, fmt.Errorf("%w: refusing LBN1 segment (unauthenticated legacy format)", ErrSegmentCorrupt)
	default:
		return nil, fmt.Errorf("%w: bad magic %q", ErrSegmentCorrupt, magic)
	}

	// Footer is: crc32 + pubkey + signature.
	sigStart := len(b) - segmentSigLen
	pubStart := sigStart - segmentPubKeyLen
	crcStart := pubStart - segmentFooterCRCLen
	if crcStart < segmentHeaderMinLen {
		return nil, fmt.Errorf("%w: footer overlaps header", ErrSegmentCorrupt)
	}

	body := b[:crcStart]
	wantCRC := binary.BigEndian.Uint32(b[crcStart:pubStart])
	pub := b[pubStart:sigStart]
	sig := b[sigStart:]
	if got := crc32.ChecksumIEEE(body); got != wantCRC {
		return nil, fmt.Errorf("%w: crc mismatch (got %08x want %08x)", ErrSegmentCorrupt, got, wantCRC)
	}
	if _, ok := v.allowed[string(pub)]; !ok {
		return nil, fmt.Errorf("%w: unknown signer key", ErrSegmentCorrupt)
	}
	// Signature covers body || crc || pubkey.
	if !ed25519.Verify(pub, b[:pubStart+segmentPubKeyLen], sig) {
		return nil, fmt.Errorf("%w: signature invalid", ErrSegmentCorrupt)
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

// --- tiny local helpers used by parseObjectKey. Keeping them local
// avoids pulling strconv/strings into this tight hot path and keeps
// segment.go self-contained. ---

func firstByte(s string, c byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == c {
			return i
		}
	}
	return -1
}

func lastByte(s string, c byte) int {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == c {
			return i
		}
	}
	return -1
}

func endsWith(s, suffix string) bool {
	return len(s) >= len(suffix) && s[len(s)-len(suffix):] == suffix
}

func parseUnsigned(s string) (uint64, error) {
	if s == "" {
		return 0, errors.New("empty uint")
	}
	var v uint64
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c < '0' || c > '9' {
			return 0, fmt.Errorf("invalid uint digit %q", c)
		}
		v = v*10 + uint64(c-'0')
	}
	return v, nil
}
