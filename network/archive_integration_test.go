//go:build integration

// Integration tests for the S3/MinIO archive backend. Gated behind the
// "integration" build tag so they never run in the standard go test
// cycle. To run:
//
//	docker run --rm -d --name minio-test -p 9000:9000 -p 9001:9001 \
//	    -e MINIO_ROOT_USER=minio -e MINIO_ROOT_PASSWORD=minio1234 \
//	    minio/minio server /data --console-address ":9001"
//	docker exec minio-test mkdir -p /data/base-archive-test
//
//	AWS_ENDPOINT_URL=http://127.0.0.1:9000 \
//	AWS_ACCESS_KEY_ID=minio AWS_SECRET_ACCESS_KEY=minio1234 \
//	go test -tags=integration ./network -run TestS3Integration -v
//
// A GCS variant lives alongside at archive_gcs_integration_test.go,
// gated behind "integration && gcs" with fake-gcs-server.

package network

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"
)

func skipWithoutMinIO(t *testing.T) string {
	t.Helper()
	if os.Getenv("AWS_ENDPOINT_URL") == "" {
		t.Skip("AWS_ENDPOINT_URL unset; start a MinIO container per the file header")
	}
	b := os.Getenv("BASE_ARCHIVE_TEST_BUCKET")
	if b == "" {
		b = "base-archive-test"
	}
	return b
}

func TestS3IntegrationRoundTrip(t *testing.T) {
	bucket := skipWithoutMinIO(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	a, err := NewArchive(ctx, ArchiveConfig{
		URL:                fmt.Sprintf("s3://%s", bucket),
		SegmentTargetBytes: 4096,
		FlushInterval:      100 * time.Millisecond,
		RetryDeadline:      30 * time.Second,
	}, "svc-itest", nil)
	if err != nil {
		t.Fatalf("NewArchive: %v", err)
	}
	if a == nil {
		t.Fatalf("NewArchive returned nil archive")
	}
	t.Cleanup(func() { _ = a.Close() })

	const n = 1000
	for i := uint64(1); i <= n; i++ {
		f := newFrame("shard-itest", i, i-1, []byte(fmt.Sprintf("payload-%d", i)))
		if err := a.Append(ctx, "shard-itest", i, f.encode()); err != nil {
			t.Fatalf("append %d: %v", i, err)
		}
	}
	if err := a.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	// Re-open to read back — proves persistence across archive instances.
	a2, err := NewArchive(ctx, ArchiveConfig{
		URL: fmt.Sprintf("s3://%s", bucket),
	}, "svc-itest", nil)
	if err != nil {
		t.Fatalf("NewArchive reopen: %v", err)
	}
	t.Cleanup(func() { _ = a2.Close() })

	it, err := a2.Range(ctx, "shard-itest", 1, n)
	if err != nil {
		t.Fatalf("Range: %v", err)
	}
	var got uint64
	for f, err := range it {
		if err != nil {
			t.Fatalf("iter err: %v", err)
		}
		got++
		if f.Seq != got {
			t.Fatalf("seq want %d got %d", got, f.Seq)
		}
		if err := f.Valid(); err != nil {
			t.Fatalf("frame %d invalid: %v", got, err)
		}
		want := fmt.Sprintf("payload-%d", got)
		if string(f.Payload) != want {
			t.Fatalf("payload %d: want %q got %q", got, want, string(f.Payload))
		}
	}
	if got != n {
		t.Fatalf("want %d frames, got %d", n, got)
	}
}
