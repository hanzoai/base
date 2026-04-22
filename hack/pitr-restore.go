// Command pitr-restore reads archived WAL frames out of S3 or GCS and
// replays them into a fresh SQLite file for point-in-time recovery.
//
// Usage:
//
//	pitr-restore --archive gs://bucket/svc --shard <id> --to-seq <n> --out <file.db>
//
// Think of it as `restic restore` for Base. No mutation of the remote
// archive; the tool is read-only on the cold store and writes locally.
//
// The tool opens a fresh SQLite database at --out (fails if it already
// exists unless --force). Each frame's Payload is written into a
// "wal_frames" table keyed by seq — the Base core apply path would
// replay those into the real schema, but for a PITR smoke test it is
// enough to prove the frame round-trips and lands in the target file
// with the expected row count.
package main

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/hanzoai/base/network"

	// modernc sqlite has no cgo — matches core/base.
	_ "modernc.org/sqlite"
)

func main() {
	var (
		archiveURL = flag.String("archive", "", "archive URL (s3://bucket/svc | gs://bucket/svc)")
		shard      = flag.String("shard", "", "shard ID to restore")
		toSeq      = flag.Uint64("to-seq", 0, "restore frames up to and including this seq (0 = all)")
		out        = flag.String("out", "", "path to the target SQLite file")
		force      = flag.Bool("force", false, "overwrite --out if it exists")
		svc        = flag.String("svc", "", "service name (inferred from --archive path if omitted)")
		timeout    = flag.Duration("timeout", 5*time.Minute, "overall deadline")
	)
	flag.Parse()

	if *archiveURL == "" || *shard == "" || *out == "" {
		fmt.Fprintln(os.Stderr, "pitr-restore: --archive, --shard, --out are required")
		flag.Usage()
		os.Exit(2)
	}

	// The archive URL may encode both the bucket and the service:
	//   gs://bucket/svc  → bucket=bucket, svc=svc
	// We pass the full URL straight to network.NewArchive and let it
	// split the path, but NewArchive needs an svc arg. Simplest: treat
	// --svc as override, otherwise derive from the last path segment.
	bucketURL, svcName, err := splitArchiveURL(*archiveURL, *svc)
	if err != nil {
		log.Fatalf("parse --archive: %v", err)
	}

	if _, err := os.Stat(*out); err == nil {
		if !*force {
			log.Fatalf("refusing to overwrite %s (pass --force)", *out)
		}
		if err := os.Remove(*out); err != nil {
			log.Fatalf("remove existing --out: %v", err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		log.Fatalf("stat --out: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	a, err := network.NewArchive(ctx, network.ArchiveConfig{URL: bucketURL}, svcName, nil)
	if err != nil {
		log.Fatalf("open archive: %v", err)
	}
	if a == nil {
		log.Fatalf("archive %q resolved to disabled — nothing to restore", *archiveURL)
	}
	defer a.Close()

	db, err := sql.Open("sqlite", *out)
	if err != nil {
		log.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()
	if _, err := db.ExecContext(ctx, `
		CREATE TABLE wal_frames (
			seq      INTEGER PRIMARY KEY,
			prev_seq INTEGER NOT NULL,
			ts       INTEGER NOT NULL,
			payload  BLOB NOT NULL,
			cksm     BLOB NOT NULL
		)`); err != nil {
		log.Fatalf("create schema: %v", err)
	}

	upper := *toSeq
	if upper == 0 {
		upper = ^uint64(0)
	}
	it, err := a.Range(ctx, *shard, 0, upper)
	if err != nil {
		log.Fatalf("range: %v", err)
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		log.Fatalf("begin tx: %v", err)
	}
	stmt, err := tx.PrepareContext(ctx,
		`INSERT INTO wal_frames(seq, prev_seq, ts, payload, cksm) VALUES(?,?,?,?,?)`)
	if err != nil {
		log.Fatalf("prepare: %v", err)
	}

	var n uint64
	for f, err := range it {
		if err != nil {
			_ = tx.Rollback()
			log.Fatalf("iter: %v", err)
		}
		if err := f.Valid(); err != nil {
			_ = tx.Rollback()
			log.Fatalf("frame seq %d checksum: %v", f.Seq, err)
		}
		if _, err := stmt.ExecContext(ctx, f.Seq, f.PrevSeq, f.Timestamp, f.Payload, f.Cksm[:]); err != nil {
			_ = tx.Rollback()
			log.Fatalf("insert seq %d: %v", f.Seq, err)
		}
		n++
	}
	if err := stmt.Close(); err != nil {
		_ = tx.Rollback()
		log.Fatalf("stmt close: %v", err)
	}
	if err := tx.Commit(); err != nil {
		log.Fatalf("commit: %v", err)
	}
	fmt.Printf("restored %d frames to %s (shard=%s, up-to-seq=%d)\n", n, *out, *shard, *toSeq)
}

// splitArchiveURL accepts scheme://bucket[/prefix/.../svc]. If the
// caller passed --svc we honour it verbatim; otherwise the last
// non-empty path segment is taken as the service name and the URL is
// trimmed to "scheme://bucket/<prefix-without-svc>".
func splitArchiveURL(raw, svcOverride string) (bucketURL, svc string, err error) {
	if svcOverride != "" {
		return raw, svcOverride, nil
	}
	// Locate the scheme separator.
	const sep = "://"
	i := indexOf(raw, sep)
	if i < 0 {
		return "", "", fmt.Errorf("archive url missing scheme: %q", raw)
	}
	scheme := raw[:i]
	tail := raw[i+len(sep):]
	// First path segment is the bucket; the rest forms the prefix.
	slash := indexOf(tail, "/")
	if slash < 0 {
		return "", "", fmt.Errorf("archive url %q has no service suffix; pass --svc", raw)
	}
	bucket := tail[:slash]
	path := tail[slash+1:]
	// Last segment of path = svc.
	lastSlash := lastIndexOf(path, "/")
	if lastSlash < 0 {
		// path IS the svc.
		return scheme + sep + bucket, path, nil
	}
	svc = path[lastSlash+1:]
	prefix := path[:lastSlash]
	return scheme + sep + bucket + "/" + prefix, svc, nil
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

func lastIndexOf(s, sub string) int {
	for i := len(s) - len(sub); i >= 0; i-- {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
