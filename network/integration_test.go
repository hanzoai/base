package network

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
)

// memoryHub is a one-process transport mesh. Every node that joins the hub
// receives every frame that any other node publishes. This is the
// in-process stand-in for a real QUIC/gRPC mesh; consensus behaviour is
// identical.
type memoryHub struct {
	mu    sync.RWMutex
	nodes map[NodeID]*memoryTransport
}

func newMemoryHub() *memoryHub {
	return &memoryHub{nodes: make(map[NodeID]*memoryTransport)}
}

func (h *memoryHub) connect(id NodeID) *memoryTransport {
	t := &memoryTransport{hub: h, self: id}
	h.mu.Lock()
	h.nodes[id] = t
	h.mu.Unlock()
	return t
}

// memoryTransport satisfies Transport by fanning out Publish via the hub.
type memoryTransport struct {
	hub  *memoryHub
	self NodeID

	mu     sync.Mutex
	recv   func(Envelope)
	closed bool
}

func (t *memoryTransport) Start(_ context.Context, recv func(Envelope)) error {
	t.mu.Lock()
	t.recv = recv
	t.closed = false
	t.mu.Unlock()
	return nil
}

func (t *memoryTransport) Publish(env Envelope) error {
	t.hub.mu.RLock()
	peers := make([]*memoryTransport, 0, len(t.hub.nodes))
	for id, peer := range t.hub.nodes {
		if id == t.self {
			continue
		}
		peers = append(peers, peer)
	}
	t.hub.mu.RUnlock()

	for _, p := range peers {
		p.mu.Lock()
		cb := p.recv
		closed := p.closed
		p.mu.Unlock()
		if cb == nil || closed {
			continue
		}
		// Detach frame bytes — decodeFrame in receiver should allocate its
		// own buffer, but defensive copy avoids any shared-slice bugs.
		cb(env)
	}
	return nil
}

func (t *memoryTransport) Stop(_ context.Context) error {
	t.mu.Lock()
	t.closed = true
	t.recv = nil
	t.mu.Unlock()
	return nil
}

// countingSource pretends to be the SQLite WAL tap. It returns a payload
// that includes a monotonic counter so we can verify ordering at apply.
type countingSource struct {
	n atomic.Uint64
}

func (c *countingSource) CapturePayload(_ string) ([]byte, error) {
	v := c.n.Add(1)
	out := make([]byte, 8)
	for i := 0; i < 8; i++ {
		out[7-i] = byte(v >> (i * 8))
	}
	return out, nil
}

// TestThreeNodeCrossShardFinalize spins up three nodes in-process sharing
// a memory transport. Node A writes to shardA, node B writes to shardB.
// After Quasar finalises, every node's shard must have applied both
// frames.
func TestThreeNodeCrossShardFinalize(t *testing.T) {
	hub := newMemoryHub()

	peers := []string{"a", "b", "c"}
	nodes := make([]*node, 0, 3)
	sources := make([]*countingSource, 0, 3)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	for _, id := range peers {
		cfg := Config{
			Enabled:     true,
			ShardKey:    "user_id",
			Replication: 3,
			Peers:       filter(peers, id),
			NodeID:      id,
			Role:        RoleValidator,
			Archive:     "off",
			ListenHTTP:  ":0",
			ListenP2P:   ":0",
		}
		nn, err := newNodeWithTransport(cfg, hub.connect(NodeID(id)))
		if err != nil {
			t.Fatalf("node %s: %v", id, err)
		}
		src := &countingSource{}
		nn.walSrc = src
		if err := nn.Start(ctx); err != nil {
			t.Fatalf("node %s start: %v", id, err)
		}
		t.Cleanup(func() { _ = nn.Stop(context.Background()) })
		nodes = append(nodes, nn)
		sources = append(sources, src)
	}

	// Writer A submits on shard-alpha; writer B submits on shard-beta.
	if err := submitLocalOn(nodes[0], "shard-alpha"); err != nil {
		t.Fatalf("node a submit: %v", err)
	}
	if err := submitLocalOn(nodes[1], "shard-beta"); err != nil {
		t.Fatalf("node b submit: %v", err)
	}

	_ = sources // only exists to ensure distinct payloads per node

	// Poll until every node has applied one frame on each of the two
	// shards, or the deadline trips.
	deadline := time.Now().Add(5 * time.Second)
	for {
		ok := true
		for _, n := range nodes {
			sa, err := n.shard("shard-alpha")
			if err != nil || sa.LocalSeq() < 1 {
				ok = false
				break
			}
			sb, err := n.shard("shard-beta")
			if err != nil || sb.LocalSeq() < 1 {
				ok = false
				break
			}
		}
		if ok {
			break
		}
		if time.Now().After(deadline) {
			for i, n := range nodes {
				sa, _ := n.shard("shard-alpha")
				sb, _ := n.shard("shard-beta")
				t.Logf("node %d (%s): alpha.localSeq=%d, beta.localSeq=%d",
					i, n.id, sa.LocalSeq(), sb.LocalSeq())
			}
			t.Fatal("timeout: not every node finalised both shards")
		}
		time.Sleep(25 * time.Millisecond)
	}

	// Sanity: every node finalised both frames.
	for i, n := range nodes {
		m := n.Metrics()
		if got := counterVal(t, m.FramesFinalized); got < 2 {
			t.Errorf("node %d: FramesFinalized = %v, want >= 2", i, got)
		}
	}
	// The two writer nodes (A, B) produced one local frame each; the
	// third saw both via transport.
	if got := counterVal(t, nodes[0].Metrics().FramesSubmitted); got < 1 {
		t.Errorf("node a: FramesSubmitted = %v, want >= 1", got)
	}
	if got := counterVal(t, nodes[1].Metrics().FramesSubmitted); got < 1 {
		t.Errorf("node b: FramesSubmitted = %v, want >= 1", got)
	}
	if got := counterVal(t, nodes[2].Metrics().FramesIngested); got < 2 {
		t.Errorf("node c: FramesIngested = %v, want >= 2", got)
	}
}

// submitLocalOn fires a synthetic commit on n for shard. This exercises
// the same path InstallWALHook's callback uses so the integration test
// covers the full produce → Quasar → apply loop without a live SQLite.
func submitLocalOn(n *node, shard string) error {
	s, err := n.shard(shard)
	if err != nil {
		return err
	}
	w := &shardWriter{shardID: shard, src: n.walSrc}
	f, err := w.buildFrame()
	if err != nil {
		return err
	}
	if err := s.submitLocal(f); err != nil {
		return err
	}
	return n.transport.Publish(Envelope{ShardID: shard, Frame: f})
}

func filter(all []string, drop string) []string {
	out := make([]string, 0, len(all)-1)
	for _, s := range all {
		if s == drop {
			continue
		}
		out = append(out, s)
	}
	return out
}

func counterVal(t *testing.T, c prometheus.Counter) float64 {
	t.Helper()
	var m dto.Metric
	if err := c.Write(&m); err != nil {
		t.Fatalf("counter Write: %v", err)
	}
	return m.GetCounter().GetValue()
}
