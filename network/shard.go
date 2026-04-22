package network

import (
	"context"
	"fmt"
	"sync"
)

// NodeID is a stable member identity — in k8s the pod DNS name, in compose
// the service DNS name, in tests a synthetic string.
type NodeID string

// Shard owns the per-shard Quasar engine, the apply loop, and the dedup
// cache that makes apply idempotent across restarts.
//
// Lifecycle: created lazily on first reference by shard(id), closed by the
// node on Stop(). One Shard per shardID per node.
type Shard struct {
	ID        string
	Members   []NodeID
	Threshold int

	engine *quasarEngine
	metric *Metrics

	mu      sync.Mutex
	applied map[ApplyKey]struct{}

	// localSeq is the last finalised sequence we have applied locally; the
	// gateway reads this to serve read-your-writes via the txseq cookie.
	// R2: advances STRICTLY by +1 per apply — never pinned to an
	// attacker-chosen height.
	localSeq uint64

	// pending holds finalised frames whose Seq is > localSeq+1 (quasar
	// may reorder across the DAG). Frames land here and drain as their
	// predecessors arrive. A bounded cap (pendingCap) drops the oldest
	// out-of-order frames to keep a hostile peer from OOMing us with a
	// far-future Seq cloud. A dropped frame re-enters via quasar's
	// finalise stream on retry, so liveness is preserved.
	pending    map[uint64]Frame
	pendingCap int

	ctx    context.Context
	cancel context.CancelFunc
}

// newShard starts the Quasar engine and the apply goroutine. apply is the
// node-level callback invoked for every finalised frame.
func newShard(parent context.Context, id string, members []NodeID, replication int, m *Metrics, apply ApplyFunc) (*Shard, error) {
	ctx, cancel := context.WithCancel(parent)

	threshold := quorum(replication)
	// Quasar requires threshold ≥ 2 except in test-engine mode — we pick
	// that path when replication < 2 so single-node works.
	eng, err := newQuasarEngine(ctx, id, members, threshold)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("shard %s: %w", id, err)
	}

	s := &Shard{
		ID:         id,
		Members:    members,
		Threshold:  threshold,
		engine:     eng,
		metric:     m,
		applied:    make(map[ApplyKey]struct{}),
		pending:    make(map[uint64]Frame),
		pendingCap: 1024, // bounded per-shard buffer for out-of-order frames
		ctx:        ctx,
		cancel:     cancel,
	}
	go s.applyLoop(apply)
	return s, nil
}

// quorum computes k = ⌊N/2⌋+1 for N≥3, N-1 for N=2, 1 for N≤1.
func quorum(n int) int {
	switch {
	case n <= 1:
		return 1
	case n == 2:
		return 2
	default:
		return n/2 + 1
	}
}

// submitLocal pushes a locally-generated frame into the engine.
//
// R1 defence-in-depth: reject any frame whose declared ShardID does
// not match this shard's ID. The transport-layer check in onPeerFrame
// already covers this path; double-checking here blocks an internal
// caller that mixed up shard identities.
func (s *Shard) submitLocal(f Frame) error {
	if f.ShardID != s.ID {
		s.metric.FramesRejectedShardMismatch.Inc()
		return fmt.Errorf("shard %s: frame shardID %q mismatch", s.ID, f.ShardID)
	}
	if err := s.engine.Submit(f); err != nil {
		return fmt.Errorf("shard %s submit: %w", s.ID, err)
	}
	s.metric.FramesSubmitted.Inc()
	return nil
}

// ingestRemote is called when the transport hands us a frame from a peer.
// Verify, then submit so Quasar folds it into the DAG.
//
// R1 fix: inner ShardID MUST match the shard we're being submitted to.
// R2 protection: the Seq value is still attacker-controllable at this
// point; that's OK — submit accepts it into the DAG, but the applyLoop
// below is what actually advances localSeq, and it enforces strict
// monotone continuity (Seq == localSeq+1).
func (s *Shard) ingestRemote(f Frame) error {
	if f.ShardID != s.ID {
		s.metric.FramesRejectedShardMismatch.Inc()
		return fmt.Errorf("shard %s: frame shardID %q mismatch", s.ID, f.ShardID)
	}
	if err := f.Valid(); err != nil {
		s.metric.FramesInvalid.Inc()
		return err
	}
	if err := s.engine.Submit(f); err != nil {
		return err
	}
	s.metric.FramesIngested.Inc()
	return nil
}

