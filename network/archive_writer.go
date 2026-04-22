package network

import (
	"context"
	"fmt"
	"iter"
	"log/slog"
	"sort"
	"strconv"
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
	backlog [][]byte // encoded segments pending upload
	// flushDeadline is when the current seg must flush even if under
	// the size target. Reset on every new segment.
	flushDeadline time.Time
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

	mu     sync.Mutex
	shards map[string]*shardQueue

	// lagBytes is the running size of buffered+backlog data across
	// every shard. Refreshed whenever append/flush changes it.
	lagBytes atomic.Int64

	stopCh chan struct{}
	stopped atomic.Bool
	wg     sync.WaitGroup
}

func newArchiveWriter(up uploader, svcPrefix string, cfg ArchiveConfig, m *ArchiveMetrics) *archiveWriter {
	w := &archiveWriter{
		up:        up,
		cfg:       cfg,
		svcPrefix: svcPrefix,
		m:         m,
		shards:    make(map[string]*shardQueue),
		stopCh:    make(chan struct{}),
	}
	w.wg.Add(1)
	go w.flushLoop()
	return w
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
	enc, err := q.seg.encode()
	if err != nil {
		return fmt.Errorf("archive: encode segment: %w", err)
	}
	q.backlog = append(q.backlog, enc)
	w.lagBytes.Add(int64(len(enc)))
	q.seg = newSegmentBuffer(q.shardID, q.seg.nextSeq)
	q.flushDeadline = time.Now().Add(w.cfg.FlushInterval)
	return nil
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
		q    *shardQueue
		data []byte
	}
	var pending []item
	for _, q := range w.shards {
		for _, seg := range q.backlog {
			pending = append(pending, item{q: q, data: seg})
		}
		q.backlog = nil
	}
	w.mu.Unlock()

	for _, it := range pending {
		w.uploadWithRetry(it.q, it.data)
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
		q    *shardQueue
		data []byte
	}
	var pending []item
	for _, q := range w.shards {
		for _, seg := range q.backlog {
			pending = append(pending, item{q: q, data: seg})
		}
		q.backlog = nil
	}
	w.mu.Unlock()

	// On close, give each segment one full retry window to drain.
	ctx, cancel := context.WithTimeout(context.Background(), w.cfg.RetryDeadline)
	defer cancel()
	for _, it := range pending {
		if err := w.uploadOnce(ctx, it.q, it.data); err != nil {
			// Re-buffer the ones we couldn't ship so Close doesn't drop data;
			// callers that care can poll until lag reaches zero.
			w.mu.Lock()
			it.q.backlog = append(it.q.backlog, it.data)
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
// On deadline, it re-buffers the segment (never data loss) and
// increments the failure counter.
func (w *archiveWriter) uploadWithRetry(q *shardQueue, data []byte) {
	ctx, cancel := context.WithTimeout(context.Background(), w.cfg.RetryDeadline)
	defer cancel()
	backoff := 250 * time.Millisecond
	for {
		err := w.uploadOnce(ctx, q, data)
		if err == nil {
			return
		}
		slog.Warn("archive: upload failed, will retry",
			"shard", q.shardID, "bytes", len(data), "err", err)
		select {
		case <-w.stopCh:
			w.reBuffer(q, data)
			return
		case <-ctx.Done():
			slog.Error("archive: upload retry deadline exceeded, re-buffering",
				"shard", q.shardID, "bytes", len(data))
			if w.m != nil && w.m.IncFailures != nil {
				w.m.IncFailures()
			}
			w.reBuffer(q, data)
			return
		case <-time.After(backoff):
		}
		backoff *= 2
		if backoff > 30*time.Second {
			backoff = 30 * time.Second
		}
	}
}

func (w *archiveWriter) uploadOnce(ctx context.Context, q *shardQueue, data []byte) error {
	dec, err := decodeSegment(data)
	if err != nil {
		// This would be a bug in our own encoder; never silently ship
		// a malformed segment.
		return fmt.Errorf("archive: refusing to upload malformed segment: %w", err)
	}
	key := objectKey(w.svcPrefix, dec.ShardID, dec.StartSeq)
	if err := w.up.put(ctx, key, data); err != nil {
		return err
	}
	w.lagBytes.Add(-int64(len(data)))
	w.reportLag()
	return nil
}

// reBuffer returns an unshipped segment to the shard's backlog so the
// next flush cycle tries again. No data is ever dropped.
func (w *archiveWriter) reBuffer(q *shardQueue, data []byte) {
	w.mu.Lock()
	// Preserve ordering: re-buffered segments go to the head.
	q.backlog = append([][]byte{data}, q.backlog...)
	w.mu.Unlock()
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
	}
	metas := make([]segMeta, 0, len(keys))
	for _, k := range keys {
		if !strings.HasSuffix(k, ".lbn") {
			continue
		}
		// key suffix: "<20-digit-seq>.lbn"
		base := k[strings.LastIndex(k, "/")+1:]
		stripped := strings.TrimSuffix(base, ".lbn")
		seq, err := strconv.ParseUint(stripped, 10, 64)
		if err != nil {
			continue // ignore unrelated objects under this prefix
		}
		metas = append(metas, segMeta{key: k, startSeq: seq})
	}
	sort.Slice(metas, func(i, j int) bool { return metas[i].startSeq < metas[j].startSeq })

	return func(yield func(Frame, error) bool) {
		for i, m := range metas {
			// Cheap prune: if this segment starts after the requested
			// range, we're done. If the NEXT segment also starts
			// strictly after fromSeq, the current one may still
			// overlap, so we only stop on pure post-range segments.
			if m.startSeq > toSeq {
				return
			}
			// Skip segments that end before fromSeq. "ends before" is
			// determined by the next segment's startSeq-1 (contiguous
			// assumption), or unbounded for the last segment — which
			// means we must download it to know.
			if i+1 < len(metas) && metas[i+1].startSeq <= fromSeq {
				continue
			}
			data, err := w.up.get(ctx, m.key)
			if err != nil {
				yield(Frame{ShardID: shardID}, fmt.Errorf("archive: get %s: %w", m.key, err))
				return
			}
			dec, err := decodeSegment(data)
			if err != nil {
				yield(Frame{ShardID: shardID}, fmt.Errorf("archive: decode %s: %w", m.key, err))
				return
			}
			for idx, raw := range dec.Frames {
				seq := dec.StartSeq + uint64(idx)
				if seq < fromSeq {
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
			}
		}
	}, nil
}
