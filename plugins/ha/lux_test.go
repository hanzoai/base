package ha

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestLuxLeader_SingleNodeIsLeader verifies that a node with no peers
// immediately elects itself and reports itself as leader.
func TestLuxLeader_SingleNodeIsLeader(t *testing.T) {
	l, err := NewLuxLeader(LuxConfig{
		NodeID:            "node1",
		LocalTarget:       "http://127.0.0.1:8090",
		HeartbeatInterval: 20 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()

	select {
	case <-l.Ready():
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for election")
	}

	if !l.IsLeader() {
		t.Fatal("expected single node to be leader")
	}
	if got := l.RedirectTarget(); got != "http://127.0.0.1:8090" {
		t.Fatalf("unexpected redirect target: %q", got)
	}
}

// TestLuxLeader_LowestIDWins brings up two nodes and verifies that the
// lexicographically lowest NodeID becomes the leader of both.
func TestLuxLeader_LowestIDWins(t *testing.T) {
	// Spin up two nodes on ephemeral ports. Each serves /_ha/heartbeat.
	n1 := startNode(t, "aaa")
	n2 := startNode(t, "zzz")
	defer n1.close()
	defer n2.close()

	// Wire them as peers of each other.
	n1.l.cfg.Peers = []string{n2.url}
	n2.l.cfg.Peers = []string{n1.url}

	// Wait for a couple of heartbeat cycles.
	if !waitFor(func() bool { return n1.l.RedirectTarget() == n1.url && n2.l.RedirectTarget() == n1.url }, 500*time.Millisecond) {
		t.Fatalf("nodes did not converge on aaa as leader: n1=%q n2=%q",
			n1.l.RedirectTarget(), n2.l.RedirectTarget())
	}

	if !n1.l.IsLeader() || n2.l.IsLeader() {
		t.Fatalf("unexpected leadership: n1.IsLeader=%v n2.IsLeader=%v", n1.l.IsLeader(), n2.l.IsLeader())
	}
}

// TestLuxLeader_LeaseExpiry verifies that when a leader stops heartbeating,
// remaining alive peers drop it from the alive set and promote a new leader.
func TestLuxLeader_LeaseExpiry(t *testing.T) {
	n1 := startNode(t, "aaa")
	n2 := startNode(t, "zzz")
	defer n2.close()

	n1.l.cfg.Peers = []string{n2.url}
	n2.l.cfg.Peers = []string{n1.url}

	if !waitFor(func() bool { return n2.l.RedirectTarget() == n1.url }, 500*time.Millisecond) {
		t.Fatal("n2 never saw n1 as leader")
	}

	// Kill n1: stop its server + loop.
	n1.close()

	// After lease timeout, n2 should promote itself.
	if !waitFor(func() bool { return n2.l.IsLeader() }, 2*time.Second) {
		t.Fatalf("n2 did not promote itself after n1 died, target=%q", n2.l.RedirectTarget())
	}
}

// --- helpers ---

type testNode struct {
	l      *LuxLeader
	srv    *httptest.Server
	url    string
	closed bool
}

func startNode(t *testing.T, id string) *testNode {
	t.Helper()
	// Create leader with an initial self-URL placeholder; we'll overwrite after the server is up.
	l, err := NewLuxLeader(LuxConfig{
		NodeID:            id,
		LocalTarget:       "http://placeholder",
		HeartbeatInterval: 30 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/_ha/heartbeat", l.HandleHeartbeat)
	srv := httptest.NewServer(mux)

	// Patch the advertised URL now that we know the server's port.
	l.cfg.LocalTarget = srv.URL
	l.mu.Lock()
	l.urls[id] = srv.URL
	l.mu.Unlock()

	return &testNode{l: l, srv: srv, url: srv.URL}
}

func (n *testNode) close() {
	if n.closed {
		return
	}
	n.closed = true
	n.l.Close()
	n.srv.Close()
}

func waitFor(cond func() bool, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return cond()
}
