package encreplica

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"os"
	"testing"
	"time"

	"github.com/hanzoai/ltx"
	"github.com/luxfi/age"
)

// ltxFile returns a byte slice whose first 100 bytes are a valid LTX header —
// enough for PeekHeader, which is all this client reads — followed by a body
// standing in for database pages. The body is what must never reach storage in
// the clear.
func ltxFile(t *testing.T, minTXID, maxTXID ltx.TXID, ts int64, body []byte) []byte {
	t.Helper()
	hdr := ltx.Header{
		Version:   ltx.Version,
		PageSize:  4096,
		Commit:    1,
		MinTXID:   minTXID,
		MaxTXID:   maxTXID,
		Timestamp: ts,
	}
	b, err := hdr.MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary: %v", err)
	}
	return append(b, body...)
}

func newClient(t *testing.T) (*Client, *LocalBlobs, age.Identity) {
	t.Helper()
	id, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatalf("GenerateX25519Identity: %v", err)
	}
	blobs := NewLocalBlobs(t.TempDir())
	c, err := New(blobs, id.Recipient(), id)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return c, blobs, id
}

// The whole reason this client exists: what lands in the backend is ciphertext.
// If the body is readable in the blob, every base's SQLite pages are sitting in
// object storage in the clear and nothing else in the stack would notice.
func TestTheBodyNeverReachesStorageInTheClear(t *testing.T) {
	c, blobs, _ := newClient(t)
	ctx := context.Background()

	secret := []byte("PAGE-DATA-6f1a2b-DO-NOT-LEAK")
	plain := ltxFile(t, 1, 2, time.Now().UnixMilli(), secret)

	info, err := c.WriteLTXFile(ctx, 0, 1, 2, bytes.NewReader(plain))
	if err != nil {
		t.Fatalf("WriteLTXFile: %v", err)
	}
	if info.Size != int64(len(plain)) {
		t.Fatalf("FileInfo.Size = %d, want %d", info.Size, len(plain))
	}

	keys, err := blobs.List(ctx, "ltx/0/")
	if err != nil || len(keys) != 1 {
		t.Fatalf("List = %v, %v; want exactly one key", keys, err)
	}
	blob, err := blobs.Get(ctx, keys[0])
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if bytes.Contains(blob, secret) {
		t.Fatal("the plaintext body is present in the stored blob")
	}
	// The header must not travel in the clear either — only the 16-byte prefix
	// this client writes itself is cleartext.
	if bytes.Contains(blob[prefixLen:], plain[:ltx.HeaderSize]) {
		t.Fatal("the LTX header is present unencrypted in the stored blob")
	}
}

// The cleartext prefix is deliberate and its shape is load-bearing: restore
// reads Size and CreatedAt without decrypting, so both must survive the round
// trip exactly.
func TestTheClearPrefixCarriesSizeAndTimestampOnly(t *testing.T) {
	c, blobs, _ := newClient(t)
	ctx := context.Background()

	ts := time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC).UnixMilli()
	plain := ltxFile(t, 3, 4, ts, bytes.Repeat([]byte("x"), 512))

	if _, err := c.WriteLTXFile(ctx, 1, 3, 4, bytes.NewReader(plain)); err != nil {
		t.Fatalf("WriteLTXFile: %v", err)
	}
	keys, _ := blobs.List(ctx, "ltx/1/")
	blob, err := blobs.Get(ctx, keys[0])
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got := int64(binary.BigEndian.Uint64(blob[0:8])); got != int64(len(plain)) {
		t.Fatalf("prefix length = %d, want %d", got, len(plain))
	}
	if got := int64(binary.BigEndian.Uint64(blob[8:16])); got != ts {
		t.Fatalf("prefix timestamp = %d, want %d", got, ts)
	}

	// And LTXFiles reports that back without ever decrypting.
	it, err := c.LTXFiles(ctx, 1, 0, false)
	if err != nil {
		t.Fatalf("LTXFiles: %v", err)
	}
	n := 0
	for it.Next() {
		fi := it.Item()
		n++
		if fi.Size != int64(len(plain)) {
			t.Fatalf("FileInfo.Size = %d, want %d", fi.Size, len(plain))
		}
		if !fi.CreatedAt.Equal(time.UnixMilli(ts).UTC()) {
			t.Fatalf("FileInfo.CreatedAt = %v, want %v", fi.CreatedAt, time.UnixMilli(ts).UTC())
		}
	}
	if n != 1 {
		t.Fatalf("LTXFiles yielded %d files, want 1", n)
	}
}

