// Copyright (c) 2025, Hanzo Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.
//
// Membership — the set of Base instances currently reachable.
//
// One interface, one DNS-based implementation that works anywhere a
// resolver does. Zero K8s API dependency. No Consul. No etcd. No
// gossip protocol. Just the DNS that every orchestrator already ships.
//
//   - docker compose    service name → container IP       (1 addr)
//   - k8s headless svc  Service DNS  → all pod IPs        (N addrs)
//   - mDNS (Avahi)      _hanzo-base.local → LAN peers     (N addrs)
//   - external DNS      cluster.hanzo.ai  → cloud pods    (N addrs)
//   - static compose    base-0,base-1,base-2              (3 × 1)
//
// The seed list (BASE_PEERS) may mix all of these. Each entry is
// re-resolved on a fixed interval. A wildcard-style entry that
// resolves to multiple addresses is expanded to that many members.
// A singleton entry that resolves to a single address stays singleton.
//
// Scale flow: pod starts, reads BASE_PEERS, resolves every entry,
// builds the initial member set. Every `refreshInterval` the whole
// thing is re-resolved; new addresses trigger peer dials, gone
// addresses trigger connection drops and router recomputation.
// No restart needed to scale 1 → N → 1.

package network

import (
	"context"
	"net"
	"sort"
	"strings"
	"sync"
	"time"
)

// refreshInterval is the DNS re-poll cadence. 5s picks up most
// orchestrator scale-up events within one cycle. Too aggressive and
// we hammer the resolver for no gain; too lazy and scale feels slow.
const refreshInterval = 5 * time.Second

// Membership is the live view of reachable Base instances. Consumers
// read a snapshot via Members(), or subscribe to change notifications
// via Watch(). The self NodeID is always included in the snapshot.
type Membership interface {
	// Members returns the current sorted member list. Includes self.
	// Never nil; a singleton returns a 1-element slice.
	Members() []NodeID

	// Watch returns a channel that emits the member list on every
	// change. The first value is delivered immediately on subscribe.
	// The channel closes when ctx is cancelled.
	Watch(ctx context.Context) <-chan []NodeID

	// Close releases any resources. Safe to call multiple times.
	Close() error
}

// Resolver turns a seed entry into a set of addresses. The default
// implementation is `net.Resolver{}.LookupHost`; tests inject a
// predictable map.
type Resolver interface {
	LookupHost(ctx context.Context, host string) ([]string, error)
}

type defaultResolver struct{ r *net.Resolver }

func (d defaultResolver) LookupHost(ctx context.Context, host string) ([]string, error) {
	if d.r == nil {
		d.r = net.DefaultResolver
	}
	return d.r.LookupHost(ctx, host)
}

// dnsMembership polls BASE_PEERS through a Resolver. One member per
// resolved address. Self is kept in the set even if DNS hasn't come
// back yet so the singleton case is correct from t=0.
type dnsMembership struct {
	self     string // NodeID of this pod
	seeds    []string
	port     string // carried across DNS lookups
	resolver Resolver
	interval time.Duration

	mu       sync.RWMutex
	members  []NodeID
	subs     []chan []NodeID

	ctx    context.Context
	cancel context.CancelFunc
	done   chan struct{}
}

// NewDNSMembership builds a DNS-based Membership. seeds are
// BASE_PEERS entries as "host" or "host:port" — port is required so
// the returned NodeIDs are addressable. A seed whose host already
// resolves to a single literal IP is used as-is.
//
// The initial Members() snapshot contains self, blocking while the
// first round of DNS resolution completes (bounded by a 2s context).
// Subsequent refreshes are fully async.
func NewDNSMembership(ctx context.Context, self string, seeds []string) *dnsMembership {
	return newDNSMembership(ctx, self, seeds, defaultResolver{}, refreshInterval)
}

func newDNSMembership(ctx context.Context, self string, seeds []string, r Resolver, interval time.Duration) *dnsMembership {
	cctx, cancel := context.WithCancel(ctx)
	port := extractFirstPort(seeds)
	m := &dnsMembership{
		self:     self,
		seeds:    append([]string(nil), seeds...),
		port:     port,
		resolver: r,
		interval: interval,
		members:  []NodeID{NodeID(self)},
		ctx:      cctx,
		cancel:   cancel,
		done:     make(chan struct{}),
	}

	// First resolve synchronously so the initial snapshot is real.
	// Bounded to 2s so a stubborn resolver can't block Start().
	firstCtx, firstCancel := context.WithTimeout(cctx, 2*time.Second)
	m.refresh(firstCtx)
	firstCancel()

	go m.loop()
	return m
}

func (m *dnsMembership) Members() []NodeID {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]NodeID, len(m.members))
	copy(out, m.members)
	return out
}

func (m *dnsMembership) Watch(ctx context.Context) <-chan []NodeID {
	// Buffer of 1 so the initial snapshot delivery (inside the same
	// lock as Subscribe) cannot race with a refresh fan-out. Both
	// writers are non-blocking and share a single buffer slot — if
	// two updates land between reads the consumer sees the latest.
	ch := make(chan []NodeID, 1)
	m.mu.Lock()
	snap := make([]NodeID, len(m.members))
	copy(snap, m.members)
	ch <- snap // safe: empty buffer, same goroutine as subscribe
	m.subs = append(m.subs, ch)
	m.mu.Unlock()
	go func() {
		<-ctx.Done()
		m.unsubscribe(ch)
	}()
	return ch
}

func (m *dnsMembership) Close() error {
	m.cancel()
	<-m.done
	m.mu.Lock()
	for _, ch := range m.subs {
		close(ch)
	}
	m.subs = nil
	m.mu.Unlock()
	return nil
}

