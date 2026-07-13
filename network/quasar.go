package network

import (
	"context"
	"fmt"
)

// quasarEngine is Base's in-process, per-shard finalization pipe for the
// Quasar replication plane. One quasarEngine per shard.
//
// Base's replication is NOT BFT consensus. Cross-pod frame delivery is the
// ZAP transport (transport_zap.go); the authoritative ordering, dedup and
// quorum-routing live in shard.go (applyLoop), router.go and membership.go.
// This engine's only job is to accept locally-submitted and peer-ingested
// frames and stream them, in submission order, to the shard's apply loop.
//
// It deliberately does not depend on github.com/luxfi/consensus. Base never
// wired a validator set, signer, or security profile into the upstream Quasar
// engine, so that engine only ever behaved as this same buffered pass-through
// (emitting a discarded SHA-256 placeholder certificate) while dragging ~220
// transitive packages — the entire PQ crypto stack — into every binary built
// on Base. If Base ever needs real cross-pod BFT finality here, that is a
// deliberate, separately-scoped feature that reintroduces the consensus layer
// with a configured validator set — not an implicit dependency of the runtime.
type quasarEngine struct {
	shardID string

	incoming  chan Frame
	finalized chan Frame
	stop      context.CancelFunc
}

// newQuasarEngine builds an engine scoped to one shard.
func newQuasarEngine(ctx context.Context, shardID string) (*quasarEngine, error) {
	engCtx, cancel := context.WithCancel(ctx)
	q := &quasarEngine{
		shardID:   shardID,
		incoming:  make(chan Frame, 1024),
		finalized: make(chan Frame, 1024),
		stop:      cancel,
	}
	go q.run(engCtx)
	return q, nil
}

// Submit accepts a frame into the pipe. Non-blocking: returns an error when
// the buffer is full so the caller surfaces backpressure rather than blocking.
func (q *quasarEngine) Submit(f Frame) error {
	select {
	case q.incoming <- f:
		return nil
	default:
		return fmt.Errorf("quasar: shard %s submit buffer full", q.shardID)
	}
}

// Finalized returns the stream of frames ready to apply.
func (q *quasarEngine) Finalized() <-chan Frame { return q.finalized }

// Stop halts the engine and closes the finalized channel.
func (q *quasarEngine) Stop() error {
	q.stop()
	return nil
}

// run drains submitted frames to the finalized stream in submission order.
// A single goroutine preserves order; sending applies backpressure to Submit
// (via the bounded incoming buffer) instead of dropping frames. Exits and
// closes finalized when the engine context is cancelled by Stop.
func (q *quasarEngine) run(ctx context.Context) {
	defer close(q.finalized)
	for {
		select {
		case <-ctx.Done():
			return
		case f := <-q.incoming:
			select {
			case q.finalized <- f:
			case <-ctx.Done():
				return
			}
		}
	}
}
