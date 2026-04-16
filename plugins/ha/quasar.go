package ha

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// StaticWriter always routes writes to a fixed URL.
type StaticWriter struct{ Target string }

func (s *StaticWriter) IsWriter() bool         { return s.Target == "" }
func (s *StaticWriter) RedirectTarget() string { return s.Target }

// WriterProvider abstracts writer-pin strategies.
type WriterProvider interface {
	IsWriter() bool
	RedirectTarget() string
}

// QuasarWriter pins one node as the SQLite writer using Quasar-style
// deterministic ranking over a heartbeat-based alive set.
//
// Quasar consensus is leaderless — all nodes are equal validators. But SQLite
// has a single-writer constraint, so we rank alive peers by NodeID and pin the
// lowest-sorted as the writer. All others are replicas that 307 mutating HTTP
// to the writer and apply change-sets via async replication.
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
	target    string               // current writer URL
	closeCh   chan struct{}
	ready     chan struct{}
	readyOnce sync.Once
	started   atomic.Bool
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
func (w *QuasarWriter) Ready() <-chan struct{}    { return w.ready }
func (w *QuasarWriter) IsWriter() bool           { return w.writerID() == w.cfg.NodeID }
func (w *QuasarWriter) RedirectTarget() string   { w.mu.RLock(); defer w.mu.RUnlock(); return w.target }

func (w *QuasarWriter) writerID() string {
	w.mu.RLock()
	defer w.mu.RUnlock()
	now := time.Now()
	ids := make([]string, 0, len(w.alive))
	for id, last := range w.alive {
		if now.Sub(last) <= w.cfg.LeaseTimeout {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return w.cfg.NodeID
	}
	sort.Strings(ids)
	return ids[0]
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

			writerID := w.writerID()
			target := w.targetFor(writerID)

			w.mu.Lock()
			if target != w.target {
				slog.Info("ha: writer changed", "from", w.target, "to", target, "writer", writerID)
			}
			w.target = target
			w.mu.Unlock()
			w.readyOnce.Do(func() { close(w.ready) })
		}
	}
}

func (w *QuasarWriter) targetFor(id string) string {
	if id == w.cfg.NodeID {
		return w.cfg.LocalTarget
	}
	w.mu.RLock()
	defer w.mu.RUnlock()
	if u, ok := w.urls[id]; ok {
		return u
	}
	return w.cfg.LocalTarget
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
