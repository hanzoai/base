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
	localSeq uint64

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
		ID:        id,
		Members:   members,
		Threshold: threshold,
		engine:    eng,
		metric:    m,
		applied:   make(map[ApplyKey]struct{}),
		ctx:       ctx,
		cancel:    cancel,
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
func (s *Shard) submitLocal(f Frame) error {
	if err := s.engine.Submit(f); err != nil {
		return fmt.Errorf("shard %s submit: %w", s.ID, err)
	}
	s.metric.FramesSubmitted.Inc()
	return nil
}

// ingestRemote is called when the transport hands us a frame from a peer.
// Verify, then submit so Quasar folds it into the DAG.
func (s *Shard) ingestRemote(f Frame) error {
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
func (s *Shard) applyLoop(apply ApplyFunc) {
	for {
		select {
		case <-s.ctx.Done():
			return
		case f, ok := <-s.engine.Finalized():
			if !ok {
				return
			}
			key := f.ApplyKey()
			s.mu.Lock()
			_, dup := s.applied[key]
			if !dup {
				s.applied[key] = struct{}{}
				if f.Seq > s.localSeq {
					s.localSeq = f.Seq
				}
			}
			s.mu.Unlock()
			if dup {
				s.metric.FramesDuplicate.Inc()
				continue
			}
			if err := apply(f); err != nil {
				s.metric.ApplyErrors.Inc()
				continue
			}
			s.metric.FramesFinalized.Inc()
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
