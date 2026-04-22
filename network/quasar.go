package network

import (
	"context"
	"crypto/sha256"
	"fmt"
	"time"

	"github.com/luxfi/consensus/protocol/quasar"
)

// quasarEngine is the adapter over github.com/luxfi/consensus/protocol/quasar.
// It narrows the upstream Engine to exactly the surface this package needs:
//
//	Submit(shardID, frame) — push a local commit into the DAG.
//	Finalized() chan Frame — stream of frames that quorum has acked.
//	Members() []NodeID     — current voting set for the shard.
//	Stop()                 — graceful shutdown.
//
// One quasarEngine per shard; one Quasar DAG per shard, per the design doc.
type quasarEngine struct {
	shardID   string
	chainID   [32]byte
	members   []NodeID
	threshold int

	inner     quasar.Engine
	finalized chan Frame
	stop      context.CancelFunc
}

// newQuasarEngine builds a Quasar engine scoped to one shard. threshold is
// the number of acks needed (k = ⌊N/2⌋+1 in quorum mode, or just N when
// N ≤ 2). Single-node (N=1) uses NewTestEngine — Quasar requires threshold
// ≥ 2 for production multi-node and we honour that cleanly.
func newQuasarEngine(ctx context.Context, shardID string, members []NodeID, threshold int) (*quasarEngine, error) {
	cfg := quasar.Config{QThreshold: threshold, QuasarTimeout: 30}

	var (
		inner quasar.Engine
		err   error
	)
	if threshold < 2 {
		inner, err = quasar.NewTestEngine(cfg)
	} else {
		inner, err = quasar.NewEngine(cfg)
	}
	if err != nil {
		return nil, fmt.Errorf("quasar: %w", err)
	}

	engCtx, cancel := context.WithCancel(ctx)
	if err := inner.Start(engCtx); err != nil {
		cancel()
		return nil, fmt.Errorf("quasar start: %w", err)
	}

	q := &quasarEngine{
		shardID:   shardID,
		chainID:   sha256.Sum256([]byte(shardID)),
		members:   append([]NodeID(nil), members...),
		threshold: threshold,
		inner:     inner,
		finalized: make(chan Frame, 1024),
		stop:      cancel,
	}

	go q.forwardFinalized(engCtx)
	return q, nil
}

// Submit wraps a Frame in a quasar.Block and hands it to the engine. The
// Block's ID is the frame checksum so the DAG keys on content.
func (q *quasarEngine) Submit(f Frame) error {
	blk := &quasar.Block{
		ID:        f.blockID(),
		ChainID:   q.chainID,
		ChainName: q.shardID,
		Height:    f.Seq,
		Timestamp: time.Unix(0, f.Timestamp),
		Data:      f.encode(),
	}
	return q.inner.Submit(blk)
}

// Finalized returns the stream of frames finalised by quorum.
func (q *quasarEngine) Finalized() <-chan Frame { return q.finalized }

// Members returns the voting set for the shard.
func (q *quasarEngine) Members() []NodeID { return q.members }

// Stop halts the engine and closes the finalized channel.
func (q *quasarEngine) Stop() error {
	q.stop()
	return q.inner.Stop()
}

func (q *quasarEngine) forwardFinalized(ctx context.Context) {
	src := q.inner.Finalized()
	for {
		select {
		case <-ctx.Done():
			close(q.finalized)
			return
		case blk, ok := <-src:
			if !ok {
				close(q.finalized)
				return
			}
			if blk == nil {
				continue
			}
			f, err := decodeFrame(blk.Data)
			if err != nil {
				continue
			}
			select {
			case q.finalized <- f:
			case <-ctx.Done():
				close(q.finalized)
				return
			}
		}
	}
}
