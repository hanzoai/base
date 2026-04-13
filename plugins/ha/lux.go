package ha

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// StaticLeader always routes writes to a fixed URL.
type StaticLeader struct{ Target string }

func (s *StaticLeader) IsLeader() bool         { return s.Target == "" }
func (s *StaticLeader) RedirectTarget() string { return s.Target }

// LuxLeader is a lightweight heartbeat-lease leader election.
//
// Peers exchange signed heartbeats on an interval; the alive peer with the
// lowest-sorted NodeID is the leader for the current epoch. Much lighter
// than RAFT — no log, no snapshots, no state machine. O(peers) memory,
// O(1) per heartbeat.
type LuxLeader struct {
	cfg LuxConfig

	mu        sync.RWMutex
	alive     map[string]time.Time
	urls      map[string]string
	target    string
	closeCh   chan struct{}
	ready     chan struct{}
	readyOnce sync.Once
	started   atomic.Bool
}

type LuxConfig struct {
	NodeID            string
	LocalTarget       string
	Peers             []string
	HeartbeatInterval time.Duration
	LeaseTimeout      time.Duration
}

// NewLuxLeader returns a started LuxLeader. Returns an error if LocalTarget
// is missing.
func NewLuxLeader(cfg LuxConfig) (*LuxLeader, error) {
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
	l := &LuxLeader{
		cfg:     cfg,
		alive:   map[string]time.Time{cfg.NodeID: time.Now()},
		urls:    map[string]string{cfg.NodeID: cfg.LocalTarget},
		closeCh: make(chan struct{}),
		ready:   make(chan struct{}, 1),
	}
	l.start()
	return l, nil
}

func (l *LuxLeader) start() {
	if !l.started.CompareAndSwap(false, true) {
		return
	}
	go l.loop()
}

// Close stops the background loop.
func (l *LuxLeader) Close() { close(l.closeCh) }

// Ready returns a channel closed after the first election round.
func (l *LuxLeader) Ready() <-chan struct{} { return l.ready }

// IsLeader reports whether this node is currently the minimum-ranked alive peer.
func (l *LuxLeader) IsLeader() bool { return l.leaderID() == l.cfg.NodeID }

// RedirectTarget returns the current leader's URL.
func (l *LuxLeader) RedirectTarget() string {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.target
}

func (l *LuxLeader) leaderID() string {
	l.mu.RLock()
	defer l.mu.RUnlock()
	now := time.Now()
	ids := make([]string, 0, len(l.alive))
	for id, last := range l.alive {
		if now.Sub(last) <= l.cfg.LeaseTimeout {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return l.cfg.NodeID
	}
	sort.Strings(ids)
	return ids[0]
}

func (l *LuxLeader) loop() {
	tick := time.NewTicker(l.cfg.HeartbeatInterval)
	defer tick.Stop()
	client := &http.Client{Timeout: l.cfg.HeartbeatInterval}
	for {
		select {
		case <-l.closeCh:
			return
		case <-tick.C:
			l.mu.Lock()
			l.alive[l.cfg.NodeID] = time.Now()
			l.mu.Unlock()

			var wg sync.WaitGroup
			for _, peer := range l.cfg.Peers {
				wg.Add(1)
				go func(peer string) {
					defer wg.Done()
					l.beat(client, peer)
				}(peer)
			}
			wg.Wait()

			leaderID := l.leaderID()
			target := l.targetFor(leaderID)

			l.mu.Lock()
			if target != l.target {
				slog.Info("ha: leader changed", "from", l.target, "to", target, "leader", leaderID)
			}
			l.target = target
			l.mu.Unlock()
			l.readyOnce.Do(func() { close(l.ready) })
		}
	}
}

func (l *LuxLeader) targetFor(id string) string {
	if id == l.cfg.NodeID {
		return l.cfg.LocalTarget
	}
	l.mu.RLock()
	defer l.mu.RUnlock()
	if u, ok := l.urls[id]; ok {
		return u
	}
	return l.cfg.LocalTarget
}

func (l *LuxLeader) beat(client *http.Client, peer string) {
	endpoint, err := url.JoinPath(peer, "/_ha/heartbeat")
	if err != nil {
		return
	}
	body, _ := json.Marshal(msg{NodeID: l.cfg.NodeID, Target: l.cfg.LocalTarget})
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
	var reply msg
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4096)).Decode(&reply); err != nil {
		return
	}
	l.ingest(reply)
}

func (l *LuxLeader) ingest(m msg) {
	if m.NodeID == "" {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.alive[m.NodeID] = time.Now()
	if m.Target != "" {
		l.urls[m.NodeID] = m.Target
	}
}

// HandleHeartbeat is the HTTP handler for peer heartbeats.
func (l *LuxLeader) HandleHeartbeat(w http.ResponseWriter, r *http.Request) {
	var in msg
	if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&in); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	l.ingest(in)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(msg{NodeID: l.cfg.NodeID, Target: l.cfg.LocalTarget})
}

type msg struct {
	NodeID string `json:"node_id"`
	Target string `json:"target"`
}
