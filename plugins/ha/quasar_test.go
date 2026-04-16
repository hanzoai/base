package ha

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestQuasarWriter_SingleNodeIsWriter(t *testing.T) {
	w, err := NewQuasarWriter(QuasarConfig{
		NodeID:            "node1",
		LocalTarget:       "http://127.0.0.1:8090",
		HeartbeatInterval: 20 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()

	select {
	case <-w.Ready():
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for election")
	}

	if !w.IsWriter() {
		t.Fatal("expected single node to be writer")
	}
	if got := w.RedirectTarget(); got != "http://127.0.0.1:8090" {
		t.Fatalf("unexpected redirect target: %q", got)
	}
}

func TestQuasarWriter_LowestIDWins(t *testing.T) {
	n1 := startNode(t, "aaa")
	n2 := startNode(t, "zzz")
	defer n1.close()
	defer n2.close()

	n1.w.cfg.Peers = []string{n2.url}
	n2.w.cfg.Peers = []string{n1.url}

	if !waitFor(func() bool {
		return n1.w.RedirectTarget() == n1.url && n2.w.RedirectTarget() == n1.url
	}, 500*time.Millisecond) {
		t.Fatalf("nodes did not converge on aaa as writer: n1=%q n2=%q",
			n1.w.RedirectTarget(), n2.w.RedirectTarget())
	}

	if !n1.w.IsWriter() || n2.w.IsWriter() {
		t.Fatalf("unexpected writer state: n1=%v n2=%v", n1.w.IsWriter(), n2.w.IsWriter())
	}
}

func TestQuasarWriter_LeaseExpiry(t *testing.T) {
	n1 := startNode(t, "aaa")
	n2 := startNode(t, "zzz")
	defer n2.close()

	n1.w.cfg.Peers = []string{n2.url}
	n2.w.cfg.Peers = []string{n1.url}

	if !waitFor(func() bool { return n2.w.RedirectTarget() == n1.url }, 500*time.Millisecond) {
		t.Fatal("n2 never saw n1 as writer")
	}

	n1.close()

	if !waitFor(func() bool { return n2.w.IsWriter() }, 2*time.Second) {
		t.Fatalf("n2 did not promote itself after n1 died, target=%q", n2.w.RedirectTarget())
	}
}

// --- helpers ---

type testNode struct {
	w      *QuasarWriter
	srv    *httptest.Server
	url    string
	closed bool
}

func startNode(t *testing.T, id string) *testNode {
	t.Helper()
	w, err := NewQuasarWriter(QuasarConfig{
		NodeID:            id,
		LocalTarget:       "http://placeholder",
		HeartbeatInterval: 30 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/_ha/heartbeat", w.HandleHeartbeat)
	srv := httptest.NewServer(mux)

	w.cfg.LocalTarget = srv.URL
	w.mu.Lock()
	w.urls[id] = srv.URL
	w.mu.Unlock()

	return &testNode{w: w, srv: srv, url: srv.URL}
}

func (n *testNode) close() {
	if n.closed {
		return
	}
	n.closed = true
	n.w.Close()
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
