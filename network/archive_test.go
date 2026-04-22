package network

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"iter"
	"sync"
	"testing"
	"time"
)

// makeFrame builds a Frame with a deterministic payload so tests can
// assert exact bytes on the way out.
func makeFrame(t *testing.T, shard string, seq uint64, payload string) Frame {
	t.Helper()
	return newFrame(shard, seq, seq-1, []byte(payload))
}

// TestSegmentHexDump prints the first 32 bytes of a minimal segment
// so a grep of test output proves the on-wire format matches the doc:
//
//	"LBN1" | shardIDLen uint16 | shardID | startSeq uint64 | frameCount uint32 | ...
func TestSegmentHexDump(t *testing.T) {
	sb := newSegmentBuffer("svc-shard-01", 42)
	_ = sb.append(42, []byte{0xAA, 0xBB, 0xCC, 0xDD})
	enc, err := sb.encode()
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	head := enc
	if len(head) > 32 {
		head = head[:32]
	}
	// Expected layout for shardID="svc-shard-01" (12 bytes):
	//   4c424e31          "LBN1"
	//   000c              uint16 len=12
	//   73 76 63 2d 73 68 61 72 64 2d 30 31   "svc-shard-01"
	//   000000000000002a  uint64 startSeq=42
	//   00000001          uint32 frameCount=1 (next 2 bytes start of frame_len)
	// 4 + 2 + 12 + 8 + 4 = 30 header bytes; first 32 bytes cover the header + 2 bytes of frame_len (big-endian uint32: 00 00 ...)
	t.Logf("segment first %d bytes hex: %x", len(head), head)
	if string(enc[:4]) != "LBN1" {
		t.Fatalf("magic mismatch: %q", string(enc[:4]))
	}
}

func TestSegmentRoundTrip(t *testing.T) {
	sb := newSegmentBuffer("shard-A", 100)
	frames := make([]Frame, 0, 8)
	for i := uint64(0); i < 8; i++ {
		f := makeFrame(t, "shard-A", 100+i, fmt.Sprintf("payload-%d", i))
		frames = append(frames, f)
		if err := sb.append(100+i, f.encode()); err != nil {
			t.Fatalf("append %d: %v", i, err)
		}
	}
	enc, err := sb.encode()
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	dec, err := decodeSegment(enc)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if dec.ShardID != "shard-A" {
		t.Fatalf("shard id: want shard-A got %q", dec.ShardID)
	}
	if dec.StartSeq != 100 {
		t.Fatalf("start seq: want 100 got %d", dec.StartSeq)
	}
	if len(dec.Frames) != len(frames) {
		t.Fatalf("frame count: want %d got %d", len(frames), len(dec.Frames))
	}
	for i, raw := range dec.Frames {
		got, err := decodeFrame(raw)
		if err != nil {
			t.Fatalf("decode frame %d: %v", i, err)
		}
		if err := got.Valid(); err != nil {
			t.Fatalf("frame %d invalid: %v", i, err)
		}
		if got.Seq != frames[i].Seq {
			t.Fatalf("frame %d seq: want %d got %d", i, frames[i].Seq, got.Seq)
		}
		if !bytes.Equal(got.Payload, frames[i].Payload) {
			t.Fatalf("frame %d payload mismatch", i)
		}
	}
}

func TestSegmentCorruptionCRC(t *testing.T) {
	sb := newSegmentBuffer("s", 1)
	_ = sb.append(1, makeFrame(t, "s", 1, "hi").encode())
	enc, err := sb.encode()
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	// Flip a byte in the middle of the body (not the CRC footer).
	enc[len(enc)/2] ^= 0x01
	if _, err := decodeSegment(enc); !errors.Is(err, ErrSegmentCorrupt) {
		t.Fatalf("want ErrSegmentCorrupt, got %v", err)
	}
}

func TestSegmentCorruptionMagic(t *testing.T) {
	sb := newSegmentBuffer("s", 1)
	_ = sb.append(1, makeFrame(t, "s", 1, "hi").encode())
	enc, _ := sb.encode()
	copy(enc[:4], "XXXX")
	_, err := decodeSegment(enc)
	if !errors.Is(err, ErrSegmentCorrupt) {
		t.Fatalf("want ErrSegmentCorrupt, got %v", err)
	}
}