// Written then read back is the original, byte for byte, including through the
// offset/size window replicate's resumable reader drives.
func TestRoundTripAndWindowing(t *testing.T) {
	c, _, _ := newClient(t)
	ctx := context.Background()

	body := []byte("0123456789abcdef")
	plain := ltxFile(t, 5, 6, time.Now().UnixMilli(), body)
	if _, err := c.WriteLTXFile(ctx, 0, 5, 6, bytes.NewReader(plain)); err != nil {
		t.Fatalf("WriteLTXFile: %v", err)
	}

	rc, err := c.OpenLTXFile(ctx, 0, 5, 6, 0, 0)
	if err != nil {
		t.Fatalf("OpenLTXFile: %v", err)
	}
	got, _ := io.ReadAll(rc)
	rc.Close()
	if !bytes.Equal(got, plain) {
		t.Fatalf("round trip changed the file: %d bytes back, want %d", len(got), len(plain))
	}

	// A window into the plaintext: skip the header, take four bytes.
	rc, err = c.OpenLTXFile(ctx, 0, 5, 6, int64(ltx.HeaderSize), 4)
	if err != nil {
		t.Fatalf("OpenLTXFile(window): %v", err)
	}
	got, _ = io.ReadAll(rc)
	rc.Close()
	if string(got) != "0123" {
		t.Fatalf("window = %q, want %q", got, "0123")
	}

	// An offset past the end yields nothing rather than panicking.
	rc, err = c.OpenLTXFile(ctx, 0, 5, 6, int64(len(plain))+100, 0)
	if err != nil {
		t.Fatalf("OpenLTXFile(past end): %v", err)
	}
	got, _ = io.ReadAll(rc)
	rc.Close()
	if len(got) != 0 {
		t.Fatalf("read %d bytes past the end, want 0", len(got))
	}
}

// Another base's key opens nothing. This is the per-org boundary: the backend is
// shared, the keys are not, and a client holding the wrong identity must fail
// rather than return something.
func TestAnotherBasesKeyCannotOpenIt(t *testing.T) {
	c, blobs, _ := newClient(t)
	ctx := context.Background()

	plain := ltxFile(t, 7, 8, time.Now().UnixMilli(), []byte("private"))
	if _, err := c.WriteLTXFile(ctx, 0, 7, 8, bytes.NewReader(plain)); err != nil {
		t.Fatalf("WriteLTXFile: %v", err)
	}

	other, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatalf("GenerateX25519Identity: %v", err)
	}
	intruder, err := New(blobs, other.Recipient(), other)
	if err != nil {
		t.Fatalf("New(intruder): %v", err)
	}
	if _, err := intruder.OpenLTXFile(ctx, 0, 7, 8, 0, 0); err == nil {
		t.Fatal("a different base's key opened the segment")
	}
}

// A tampered ciphertext is refused, not returned. age is authenticated, so this
// is really asserting that the client does not swallow the failure.
func TestTamperedCiphertextIsRefused(t *testing.T) {
	c, blobs, _ := newClient(t)
	ctx := context.Background()

	plain := ltxFile(t, 9, 10, time.Now().UnixMilli(), []byte("intact"))
	if _, err := c.WriteLTXFile(ctx, 0, 9, 10, bytes.NewReader(plain)); err != nil {
		t.Fatalf("WriteLTXFile: %v", err)
	}
	keys, _ := blobs.List(ctx, "ltx/0/")
	blob, _ := blobs.Get(ctx, keys[0])
	blob[len(blob)-1] ^= 0xff // flip a bit in the ciphertext
	if err := blobs.Put(ctx, keys[0], blob); err != nil {
		t.Fatalf("Put: %v", err)
	}

	if _, err := c.OpenLTXFile(ctx, 0, 9, 10, 0, 0); err == nil {
		t.Fatal("a tampered segment was returned as if intact")
	}
}

// A missing segment must keep os.ErrNotExist, because replicate distinguishes
// "not replicated yet" from "the backend is broken" on exactly that.
func TestMissingSegmentPreservesErrNotExist(t *testing.T) {
	c, _, _ := newClient(t)
	_, err := c.OpenLTXFile(context.Background(), 0, 99, 100, 0, 0)
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("err = %v, want it to satisfy os.ErrNotExist", err)
	}
}
