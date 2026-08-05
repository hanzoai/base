package ha

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/hanzoai/ha"
	"time"

	luxlog "github.com/luxfi/log"
)

// StaticWriter always routes writes to a fixed URL.
type StaticWriter struct{ Target string }

func (s *StaticWriter) IsWriter(string) bool         { return s.Target == "" }
func (s *StaticWriter) RedirectTarget(string) string { return s.Target }

// WriterProvider abstracts writer-pin strategies.
//
// Both methods take the ownership KEY, because ownership is per tenant, not per
// process: store.Key.String() ("org/apps/a/projects/p") names one SQLite file,
// and SQLite's single-writer constraint is per FILE. A process-wide writer would
// pin every tenant's writes to one node — correct, and a bottleneck that grows
// with tenant count.
type WriterProvider interface {
	IsWriter(key string) bool
	RedirectTarget(key string) string
}

// QuasarWriter pins ONE node per KEY as that key's SQLite writer, over a
// heartbeat-based live set.
//
// Quasar consensus is leaderless — all nodes are equal validators — and SQLite's
// single-writer constraint is per FILE, not per process. So ownership is per key
// (store.Key.String() names one file) and comes from ha.Owner: Rendezvous (HRW)
// weight over the live members, computed identically on every node from the same
// membership without asking anyone. A node that does not own a key 307s mutating
// HTTP to the one that does, and applies change-sets via async replication.
//
// It previously ranked the live set by NodeID and pinned the lowest-sorted as the
// writer for EVERYTHING. That is correct for SQLite and wrong for a fleet: every
// tenant's writes land on one node, and a rolling restart hands the whole write
// load to whoever sorts first next. HRW spreads ownership and moves only the keys
// a departing node owned, so losing one of N relocates 1/N of the tenants.
//
// WHAT THIS DOES NOT YET DO. Heartbeat liveness cannot make a deposed or
// partitioned owner STOP writing, so two nodes with different views of the live
// set can each believe they own a key. store/multitenant.go documents that window
// honestly ("during an HPA rebalance a (short) single-writer window may overlap
// across pods; ops MUST drain before scaling"). Closing it needs ha.Leases: the
// owner stamps a monotone Round onto each write and the store refuses any round
// below the highest it has admitted, which makes a deposed writer harmless
// instead of merely unlikely. The election is now the shape that accepts it.
//
// Transport: HTTP /_ha/heartbeat by default. Compose with plugins/zap for
// sub-ms ZAP transport (the ZAP plugin provides mDNS discovery + binary
// messaging for the fast path).
//
// O(peers) memory, O(1) per heartbeat.
type QuasarWriter struct {
	cfg QuasarConfig

	mu        sync.RWMutex
	alive     map[string]time.Time // nodeID -> last heartbeat
	urls      map[string]string    // nodeID -> advertised base URL
	closeCh   chan struct{}
	ready     chan struct{}
	readyOnce sync.Once
	started   atomic.Bool
	lastSize  atomic.Int32 // last logged live-member count
}

type QuasarConfig struct {
	NodeID            string
	LocalTarget       string   // this node's reachable URL
	Peers             []string // peer base URLs
	HeartbeatInterval time.Duration
	LeaseTimeout      time.Duration
}

func NewQuasarWriter(cfg QuasarConfig) (*QuasarWriter, error) {
	if cfg.LocalTarget == "" {
		return nil, fmt.Errorf("LocalTarget is required")
	}
	if cfg.NodeID == "" {
		cfg.NodeID, _ = os.Hostname()
	}
	if cfg.HeartbeatInterval == 0 {
		cfg.HeartbeatInterval = 500 * time.Millisecond
	}
	if cfg.LeaseTimeout == 0 {
		cfg.LeaseTimeout = 3 * cfg.HeartbeatInterval
	}
	w := &QuasarWriter{
		cfg:     cfg,
		alive:   map[string]time.Time{cfg.NodeID: time.Now()},
		urls:    map[string]string{cfg.NodeID: cfg.LocalTarget},
		closeCh: make(chan struct{}),
		ready:   make(chan struct{}, 1),
	}
	w.start()
	return w, nil
}

func (w *QuasarWriter) start() {
	if !w.started.CompareAndSwap(false, true) {
		return
	}
	go w.loop()
}

func (w *QuasarWriter) Close()                   { close(w.closeCh) }
func (w *QuasarWriter) Ready() <-chan struct{}   { return w.ready }
func (w *QuasarWriter) IsWriter(key string) bool { return w.writerID(key) == w.cfg.NodeID }