func TestSegmentCorruptionTruncated(t *testing.T) {
	sb := newSegmentBuffer("s", 1)
	_ = sb.append(1, makeFrame(t, "s", 1, "hi").encode())
	enc, _ := sb.encode()
	if _, err := decodeSegment(enc[:len(enc)/2]); !errors.Is(err, ErrSegmentCorrupt) {
		t.Fatalf("want ErrSegmentCorrupt on truncation, got %v", err)
	}
}

func TestSegmentOutOfOrderAppend(t *testing.T) {
	sb := newSegmentBuffer("s", 1)
	if err := sb.append(1, []byte("a")); err != nil {
		t.Fatalf("first append: %v", err)
	}
	if err := sb.append(3, []byte("b")); err == nil {
		t.Fatalf("out-of-order append should fail")
	}
}

func TestObjectKeyLayout(t *testing.T) {
	got := objectKey("liquid-bd", "shard-123", 4_200_000)
	// seq 4_200_000 / 1_000_000 = 4 → padded to 16 digits; seq → 20 digits.
	want := "liquid-bd/shard-123/0000000000000004/00000000000004200000.lbn"
	if got != want {
		t.Fatalf("\nwant %s\ngot  %s", want, got)
	}
}

// --- memUploader: in-memory uploader used to test archiveWriter ---

type memUploader struct {
	mu      sync.Mutex
	objects map[string][]byte
	fails   int // return transient error for the first N put calls
	closed  bool
}

func newMemUploader() *memUploader {
	return &memUploader{objects: map[string][]byte{}}
}

func (u *memUploader) put(_ context.Context, key string, body []byte) error {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.fails > 0 {
		u.fails--
		return errors.New("transient")
	}
	dup := make([]byte, len(body))
	copy(dup, body)
	u.objects[key] = dup
	return nil
}

func (u *memUploader) get(_ context.Context, key string) ([]byte, error) {
	u.mu.Lock()
	defer u.mu.Unlock()
	b, ok := u.objects[key]
	if !ok {
		return nil, fmt.Errorf("not found: %s", key)
	}
	return b, nil
}

func (u *memUploader) list(_ context.Context, prefix string) ([]string, error) {
	u.mu.Lock()
	defer u.mu.Unlock()
	var out []string
	for k := range u.objects {
		if len(k) >= len(prefix) && k[:len(prefix)] == prefix {
			out = append(out, k)
		}
	}
	return out, nil
}

func (u *memUploader) close() error {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.closed = true
	return nil
}

func (u *memUploader) scheme() string { return "mem" }

func (u *memUploader) count() int {
	u.mu.Lock()
	defer u.mu.Unlock()
	return len(u.objects)
}

// --- archiveWriter behaviour ---

func TestArchiveWriterAppendAndFlush(t *testing.T) {
	up := newMemUploader()
	lag := int64(-1)
	m := &ArchiveMetrics{
		SetLagBytes: func(b int64) { lag = b },
		IncFailures: func() { t.Fatalf("unexpected failure") },
	}
	w := newArchiveWriter(up, "svc", ArchiveConfig{
		SegmentTargetBytes: 64, // tiny so we rotate frequently
		FlushInterval:      20 * time.Millisecond,
		RetryDeadline:      time.Second,
	}, m)
	t.Cleanup(func() { _ = w.Close() })

	ctx := context.Background()
	for i := uint64(0); i < 50; i++ {
		f := makeFrame(t, "shard-A", i+1, fmt.Sprintf("p%d", i))
		if err := w.Append(ctx, "shard-A", f.Seq, f.encode()); err != nil {
			t.Fatalf("append %d: %v", i, err)
		}
	}
	// Wait until all data has been flushed.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if lag == 0 && up.count() > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if lag != 0 {
		t.Fatalf("lag bytes: want 0, got %d", lag)
	}
	if up.count() == 0 {
		t.Fatalf("no segments uploaded")
	}

	// Read back the full range and verify we get all 50 frames.
	it, err := w.Range(ctx, "shard-A", 1, 50)
	if err != nil {
		t.Fatalf("range: %v", err)
	}
	got := collect(t, it)
	if len(got) != 50 {
		t.Fatalf("want 50 frames, got %d", len(got))
	}
	for i, f := range got {
		if f.Seq != uint64(i+1) {
			t.Fatalf("seq %d: want %d got %d", i, i+1, f.Seq)
		}
		if err := f.Valid(); err != nil {
			t.Fatalf("frame %d invalid: %v", i, err)
		}
	}
}

