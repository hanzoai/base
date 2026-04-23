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

	// membership is the live view of reachable members. When set, Start
	// subscribes to change events and rebuilds the router on every
	// update. Dynamic reconnects are delegated to the transport.
	membership Membership

	mu     sync.RWMutex
	shards map[string]*Shard
	walSrc walSource
	ctx    context.Context
	cancel context.CancelFunc
}

// newNode constructs the live node with the production ZAP transport and
// a DNS-based Membership built from BASE_PEERS. Tests that wire peers
// together in-process use NewWithTransport.
func newNode(cfg Config) (*node, error) {
	n, err := newNodeWithTransport(cfg, newZapTransport(cfg))
	if err != nil {
		return nil, err
	}
	// Production Membership tracks BASE_PEERS through DNS: a seed that
	// resolves to N addresses yields N members; scale events propagate
	// within refreshInterval (5s by default).
	n.membership = NewDNSMembership(context.Background(), cfg.NodeID, cfg.Peers)
	return n, nil
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
		// Test-only path: callers may pass a nil transport explicitly. The
		// production entry point newNode() always injects the ZAP transport.
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

// SetMembership injects a Membership source. The node subscribes on
// Start and rebuilds the router on every change. Tests use this to
// drive scale events; production wires a dnsMembership automatically
// from newNode.
func (n *node) SetMembership(m Membership) {
	n.mu.Lock()
	n.membership = m
	n.mu.Unlock()
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
	if n.ctx != nil {
		n.mu.Unlock()
		return fmt.Errorf("network: already started")
	}
	n.ctx, n.cancel = context.WithCancel(ctx)
	ourCtx := n.ctx
	membership := n.membership
	n.mu.Unlock()

	// Transport fan-in: peer frames arrive here and are submitted to the
	// local shard engine so every member converges on the same DAG.
	if err := n.transport.Start(ourCtx, n.onPeerFrame); err != nil {
		n.cancel()
		n.mu.Lock()
		n.ctx = nil
		n.mu.Unlock()
		return fmt.Errorf("network: transport start: %w", err)
	}

	// Dynamic-membership watch: every Membership change rebuilds the
	// router's member set. Transport learns about new peers via its own
	// reconnect loop — we don't have to push dials from here. Dropped
	// peers naturally time out on the transport side.
	if membership != nil {
		go n.watchMembership(ourCtx, membership)
	}
	return nil
}

// watchMembership blocks on the Membership change stream and forwards
// updates to the router. Exits when ctx is cancelled (Stop).
func (n *node) watchMembership(ctx context.Context, m Membership) {
	updates := m.Watch(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case members, ok := <-updates:
			if !ok {
				return
			}
			n.router.setMembers(members)
			n.metrics.MembershipSize.Set(float64(len(members)))
		}
	}
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
