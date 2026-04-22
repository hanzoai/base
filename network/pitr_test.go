//go:build integration

package network

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

// TestPITRRestore smoke-tests the hack/pitr-restore CLI by writing
// 100 frames to an S3 archive, running the CLI to restore up to
// seq 50, and asserting the resulting SQLite file has exactly 50
// rows.
//
// Requires the same MinIO container as TestS3IntegrationRoundTrip —
// AWS_ENDPOINT_URL, AWS_ACCESS_KEY_ID, AWS_SECRET_ACCESS_KEY env.
func TestPITRRestore(t *testing.T) {
	bucket := skipWithoutMinIO(t)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	const svc = "svc-pitr"
	const shard = "shard-pitr"

	a, err := NewArchive(ctx, ArchiveConfig{
		URL:                fmt.Sprintf("s3://%s", bucket),
		SegmentTargetBytes: 1024,
		FlushInterval:      50 * time.Millisecond,
		RetryDeadline:      30 * time.Second,
	}, svc, nil)
	if err != nil {
		t.Fatalf("open archive: %v", err)
	}
	for i := uint64(1); i <= 100; i++ {
		f := newFrame(shard, i, i-1, []byte(fmt.Sprintf("payload-%d", i)))
		if err := a.Append(ctx, shard, i, f.encode()); err != nil {
			t.Fatalf("append %d: %v", i, err)
		}
	}
	if err := a.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	// Build the CLI.
	tmpDir := t.TempDir()
	bin := filepath.Join(tmpDir, "pitr-restore")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	build := exec.CommandContext(ctx, "go", "build", "-o", bin, "./hack")
	build.Dir = repoRootFromCaller(t)
	var bstderr bytes.Buffer
	build.Stderr = &bstderr
	if err := build.Run(); err != nil {
		t.Fatalf("build cli: %v; stderr: %s", err, bstderr.String())
	}

	// Run the CLI with --to-seq=50.
	outFile := filepath.Join(tmpDir, "restored.db")
	cmd := exec.CommandContext(ctx, bin,
		"--archive", fmt.Sprintf("s3://%s/%s", bucket, svc),
		"--shard", shard,
		"--to-seq", "50",
		"--out", outFile,
	)
	cmd.Env = append(os.Environ(),
		"AWS_ENDPOINT_URL="+os.Getenv("AWS_ENDPOINT_URL"),
		"AWS_ACCESS_KEY_ID="+os.Getenv("AWS_ACCESS_KEY_ID"),
		"AWS_SECRET_ACCESS_KEY="+os.Getenv("AWS_SECRET_ACCESS_KEY"),
		"AWS_REGION="+envOrDefault("AWS_REGION", "us-east-1"),
	)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("cli failed: %v\nstdout: %s\nstderr: %s", err, out, stderr.String())
	}
	t.Logf("cli: %s", bytes.TrimSpace(out))

	// Verify the SQLite file has exactly 50 rows with seqs 1..50.
	db, err := sql.Open("sqlite", outFile)
	if err != nil {
		t.Fatalf("open restored db: %v", err)
	}
	defer db.Close()
	var count, minSeq, maxSeq int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*), IFNULL(MIN(seq),0), IFNULL(MAX(seq),0) FROM wal_frames").Scan(&count, &minSeq, &maxSeq); err != nil {
		t.Fatalf("query: %v", err)
	}
	if count != 50 {
		t.Fatalf("want 50 rows, got %d", count)
	}
	if minSeq != 1 || maxSeq != 50 {
		t.Fatalf("seq bounds: want [1,50], got [%d,%d]", minSeq, maxSeq)
	}
}

func envOrDefault(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

// repoRootFromCaller walks up from this test file to the base/ root so
// the CLI `go build` invocation runs at the module root where `./hack`
// resolves correctly.
func repoRootFromCaller(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatalf("cannot locate caller file")
	}
	// file = .../base/network/pitr_test.go → ../ = .../base
	return filepath.Dir(filepath.Dir(file))
}
