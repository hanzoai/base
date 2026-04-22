package network

import (
	"context"
	"fmt"
	"sync"
)

// node is the live network member. It owns the per-shard Quasar engines,
// the transport to peers, and the shard cache. Start/Stop drive lifecycle;
// InstallWALHook is called once per sqlite connection.
type node struct {
	cfg     Config
	id      NodeID
	router  *router
	metrics *Metrics

	// transport is the cross-node plane. In-process tests inject a memory
	// transport; production will inject a QUIC/gRPC transport. Always set.
	transport Transport

	mu     sync.RWMutex
	shards map[string]*Shard
	walSrc walSource
	ctx    context.Context
	cancel context.CancelFunc
}

// newNode constructs the live node with the default transport. Tests that
// wire peers together in-process use NewWithTransport.
func newNode(cfg Config) (*node, error) {
	return newNodeWithTransport(cfg, nil)
}

func newNodeWithTransport(cfg Config, t Transport) (*node, error) {
	if err := cfg.validate(); err != nil {
		return nil, fmt.Errorf("network: %w", err)
	}
	if !cfg.Enabled {
		return nil, fmt.Errorf("network: disabled")
	}

	members := make([]NodeID, 0, len(cfg.Peers)+1)
	members = append(members, NodeID(cfg.NodeID))
	for _, p := range cfg.Peers {
		members = append(members, NodeID(p))
	}

	if t == nil {
		t = &nopTransport{}
	}

	return &node{
		cfg:       cfg,
		id:        NodeID(cfg.NodeID),
		router:    newRouter(members, cfg.Replication),
		metrics:   NewMetrics(),
		transport: t,
		shards:    make(map[string]*Shard),
		walSrc:    nopSource{},
	}, nil
}

// NewWithTransport constructs a Network with a caller-supplied transport.
// In-process tests inject a memory transport here; production callers use
// the default wired by New.
func NewWithTransport(cfg Config, t Transport) (Network, error) {
	if !cfg.Enabled {
		if err := cfg.validate(); err != nil {
			return nil, fmt.Errorf("network: %w", err)
		}
		return &noop{}, nil
	}
	return newNodeWithTransport(cfg, t)
}

func (n *node) Enabled() bool { return true }

func (n *node) Start(ctx context.Context) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.ctx != nil {
		return fmt.Errorf("network: already started")
	}
	n.ctx, n.cancel = context.WithCancel(ctx)

	// Transport fan-in: peer frames arrive here and are submitted to the
	// local shard engine so every member converges on the same DAG.
	if err := n.transport.Start(n.ctx, n.onPeerFrame); err != nil {
		n.cancel()
		n.ctx = nil
		return fmt.Errorf("network: transport start: %w", err)
	}
	return nil
}

func (n *node) Stop(ctx context.Context) error {
	n.mu.Lock()
	cancel := n.cancel
	shards := n.shards
	n.shards = map[string]*Shard{}
	n.ctx, n.cancel = nil, nil
	n.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	for _, s := range shards {
		s.close()
	}
	return n.transport.Stop(ctx)
}

func (n *node) WriterFor(shardID string) (string, bool) {
	owner := n.router.ownerOf(shardID)
	return string(owner), owner == n.id
}

func (n *node) MembersFor(shardID string) []string {
	ms := n.router.membersFor(shardID)
	out := make([]string, len(ms))
	for i, m := range ms {
		out[i] = string(m)
	}
	return out
}

func (n *node) Metrics() *Metrics { return n.metrics }

// shard returns the per-shard state, creating it (and its Quasar engine) on
// first use.
func (n *node) shard(shardID string) (*Shard, error) {
	n.mu.RLock()
	s, ok := n.shards[shardID]
	ctx := n.ctx
	n.mu.RUnlock()
	if ok {
		return s, nil
	}
	if ctx == nil {
		return nil, fmt.Errorf("network: not started")
	}

	n.mu.Lock()
	defer n.mu.Unlock()
	if s, ok = n.shards[shardID]; ok {
		return s, nil
	}

	members := n.router.membersFor(shardID)
	s, err := newShard(ctx, shardID, members, n.cfg.Replication, n.metrics, n.onLocalApply(shardID))
	if err != nil {
		return nil, err
	}
	n.shards[shardID] = s
	n.metrics.ActiveShards.Inc()
	return s, nil
}

// onPeerFrame is the transport callback: a remote member submitted a frame,
// we cross-feed it into our local engine so consensus converges.
//
// R1 fix: the Envelope's ShardID is the trust boundary — we route by it, so
// the inner Frame MUST declare the same shardID (its checksum binds to that
// value, and Quasar's ChainID = SHA256(shardID) must match the engine we're
// about to submit into). If the two disagree the envelope is malformed or
// hostile; drop it, bump the rejection metric, and return. This closes the
// cross-shard state-injection probe.
func (n *node) onPeerFrame(env Envelope) {
	if env.Frame.ShardID != env.ShardID {
		n.metrics.FramesRejectedShardMismatch.Inc()
		return
	}
	s, err := n.shard(env.ShardID)
	if err != nil {
		n.metrics.ApplyErrors.Inc()
		return
	}
	if err := s.ingestRemote(env.Frame); err != nil {
		n.metrics.ApplyErrors.Inc()
	}
}

// onLocalApply is the callback plumbed into each shard's Quasar finalize
// loop. Apply is per-(shard,salt,cksm) idempotent so a pod replaying its
// WAL on restart is a no-op.
func (n *node) onLocalApply(shardID string) ApplyFunc {
	return func(f Frame) error {
		// v0 default apply: record into shard state. SQLite write-back is
		// wired by InstallWALHook when a concrete conn is present.
		return nil
	}
}
