// Package network archive layer.
//
// Archive writes quasar-finalised WAL frames to object storage (S3 or GCS)
// as per-shard segment files. Each segment is a length-prefixed list of
// PQ-signed frames that can be replayed for point-in-time recovery.
//
// Segment format: magic "LBN2" (authenticated — body+crc+pubkey signed by
// Ed25519). Version 1 ("LBN1") existed only during dev and is rejected
// unconditionally by readers to prevent downgrade attacks. Future format
// changes MUST bump the magic to "LBN3"/... and version-detect on read;
// never overload an existing magic — forwards compatibility only.
//
// This file defines the Archive interface and the URL-based backend
// dispatcher. The consensus-side wiring (witness validator, frame feed)
// is owned by the core network package; archive is a dumb consumer of
// already-finalised data.
package network

import (
	"context"
	"crypto/ed25519"
	"errors"
	"fmt"
	"iter"
	"net/url"
	"strings"
	"time"
)

// Frame is defined in wal.go; the archive uses the ShardID, Seq and Bytes
// fields to write and replay quasar-finalised frames. Bytes is the exact
// serialized frame as it exited consensus — the archive never re-signs.

// Archive is the write+replay interface against object storage.
//
// Append is called per finalised frame by the witness validator loop.
// It MUST buffer and return quickly; flushing to storage happens
// asynchronously. A slow backend must back-pressure via metrics, not
// by blocking Append indefinitely.
//
// Range yields frames in seq order across segments overlapping
// [fromSeq, toSeq]. Iteration stops at the first error yielded.
type Archive interface {
	Append(ctx context.Context, shardID string, seq uint64, frame []byte) error
	Range(ctx context.Context, shardID string, fromSeq, toSeq uint64) (iter.Seq2[Frame, error], error)
	Close() error
}

// ArchiveConfig controls segment size + flush cadence. Zero values
// fall back to defaults (8 MiB, 10 s).
type ArchiveConfig struct {
	// URL is the backend destination: s3://bucket/prefix or
	// gs://bucket/prefix. Prefix is optional; if absent, objects land
	// at the bucket root.
	URL string

	// SegmentTargetBytes is the size at which an in-memory segment
	// flushes. Defaults to 8 MiB.
	SegmentTargetBytes int

	// FlushInterval is the max time a partial segment may linger in
	// memory before being flushed. Defaults to 10 s.
	FlushInterval time.Duration

	// RetryDeadline bounds transient retry attempts for a single
	// segment flush. Defaults to 5 minutes. After it elapses, the
	// segment is retained for the next flush cycle (never dropped).
	RetryDeadline time.Duration

	// SigningKey is the Ed25519 private key used to sign every segment
	// we write. Production callers MUST inject a KMS-held key; when
	// unset (tests, dev), the archive writer auto-generates a transient
	// key — fine in-process, useless across restarts.
	SigningKey ed25519.PrivateKey

	// TrustedSegmentKeys are Ed25519 public keys whose segments the
	// reader accepts. The local SigningKey's public half is always
	// trusted; additional keys cover rotation (PITR against archives
	// written under a now-retired key).
	TrustedSegmentKeys []ed25519.PublicKey

	// BacklogMaxBytes caps the per-shard in-memory backlog of encoded
	// segments waiting to upload. Once exceeded, the oldest segments
	// are dropped (counted on IncDrops). Zero = unbounded (dev only);
	// production callers MUST set this to a sane ceiling.
	BacklogMaxBytes int

	// BacklogMaxSegments caps the per-shard backlog by segment count.
	// Either limit firing triggers the drop path. Zero = unbounded.
	BacklogMaxSegments int
}

// Defaults applied when a field is zero. R6: BacklogMax* cap per-shard
// in-memory backlog so a hostile / broken storage backend cannot OOM
// the process. 64 MiB / 100 000 segments is "enough headroom for a real
// S3 outage" but "small enough that even 32 shards on a 2 GiB pod can't
// OOM". Operators override via `BASE_SHARD_BACKLOG_MAX` and
// `BASE_SHARD_BACKLOG_SEGMENTS`.
const (
	DefaultSegmentTargetBytes = 8 * 1024 * 1024
	DefaultFlushInterval      = 10 * time.Second
	DefaultRetryDeadline      = 5 * time.Minute
	DefaultBacklogMaxBytes    = 64 * 1024 * 1024
	DefaultBacklogMaxSegments = 100_000
)