func TestArchiveWriterTransientRetry(t *testing.T) {
	up := newMemUploader()
	up.fails = 3 // first 3 put calls fail
	failures := 0
	m := &ArchiveMetrics{IncFailures: func() { failures++ }}
	w := newArchiveWriter(up, "svc", ArchiveConfig{
		SegmentTargetBytes: 16,
		FlushInterval:      10 * time.Millisecond,
		RetryDeadline:      5 * time.Second,
	}, m)
	t.Cleanup(func() { _ = w.Close() })

	ctx := context.Background()
	for i := uint64(0); i < 4; i++ {
		f := makeFrame(t, "s", i+1, "x")
		if err := w.Append(ctx, "s", f.Seq, f.encode()); err != nil {
			t.Fatalf("append: %v", err)
		}
	}
	// Wait for the retries to clear.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if up.count() > 0 && up.fails == 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if failures != 0 {
		t.Fatalf("transient retries should not bump failures counter, got %d", failures)
	}
	if up.count() == 0 {
		t.Fatalf("nothing uploaded after transient recovery")
	}
}

func TestArchiveWriterCloseDrains(t *testing.T) {
	up := newMemUploader()
	w := newArchiveWriter(up, "svc", ArchiveConfig{
		SegmentTargetBytes: 1 << 20,   // huge — never rotates on size
		FlushInterval:      time.Hour, // never rotates on time
		RetryDeadline:      time.Second,
	}, nil)

	ctx := context.Background()
	for i := uint64(0); i < 5; i++ {
		f := makeFrame(t, "s", i+1, "y")
		if err := w.Append(ctx, "s", f.Seq, f.encode()); err != nil {
			t.Fatalf("append: %v", err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if up.count() == 0 {
		t.Fatalf("close did not drain")
	}
	if !up.closed {
		t.Fatalf("uploader.close() not called")
	}
}

func TestArchiveWriterRangePartial(t *testing.T) {
	up := newMemUploader()
	w := newArchiveWriter(up, "svc", ArchiveConfig{
		SegmentTargetBytes: 96, // forces several segments
		FlushInterval:      10 * time.Millisecond,
		RetryDeadline:      time.Second,
	}, nil)
	ctx := context.Background()
	for i := uint64(1); i <= 30; i++ {
		f := makeFrame(t, "s", i, fmt.Sprintf("p%02d", i))
		if err := w.Append(ctx, "s", i, f.encode()); err != nil {
			t.Fatalf("append %d: %v", i, err)
		}
	}
	_ = w.Close()

	it, err := w.Range(ctx, "s", 10, 20)
	if err != nil {
		t.Fatalf("range: %v", err)
	}
	got := collect(t, it)
	if len(got) != 11 {
		t.Fatalf("want 11 frames (seq 10..20), got %d", len(got))
	}
	if got[0].Seq != 10 || got[len(got)-1].Seq != 20 {
		t.Fatalf("bounds: first %d last %d", got[0].Seq, got[len(got)-1].Seq)
	}
}

func TestArchiveURLDispatch(t *testing.T) {
	ctx := context.Background()
	if _, err := NewArchive(ctx, ArchiveConfig{URL: "off"}, "svc", nil); err != nil {
		t.Fatalf("off should be accepted: %v", err)
	}
	if a, err := NewArchive(ctx, ArchiveConfig{URL: ""}, "svc", nil); err != nil || a != nil {
		t.Fatalf("empty url: want nil,nil got %v,%v", a, err)
	}
	if _, err := NewArchive(ctx, ArchiveConfig{URL: "ftp://nope"}, "svc", nil); err == nil {
		t.Fatalf("ftp:// should be rejected")
	}
	if _, err := NewArchive(ctx, ArchiveConfig{URL: "s3://bucket"}, "", nil); err == nil {
		t.Fatalf("empty svc should be rejected")
	}
}

// collect exhausts an iter.Seq2 and fails the test on any yielded error.
func collect(t *testing.T, it iter.Seq2[Frame, error]) []Frame {
	t.Helper()
	var out []Frame
	for f, err := range it {
		if err != nil {
			t.Fatalf("iter error: %v", err)
		}
		out = append(out, f)
	}
	return out
}
