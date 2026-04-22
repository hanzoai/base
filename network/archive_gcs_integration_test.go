//go:build integration && gcs

package network

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"
)

func TestGCSIntegrationRoundTrip(t *testing.T) {
	if os.Getenv("STORAGE_EMULATOR_HOST") == "" {
		t.Skip("STORAGE_EMULATOR_HOST unset; start fake-gcs-server per the header of archive_integration_test.go")
	}
	bucket := os.Getenv("BASE_ARCHIVE_TEST_BUCKET")
	if bucket == "" {
		bucket = "base-archive-gcs-test"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	a, err := NewArchive(ctx, ArchiveConfig{
		URL:                fmt.Sprintf("gs://%s", bucket),
		SegmentTargetBytes: 4096,
		FlushInterval:      100 * time.Millisecond,
		RetryDeadline:      30 * time.Second,
	}, "svc-gitest", nil)
	if err != nil {
		t.Fatalf("NewArchive gs: %v", err)
	}
	t.Cleanup(func() { _ = a.Close() })

	const n = 200
	for i := uint64(1); i <= n; i++ {
		f := newFrame("shard-g", i, i-1, []byte(fmt.Sprintf("p%d", i)))
		if err := a.Append(ctx, "shard-g", i, f.encode()); err != nil {
			t.Fatalf("append: %v", err)
		}
	}
	if err := a.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	a2, err := NewArchive(ctx, ArchiveConfig{URL: fmt.Sprintf("gs://%s", bucket)}, "svc-gitest", nil)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	t.Cleanup(func() { _ = a2.Close() })

	it, err := a2.Range(ctx, "shard-g", 1, n)
	if err != nil {
		t.Fatalf("range: %v", err)
	}
	var got uint64
	for f, err := range it {
		if err != nil {
			t.Fatalf("iter: %v", err)
		}
		got++
		if f.Seq != got {
			t.Fatalf("seq: want %d got %d", got, f.Seq)
		}
	}
	if got != n {
		t.Fatalf("want %d frames, got %d", n, got)
	}
}