func (c ArchiveConfig) withDefaults() ArchiveConfig {
	if c.SegmentTargetBytes <= 0 {
		c.SegmentTargetBytes = DefaultSegmentTargetBytes
	}
	if c.FlushInterval <= 0 {
		c.FlushInterval = DefaultFlushInterval
	}
	if c.RetryDeadline <= 0 {
		c.RetryDeadline = DefaultRetryDeadline
	}
	if c.BacklogMaxBytes <= 0 {
		c.BacklogMaxBytes = DefaultBacklogMaxBytes
	}
	if c.BacklogMaxSegments <= 0 {
		c.BacklogMaxSegments = DefaultBacklogMaxSegments
	}
	return c
}

// ArchiveMetrics is a callback-based view onto the core Metrics
// collectors declared in metrics.go. Archive code stays decoupled from
// the Prometheus registry by going through function hooks — callers
// bind SetLagBytes to Metrics.ArchiveLagBytes.Set and IncFailures to
// a counter of their choice. A nil ArchiveMetrics is valid; both
// hooks become no-ops.
type ArchiveMetrics struct {
	// SetLagBytes reports the total size of buffered, not-yet-flushed
	// frames across all shards. Surfaced as base_archive_lag_bytes.
	SetLagBytes func(bytes int64)
	// IncFailures increments a failures counter whenever a segment
	// exhausts its retry deadline. The segment is re-buffered unless
	// the backlog cap is exceeded.
	IncFailures func()
	// IncDrops increments a drop counter whenever the backlog cap
	// sheds the oldest segment. Liveness-over-durability — operators
	// should page on this.
	IncDrops func()
}

// BindArchiveMetrics builds an ArchiveMetrics that writes through to
// the core Metrics collectors. Callers pass the returned value into
// NewArchive. Nil-safe: a nil Metrics yields a nil-out ArchiveMetrics.
func BindArchiveMetrics(m *Metrics) *ArchiveMetrics {
	if m == nil {
		return nil
	}
	return &ArchiveMetrics{
		SetLagBytes: func(b int64) { m.ArchiveLagBytes.Set(float64(b)) },
		IncFailures: func() { m.ArchiveFailures.Inc() },
		IncDrops:    func() { m.ArchiveDrops.Inc() },
	}
}

// NewArchive dispatches on the URL scheme. Currently supported:
// s3://   → MinIO-protocol S3 (hanzoai/s3 self-hosted or AWS).
// gs://   → Google Cloud Storage.
// off     → nil Archive, disabled (call sites treat as no-op).
// Anything else is a config error.
func NewArchive(ctx context.Context, cfg ArchiveConfig, svc string, m *ArchiveMetrics) (Archive, error) {
	cfg = cfg.withDefaults()
	if cfg.URL == "" || strings.EqualFold(cfg.URL, "off") {
		return nil, nil //nolint:nilnil // disabled archive is a valid state
	}
	u, err := url.Parse(cfg.URL)
	if err != nil {
		return nil, fmt.Errorf("archive: parse %q: %w", cfg.URL, err)
	}
	if u.Host == "" {
		return nil, fmt.Errorf("archive: url %q has no bucket", cfg.URL)
	}
	prefix := strings.Trim(u.Path, "/")
	if svc == "" {
		return nil, errors.New("archive: svc is required")
	}
	// Service name prefixes every object: <svc>/<shard>/<seq-prefix>/<segment>.lbn
	// If the URL already carries a path prefix, compose them.
	objPrefix := svc
	if prefix != "" {
		objPrefix = prefix + "/" + svc
	}
	switch strings.ToLower(u.Scheme) {
	case "s3":
		return newS3Archive(ctx, u.Host, objPrefix, cfg, m)
	case "gs":
		return newGCSArchive(ctx, u.Host, objPrefix, cfg, m)
	default:
		return nil, fmt.Errorf("archive: unsupported scheme %q (want s3:// or gs://)", u.Scheme)
	}
}