func (m *dnsMembership) loop() {
	defer close(m.done)
	t := time.NewTicker(m.interval)
	defer t.Stop()
	for {
		select {
		case <-m.ctx.Done():
			return
		case <-t.C:
			m.refresh(m.ctx)
		}
	}
}

func (m *dnsMembership) refresh(ctx context.Context) {
	addrs := map[string]struct{}{}
	// Always include self so the singleton case converges on the
	// first refresh even if DNS for every seed fails.
	addrs[m.self] = struct{}{}

	for _, seed := range m.seeds {
		host, seedPort := splitHostPort(seed)
		if seedPort == "" {
			seedPort = m.port
		}
		resolved, err := m.resolver.LookupHost(ctx, host)
		if err != nil {
			// A single seed's DNS failure doesn't invalidate the
			// rest. Scale-up events always trigger at least one
			// transient NXDOMAIN before propagation.
			continue
		}
		for _, addr := range resolved {
			// NodeIDs are "host:port" strings so the transport
			// can dial directly without re-parsing.
			if seedPort != "" {
				addrs[addr+":"+seedPort] = struct{}{}
			} else {
				addrs[addr] = struct{}{}
			}
		}
	}

	next := make([]NodeID, 0, len(addrs))
	for a := range addrs {
		next = append(next, NodeID(a))
	}
	sort.Slice(next, func(i, j int) bool { return next[i] < next[j] })

	// Hold the write lock across the fan-out: (1) the members
	// field write must be atomic with the snapshot used for
	// subscribers (consumers should never see a mismatch), and
	// (2) unsubscribe takes the same lock and closes channels,
	// so holding here is what prevents a send-to-closed race.
	// Sends are non-blocking so a slow subscriber cannot stall
	// the polling loop even under contention.
	m.mu.Lock()
	if equalMembers(m.members, next) {
		m.mu.Unlock()
		return
	}
	m.members = next
	snap := make([]NodeID, len(next))
	copy(snap, next)
	for _, ch := range m.subs {
		select {
		case ch <- snap:
		default:
		}
	}
	m.mu.Unlock()
}

func (m *dnsMembership) unsubscribe(ch chan []NodeID) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, s := range m.subs {
		if s == ch {
			m.subs = append(m.subs[:i], m.subs[i+1:]...)
			close(ch)
			return
		}
	}
}

// equalMembers is a cheap ordered compare; the caller sorts both
// sides so index-by-index suffices.
func equalMembers(a, b []NodeID) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// splitHostPort is like net.SplitHostPort but tolerates missing port.
func splitHostPort(s string) (host, port string) {
	if i := strings.LastIndex(s, ":"); i >= 0 {
		return s[:i], s[i+1:]
	}
	return s, ""
}

// extractFirstPort returns the port of the first seed that has one.
// Falls back to "9651" (Quasar default) when every seed is host-only.
func extractFirstPort(seeds []string) string {
	for _, s := range seeds {
		if _, p := splitHostPort(s); p != "" {
			return p
		}
	}
	return "9651"
}

// staticMembership is the in-process test Membership — fixed list,
// no DNS. Handy for unit tests that want to inject specific topologies.
type staticMembership struct {
	self    NodeID
	members []NodeID
	mu      sync.RWMutex
	subs    []chan []NodeID
	done    chan struct{}
}

// NewStaticMembership returns a Membership with a fixed member set.
// self is automatically included if not already present.
func NewStaticMembership(self string, members []string) *staticMembership {
	set := map[string]struct{}{self: {}}
	for _, m := range members {
		set[m] = struct{}{}
	}
	ns := make([]NodeID, 0, len(set))
	for m := range set {
		ns = append(ns, NodeID(m))
	}
	sort.Slice(ns, func(i, j int) bool { return ns[i] < ns[j] })
	return &staticMembership{
		self:    NodeID(self),
		members: ns,
		done:    make(chan struct{}),
	}
}

func (s *staticMembership) Members() []NodeID {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]NodeID, len(s.members))
	copy(out, s.members)
	return out
}

func (s *staticMembership) Watch(ctx context.Context) <-chan []NodeID {
	// Same pattern as dnsMembership.Watch — initial send inside the
	// same lock as Subscribe so refresh fan-out can't prefill the
	// buffer and deadlock the initial snapshot.
	ch := make(chan []NodeID, 1)
	s.mu.Lock()
	snap := make([]NodeID, len(s.members))
	copy(snap, s.members)
	ch <- snap
	s.subs = append(s.subs, ch)
	s.mu.Unlock()
	go func() {
		<-ctx.Done()
		s.mu.Lock()
		for i, sub := range s.subs {
			if sub == ch {
				s.subs = append(s.subs[:i], s.subs[i+1:]...)
				close(ch)
				break
			}
		}
		s.mu.Unlock()
	}()
	return ch
}

func (s *staticMembership) Close() error {
	select {
	case <-s.done:
	default:
		close(s.done)
	}
	return nil
}

// Set replaces the member list and notifies subscribers. Test-only.
func (s *staticMembership) Set(members []string) {
	set := map[string]struct{}{string(s.self): {}}
	for _, m := range members {
		set[m] = struct{}{}
	}
	ns := make([]NodeID, 0, len(set))
	for m := range set {
		ns = append(ns, NodeID(m))
	}
	sort.Slice(ns, func(i, j int) bool { return ns[i] < ns[j] })

	// Hold the lock across the fan-out so Watch-unsubscribe (which
	// closes channels under the same lock) cannot race with sends.
	s.mu.Lock()
	s.members = ns
	snap := make([]NodeID, len(ns))
	copy(snap, ns)
	for _, ch := range s.subs {
		select {
		case ch <- snap:
		default:
		}
	}
	s.mu.Unlock()
}
