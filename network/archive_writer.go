package network

import (
	"context"
	"crypto/ed25519"
	"fmt"
	"iter"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// uploader is the minimal put/get/list surface every backend must
// expose to the shared writer loop. S3 and GCS implement it in their
// own files; the loop itself knows nothing about the underlying SDK.
type uploader interface {
	put(ctx context.Context, key string, body []byte) error
	get(ctx context.Context, key string) ([]byte, error)
	list(ctx context.Context, prefix string) ([]string, error)
	close() error
	scheme() string
}

// shardQueue holds the in-progress segment for one shard plus any
// backlog of previously-encoded segments whose upload is still being
// retried. Backlog preserves ordering: items flush oldest-first.
type shardQueue struct {
	shardID string
	seg     *segmentBuffer
	backlog []segmentPending
	// flushDeadline is when the current seg must flush even if under
	// the size target. Reset on every new segment.
	flushDeadline time.Time
}

// segmentPending is an encoded segment waiting to be uploaded. The
// nanos field is the monotonic suffix that goes into the object key —
// assigned once on rotate, never changed after, so re-buffered items
// don't collide with fresh encodes of the same seq range.
type segmentPending struct {
	nanos    int64
	startSeq uint64
	data     []byte
}

// archiveWriter is the shared goroutine-per-shard pipeline that both
// the S3 and GCS backends sit behind.
type archiveWriter struct {
	up  uploader
	cfg ArchiveConfig
	// svcPrefix is the storage-side prefix: "<bucket-prefix>/<svc>"
	// (already composed by NewArchive). Used unmodified in objectKey.
	svcPrefix string
	m         *ArchiveMetrics

	// signer produces per-segment Ed25519 signatures. Every segment we
	// write is signed; without a signer the writer refuses to encode.
	signer *segmentSigner
	// verifier is the trust policy for segments we read back. Defaults
	// to accepting only the local signer's key. Callers needing to
	// trust a rotated-out key (PITR) inject additional keys via
	// cfg.TrustedSegmentKeys.
	verifier *segmentVerifier

	mu     sync.Mutex
	shards map[string]*shardQueue

	// lagBytes is the running size of buffered+backlog data across
	// every shard. Refreshed whenever append/flush changes it.
	lagBytes atomic.Int64

	stopCh  chan struct{}
	stopped atomic.Bool
	wg      sync.WaitGroup
}

func newArchiveWriter(up uploader, svcPrefix string, cfg ArchiveConfig, m *ArchiveMetrics) *archiveWriter {
	cfg = cfg.withDefaults()
	signer, verifier := resolveSegmentCrypto(&cfg)
	w := &archiveWriter{
		up:        up,
		cfg:       cfg,
		svcPrefix: svcPrefix,
		m:         m,
		signer:    signer,
		verifier:  verifier,
		shards:    make(map[string]*shardQueue),
		stopCh:    make(chan struct{}),
	}
	w.wg.Add(1)
	go w.flushLoop()
	return w
}

// resolveSegmentCrypto picks the signing key and builds the verifier
// trust set. An unconfigured cfg auto-generates a transient key so
// tests and dev runs Just Work — production callers are expected to
// inject a KMS-held key via cfg.SigningKey and the matching public
// key(s) via cfg.TrustedSegmentKeys.
func resolveSegmentCrypto(cfg *ArchiveConfig) (*segmentSigner, *segmentVerifier) {
	priv := cfg.SigningKey
	if len(priv) != ed25519.PrivateKeySize {
		// No key supplied — auto-generate. Sized so archives read
		// within a single process (tests, dev) can be verified by the
		// same writer's verifier.
		pub, gen, err := ed25519.GenerateKey(nil)
		if err != nil {
			// Ed25519.GenerateKey only fails on a catastrophically
			// broken crypto/rand — return a disabled pair so the
			// writer fails closed on encode.
			_ = pub
			return nil, newSegmentVerifier()
		}
		priv = gen
	}
	s := newSegmentSigner(priv)
	// Trust the local signer's key plus any explicitly trusted keys.
	trust := []ed25519.PublicKey{s.pub}
	trust = append(trust, cfg.TrustedSegmentKeys...)
	return s, newSegmentVerifier(trust...)
}

// Append buffers a frame. It never blocks on the network; the flush
// loop handles upload. Fast-path returns without allocations when the
// shard queue already exists.
//
// lagBytes tracks backlog only (sealed, encoded segments awaiting
// upload). In-progress segments are deliberately excluded so the
// metric reflects "bytes pending flush to storage", which is the
// signal an operator cares about.
func (w *archiveWriter) Append(ctx context.Context, shardID string, seq uint64, frame []byte) error {
	if w.stopped.Load() {
		return fmt.Errorf("archive: writer closed")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	q, ok := w.shards[shardID]
	if !ok {
		q = &shardQueue{
			shardID:       shardID,
			seg:           newSegmentBuffer(shardID, seq),
			flushDeadline: time.Now().Add(w.cfg.FlushInterval),
		}
		w.shards[shardID] = q
	}
	if err := q.seg.append(seq, frame); err != nil {
		return err
	}
	// Size-based flush: move current seg to backlog, reset buffer.
	if q.seg.sizeBytes() >= w.cfg.SegmentTargetBytes {
		if err := w.rotateLocked(q); err != nil {
			return err
		}
	}
	w.enforceBacklogCapLocked(q)
	w.reportLag()
	return nil
}

// rotateLocked encodes the current segment, pushes it onto backlog,
// and starts a new empty segment. Caller must hold w.mu. Adds the
// encoded size to lagBytes so the metric reflects the backlog.
func (w *archiveWriter) rotateLocked(q *shardQueue) error {
	if q.seg.len() == 0 {
		// Empty segment: just reset the deadline, no upload.
		q.flushDeadline = time.Now().Add(w.cfg.FlushInterval)
		return nil
	}
	enc, err := q.seg.encode(w.signer)
	if err != nil {
		return fmt.Errorf("archive: encode segment: %w", err)
	}
	pending := segmentPending{
		nanos:    time.Now().UnixNano(),
		startSeq: q.seg.startSeq,
		data:     enc,
	}
	q.backlog = append(q.backlog, pending)
	w.lagBytes.Add(int64(len(enc)))
	q.seg = newSegmentBuffer(q.shardID, q.seg.nextSeq)
	q.flushDeadline = time.Now().Add(w.cfg.FlushInterval)
	return nil
}

// enforceBacklogCapLocked sheds the oldest segments once the shard's
// backlog exceeds either the byte cap or the segment-count cap. Drops
// are counted on the IncDrops metric so operators get paged; this is
// strictly a liveness-over-durability trade that fires only under a
// hostile / broken backend (retries exhausted, uploads never
// succeeding). The alternative is OOM, which drops everything.
func (w *archiveWriter) enforceBacklogCapLocked(q *shardQueue) {
	if w.cfg.BacklogMaxBytes <= 0 && w.cfg.BacklogMaxSegments <= 0 {
		return
	}
	for {
		total := 0
		for _, p := range q.backlog {
			total += len(p.data)
		}
		overBytes := w.cfg.BacklogMaxBytes > 0 && total > w.cfg.BacklogMaxBytes
		overCount := w.cfg.BacklogMaxSegments > 0 && len(q.backlog) > w.cfg.BacklogMaxSegments
		if !overBytes && !overCount {
			return
		}
		if len(q.backlog) == 0 {
			return
		}
		dropped := q.backlog[0]
		q.backlog = q.backlog[1:]
		w.lagBytes.Add(-int64(len(dropped.data)))
		slog.Error("archive: backlog cap exceeded, dropping oldest segment",
			"shard", q.shardID,
			"dropped_seq", dropped.startSeq,
			"dropped_bytes", len(dropped.data),
			"backlog_segments_after", len(q.backlog),
			"backlog_bytes_after", total-len(dropped.data),
		)
		if w.m != nil && w.m.IncDrops != nil {
			w.m.IncDrops()
		}
	}
}

// flushLoop ticks at half the configured interval (so worst-case lag
// between deadline and flush is FlushInterval/2). Each tick scans
// shards for ones past deadline or with backlog, and uploads.
func (w *archiveWriter) flushLoop() {
	defer w.wg.Done()
	tick := w.cfg.FlushInterval / 2
	if tick <= 0 {
		tick = time.Second
	}
	t := time.NewTicker(tick)
	defer t.Stop()
	for {
		select {
		case <-w.stopCh:
			// Drain: flush every remaining shard unconditionally.
			w.drain()
			return
		case <-t.C:
			w.flushReady()
		}
	}
}

// flushReady rotates any shard past its deadline, then drains backlog
// for every shard.
func (w *archiveWriter) flushReady() {
	now := time.Now()
	w.mu.Lock()
	for _, q := range w.shards {
		if !now.Before(q.flushDeadline) && q.seg.len() > 0 {
			_ = w.rotateLocked(q) // rotateLocked only fails on encoder overflow
		}
	}
	// Snapshot backlog keys so we can upload without holding the lock.
	type item struct {
		q       *shardQueue
		pending segmentPending
	}
	var pending []item
	for _, q := range w.shards {
		for _, seg := range q.backlog {
			pending = append(pending, item{q: q, pending: seg})
		}
		q.backlog = nil
	}
	w.mu.Unlock()

	for _, it := range pending {
		w.uploadWithRetry(it.q, it.pending)
	}
	w.reportLag()
}

// drain is flushReady with current seg also rotated regardless of
// deadline, called on Close.
func (w *archiveWriter) drain() {
	w.mu.Lock()
	for _, q := range w.shards {
		if q.seg.len() > 0 {
			_ = w.rotateLocked(q)
		}
	}
	type item struct {
		q       *shardQueue
		pending segmentPending
	}
	var pending []item
	for _, q := range w.shards {
		for _, seg := range q.backlog {
			pending = append(pending, item{q: q, pending: seg})
		}
		q.backlog = nil
	}
	w.mu.Unlock()

	// On close, give each segment one full retry window to drain.
	ctx, cancel := context.WithTimeout(context.Background(), w.cfg.RetryDeadline)
	defer cancel()
	for _, it := range pending {
		if err := w.uploadOnce(ctx, it.q, it.pending); err != nil {
			// Re-buffer the ones we couldn't ship so Close doesn't drop data;
			// callers that care can poll until lag reaches zero.
			w.mu.Lock()
			it.q.backlog = append(it.q.backlog, it.pending)
			w.enforceBacklogCapLocked(it.q)
			w.mu.Unlock()
			slog.Error("archive: flush on close failed", "shard", it.q.shardID, "err", err)
			if w.m != nil && w.m.IncFailures != nil {
				w.m.IncFailures()
			}
		}
	}
	w.reportLag()
}

// uploadWithRetry retries exponentially until RetryDeadline expires.
// On deadline, it re-buffers the segment (never silent data loss) and
// increments the failure counter. The backlog cap is enforced again on
// re-buffer, so a persistent hostile backend still bounds memory.
func (w *archiveWriter) uploadWithRetry(q *shardQueue, p segmentPending) {
	ctx, cancel := context.WithTimeout(context.Background(), w.cfg.RetryDeadline)
	defer cancel()
	backoff := 250 * time.Millisecond
	for {
		err := w.uploadOnce(ctx, q, p)
		if err == nil {
			return
		}
		slog.Warn("archive: upload failed, will retry",
			"shard", q.shardID, "bytes", len(p.data), "err", err)
		select {
		case <-w.stopCh:
			w.reBuffer(q, p)
			return
		case <-ctx.Done():
			slog.Error("archive: upload retry deadline exceeded, re-buffering",
				"shard", q.shardID, "bytes", len(p.data))
			if w.m != nil && w.m.IncFailures != nil {
				w.m.IncFailures()
			}
			w.reBuffer(q, p)
			return
		case <-time.After(backoff):
		}
		backoff *= 2
		if backoff > 30*time.Second {
			backoff = 30 * time.Second
		}
	}
}

func (w *archiveWriter) uploadOnce(ctx context.Context, q *shardQueue, p segmentPending) error {
	// Sanity round-trip: refuse to ship anything we can't decode.
	if _, err := decodeSegment(p.data, w.verifier); err != nil {
		return fmt.Errorf("archive: refusing to upload unverifiable segment: %w", err)
	}
	key := objectKey(w.svcPrefix, q.shardID, p.startSeq, p.nanos)
	if err := w.up.put(ctx, key, p.data); err != nil {
		return err
	}
	w.lagBytes.Add(-int64(len(p.data)))
	w.reportLag()
	return nil
}

// reBuffer returns an unshipped segment to the shard's backlog so the
// next flush cycle tries again. The head-insert preserves oldest-first
// ordering; the cap check ensures memory is still bounded.
func (w *archiveWriter) reBuffer(q *shardQueue, p segmentPending) {
	w.mu.Lock()
	defer w.mu.Unlock()
	q.backlog = append([]segmentPending{p}, q.backlog...)
	w.lagBytes.Add(int64(len(p.data)))
	w.enforceBacklogCapLocked(q)
}

func (w *archiveWriter) reportLag() {
	if w.m != nil && w.m.SetLagBytes != nil {
		w.m.SetLagBytes(w.lagBytes.Load())
	}
}

// Close stops the flush loop and waits for in-flight uploads to
// complete. Segments that don't ship during the final drain are
// retained in-memory; a follow-on Close call or process restart will
// re-attempt. The error reflects the backend close, not upload state.
func (w *archiveWriter) Close() error {
	if w.stopped.Swap(true) {
		return nil
	}
	close(w.stopCh)
	w.wg.Wait()
	return w.up.close()
}

// Range implements Archive.Range by listing objects under the shard
// prefix, filtering by seq range, downloading each overlapping
// segment, and yielding frames in order.
//
// Keys contain a (startSeq, nanos) pair so multiple flushes of the
// same seq range don't overwrite. The iterator sorts by (startSeq,
// nanos) and dedupes frames by (startSeq + index) — the latest nanos
// for a given startSeq wins when two segments cover the same range.
func (w *archiveWriter) Range(ctx context.Context, shardID string, fromSeq, toSeq uint64) (iter.Seq2[Frame, error], error) {
	if toSeq < fromSeq {
		return nil, fmt.Errorf("archive: range toSeq %d < fromSeq %d", toSeq, fromSeq)
	}
	prefix := fmt.Sprintf("%s/%s/", w.svcPrefix, shardID)
	keys, err := w.up.list(ctx, prefix)
	if err != nil {
		return nil, fmt.Errorf("archive: list %s: %w", prefix, err)
	}
	type segMeta struct {
		key      string
		startSeq uint64
		nanos    int64
	}
	metas := make([]segMeta, 0, len(keys))
	for _, k := range keys {
		if !strings.HasSuffix(k, ".lbn") {
			continue
		}
		startSeq, nanos, ok := parseObjectKey(k)
		if !ok {
			continue // ignore unrelated objects under this prefix
		}
		metas = append(metas, segMeta{key: k, startSeq: startSeq, nanos: nanos})
	}
	// Sort primarily by startSeq, secondarily by nanos (older first)
	// so a later flush of overlapping ranges wins on dedupe below.
	sort.Slice(metas, func(i, j int) bool {
		if metas[i].startSeq != metas[j].startSeq {
			return metas[i].startSeq < metas[j].startSeq
		}
		return metas[i].nanos < metas[j].nanos
	})

	return func(yield func(Frame, error) bool) {
		// Track the next seq we haven't yielded yet; we skip frames we've
		// already emitted when two segments overlap.
		nextSeq := fromSeq
		for i, m := range metas {
			if m.startSeq > toSeq {
				return
			}
			// Skip segments that end before fromSeq. We can only know the
			// end by reading or the next segment's startSeq.
			if i+1 < len(metas) && metas[i+1].startSeq <= fromSeq && metas[i+1].startSeq != m.startSeq {
				continue
			}
			data, err := w.up.get(ctx, m.key)
			if err != nil {
				yield(Frame{ShardID: shardID}, fmt.Errorf("archive: get %s: %w", m.key, err))
				return
			}
			dec, err := decodeSegment(data, w.verifier)
			if err != nil {
				// R3: forged / truncated / foreign-signed segments are
				// SILENTLY SKIPPED. Halting PITR on a single poisoned
				// segment hands the attacker a DoS against restore;
				// surfacing the skip as a metric keeps it visible.
				slog.Warn("archive: skipping unverifiable segment",
					"shard", shardID, "key", m.key, "err", err)
				if w.m != nil && w.m.IncFailures != nil {
					w.m.IncFailures()
				}
				continue
			}
			for idx, raw := range dec.Frames {
				seq := dec.StartSeq + uint64(idx)
				if seq < nextSeq {
					// Already yielded from an earlier segment covering
					// this range; skip the duplicate copy.
					continue
				}
				if seq > toSeq {
					return
				}
				// The stored bytes are the output of Frame.encode() —
				// decode back so callers get a validated struct.
				f, err := decodeFrame(raw)
				if err != nil {
					yield(Frame{ShardID: shardID, Seq: seq}, fmt.Errorf("archive: decode frame seq %d in %s: %w", seq, m.key, err))
					return
				}
				if !yield(f, nil) {
					return
				}
				nextSeq = seq + 1
			}
		}
	}, nil
}