// RedirectTarget is the owner's advertised URL for key — empty when no member
// owns it, which the caller MUST treat as "no writer" rather than "me".
func (w *QuasarWriter) RedirectTarget(key string) string {
	o, ok := ha.Owner(key, w.members())
	if !ok {
		return ""
	}
	return o.Addr
}

// members is the live, writer-eligible set: every node whose heartbeat is inside
// the lease timeout. This is the ONLY thing heartbeats decide now — liveness.
// WHO writes is ha.Owner's answer, computed identically on every node.
func (w *QuasarWriter) members() []ha.Member {
	w.mu.RLock()
	defer w.mu.RUnlock()
	now := time.Now()
	out := make([]ha.Member, 0, len(w.alive))
	for id, last := range w.alive {
		if now.Sub(last) <= w.cfg.LeaseTimeout {
			out = append(out, ha.Member{ID: id, Addr: w.urls[id]})
		}
	}
	return out
}

// writerID names the owner of key by Rendezvous (HRW) weight over the live set.
//
// It replaced `sort.Strings(alive); return alive[0]`, which made the
// lowest-sorted node the writer for EVERY tenant — one hotspot, and a rolling
// restart that hands the whole write load to whichever node sorts first next.
// HRW spreads ownership across the set and moves only the keys a departing node
// owned, so losing one node relocates 1/N of the tenants instead of all of them.
//
// Deterministic and order-independent, so every node reaches the same answer
// from the same membership without asking anyone.
func (w *QuasarWriter) writerID(key string) string {
	o, ok := ha.Owner(key, w.members())
	if !ok {
		return w.cfg.NodeID // fail to SELF only when the set is empty
	}
	return o.ID
}

func (w *QuasarWriter) loop() {
	tick := time.NewTicker(w.cfg.HeartbeatInterval)
	defer tick.Stop()
	client := &http.Client{Timeout: w.cfg.HeartbeatInterval}
	for {
		select {
		case <-w.closeCh:
			return
		case <-tick.C:
			w.mu.Lock()
			w.alive[w.cfg.NodeID] = time.Now()
			w.mu.Unlock()

			var wg sync.WaitGroup
			for _, peer := range w.cfg.Peers {
				wg.Add(1)
				go func(peer string) {
					defer wg.Done()
					w.beat(client, peer)
				}(peer)
			}
			wg.Wait()

			// Liveness only. There is no longer ONE writer to cache: ownership is
			// per key, so RedirectTarget(key) computes it from the live set on
			// demand. Caching a single target here is what made every tenant's
			// writes land on one node.
			//
			// MEMBERSHIP is what to log now, and it is the more useful signal: it
			// is the input every node hashes, so two nodes disagreeing about it is
			// the one way they can disagree about an owner.
			if n := len(w.members()); n != int(w.lastSize.Swap(int32(n))) {
				luxlog.Info("ha: membership changed", "live", n, "self", w.cfg.NodeID)
			}
			w.readyOnce.Do(func() { close(w.ready) })
		}
	}
}

func (w *QuasarWriter) beat(client *http.Client, peer string) {
	endpoint := peer + "/_ha/heartbeat"
	body, _ := json.Marshal(heartbeat{NodeID: w.cfg.NodeID, Target: w.cfg.LocalTarget})
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, endpoint, strings.NewReader(string(body)))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()
	var reply heartbeat
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4096)).Decode(&reply); err != nil {
		return
	}
	w.Ingest(reply)
}

// Ingest processes an incoming heartbeat (from any transport — HTTP or ZAP).
// Exported so the ZAP plugin can feed heartbeats in from the binary path.
func (w *QuasarWriter) Ingest(h heartbeat) {
	if h.NodeID == "" {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	w.alive[h.NodeID] = time.Now()
	if h.Target != "" {
		w.urls[h.NodeID] = h.Target
	}
}

// HandleHeartbeat is the HTTP handler for /_ha/heartbeat.
func (w *QuasarWriter) HandleHeartbeat(rw http.ResponseWriter, r *http.Request) {
	var in heartbeat
	if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&in); err != nil {
		http.Error(rw, err.Error(), http.StatusBadRequest)
		return
	}
	w.Ingest(in)
	rw.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(rw).Encode(heartbeat{NodeID: w.cfg.NodeID, Target: w.cfg.LocalTarget})
}

// Heartbeat is the wire format for the alive-set protocol.
// Exported so the ZAP plugin can construct heartbeats.
type heartbeat = Heartbeat

type Heartbeat struct {
	NodeID string `json:"node_id"`
	Target string `json:"target"`
}

// SelfHeartbeat returns this node's identity for external transports.
func (w *QuasarWriter) SelfHeartbeat() Heartbeat {
	return Heartbeat{NodeID: w.cfg.NodeID, Target: w.cfg.LocalTarget}
}
