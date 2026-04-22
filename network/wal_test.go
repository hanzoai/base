package network

import (
	"bytes"
	"testing"
)

func TestFrameEncodeDecodeRoundTrip(t *testing.T) {
	f := newFrame("shard-42", 7, 6, []byte("hello"))
	b := f.encode()

	got, err := decodeFrame(b)
	if err != nil {
		t.Fatalf("decodeFrame: %v", err)
	}
	if got.ShardID != f.ShardID {
		t.Errorf("ShardID: got %q want %q", got.ShardID, f.ShardID)
	}
	if got.Seq != f.Seq || got.PrevSeq != f.PrevSeq {
		t.Errorf("seq: got (%d,%d) want (%d,%d)", got.Seq, got.PrevSeq, f.Seq, f.PrevSeq)
	}
	if got.Salt != f.Salt {
		t.Errorf("salt mismatch")
	}
	if got.Cksm != f.Cksm {
		t.Errorf("cksm mismatch")
	}
	if !bytes.Equal(got.Payload, f.Payload) {
		t.Errorf("payload mismatch")
	}
	if !bytes.Equal(got.Bytes, b) {
		t.Errorf("Bytes field should carry raw input back")
	}
	if err := got.Valid(); err != nil {
		t.Errorf("Valid() after round-trip: %v", err)
	}
}

func TestFrameChecksumDetectsTamper(t *testing.T) {
	f := newFrame("s", 1, 0, []byte("payload"))
	b := f.encode()
	// Corrupt one payload byte.
	b[len(b)-33] ^= 0x01

	got, err := decodeFrame(b)
	if err != nil {
		t.Fatalf("decodeFrame: %v", err)
	}
	if err := got.Valid(); err == nil {
		t.Error("expected Valid() to fail on tampered payload")
	}
}

func TestFrameDecodeRejectsTruncated(t *testing.T) {
	f := newFrame("s", 1, 0, []byte("abc"))
	b := f.encode()
	_, err := decodeFrame(b[:len(b)-1])
	if err == nil {
		t.Error("expected error on truncated input")
	}
}

func TestFrameApplyKeyStable(t *testing.T) {
	f := newFrame("s", 1, 0, []byte("x"))
	k1 := f.ApplyKey()
	// Re-decode and check key stability.
	g, _ := decodeFrame(f.encode())
	k2 := g.ApplyKey()
	if k1 != k2 {
		t.Error("ApplyKey must be stable across encode/decode")
	}
}

func TestFrameApplyKeyUnique(t *testing.T) {
	a := newFrame("s", 1, 0, []byte("x"))
	b := newFrame("s", 1, 0, []byte("x"))
	if a.ApplyKey() == b.ApplyKey() {
		t.Error("two frames with independent salts must not share ApplyKey")
	}
}

func TestFrameDecodeRejectsBadVersion(t *testing.T) {
	f := newFrame("s", 1, 0, nil)
	b := f.encode()
	b[0] = 0xFE
	if _, err := decodeFrame(b); err == nil {
		t.Error("expected version-mismatch error")
	}
}