// applyLoop drains finalised frames and calls apply exactly once per
// (salt, cksm).
//
// R2 fix: localSeq advances STRICTLY by +1 per frame. Frames arriving
// out of order (quasar's DAG does not guarantee submission order on the
// finalise stream) are buffered in s.pending and applied when their
// predecessor lands. A frame whose Seq is <= localSeq is either a
// duplicate (dedupe by ApplyKey) or a stale replay (drop). A frame
// whose Seq is > localSeq+1 waits in pending until the gap closes.
//
// This blocks the attacker's "submit Seq=2^62 to pin localSeq" attack:
// the 2^62 frame stays in pending forever because Seqs 1..2^62-1 never
// arrive. The gateway sees localSeq==0 (or whatever honestly advanced),
// never jumps.
//
// pending is capped — once full, new far-future frames are dropped so
// one peer cannot OOM the shard with a forged-Seq cloud. Dropped
// frames are re-delivered by quasar's finalise stream on retry (the
// engine persists its DAG), so liveness is preserved.
//
// Idempotency is still keyed on (salt, cksm) — a duplicate finalised
// frame (e.g. peer re-submits after a reconnect) is counted as a dup
// and skipped.
func (s *Shard) applyLoop(apply ApplyFunc) {
	drain := func(f Frame) {
		if err := apply(f); err != nil {
			s.metric.ApplyErrors.Inc()
			return
		}
		s.metric.FramesFinalized.Inc()
	}
	for {
		select {
		case <-s.ctx.Done():
			return
		case f, ok := <-s.engine.Finalized():
			if !ok {
				return
			}
			// R1 defence-in-depth at apply boundary: the engine's
			// finalised stream can only ever carry frames we submitted
			// for this shard (engine is per-shard), but we re-verify
			// the inner shardID anyway in case Quasar's ordering
			// guarantees change. Wrong shard → drop, metric.
			if f.ShardID != s.ID {
				s.metric.FramesRejectedShardMismatch.Inc()
				continue
			}
			key := f.ApplyKey()

			s.mu.Lock()
			if _, dup := s.applied[key]; dup {
				s.mu.Unlock()
				s.metric.FramesDuplicate.Inc()
				continue
			}
			if f.Seq <= s.localSeq {
				// Stale replay or re-delivery below the watermark.
				// Either the salt/cksm already matched (dup above) or
				// an attacker replayed a different payload for an
				// already-applied Seq. Drop — localSeq is authoritative.
				s.mu.Unlock()
				s.metric.FramesRejectedSeqGap.Inc()
				continue
			}
			if f.Seq != s.localSeq+1 {
				// Out-of-order arrival. Buffer until predecessors land.
				// Cap enforcement: if pending is full, drop the frame
				// with the highest Seq (furthest from the gap) — keeps
				// the window tight and makes attacker-forged far-future
				// frames fall out first.
				if len(s.pending) >= s.pendingCap {
					var maxSeq uint64
					for seq := range s.pending {
						if seq > maxSeq {
							maxSeq = seq
						}
					}
					if f.Seq >= maxSeq {
						// Incoming is further than anything pending —
						// drop it (don't even buffer).
						s.mu.Unlock()
						s.metric.FramesRejectedSeqGap.Inc()
						continue
					}
					// Evict the furthest pending to make room.
					delete(s.pending, maxSeq)
					s.metric.FramesRejectedSeqGap.Inc()
				}
				s.pending[f.Seq] = f
				s.mu.Unlock()
				continue
			}

			// f.Seq == localSeq + 1: apply and drain any now-contiguous
			// pending frames.
			s.applied[key] = struct{}{}
			s.localSeq = f.Seq
			ready := []Frame{f}
			for {
				next, ok := s.pending[s.localSeq+1]
				if !ok {
					break
				}
				delete(s.pending, s.localSeq+1)
				nextKey := next.ApplyKey()
				if _, dup := s.applied[nextKey]; dup {
					s.metric.FramesDuplicate.Inc()
					continue
				}
				s.applied[nextKey] = struct{}{}
				s.localSeq = next.Seq
				ready = append(ready, next)
			}
			s.mu.Unlock()

			for _, rf := range ready {
				drain(rf)
			}
		}
	}
}

// close halts the shard engine. Called by node.Stop.
func (s *Shard) close() {
	s.cancel()
	_ = s.engine.Stop()
}

// LocalSeq returns the highest finalised seq this shard has applied
// locally. Gateways compare this against the client's txseq cookie.
func (s *Shard) LocalSeq() uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.localSeq
}

// ApplyFunc is called once per finalised frame per shard. Errors are
// counted on the metrics and do not retry — Quasar has already finalised
// the frame, so the caller owns recovery.
type ApplyFunc func(Frame) error
