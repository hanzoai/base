package ha

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// anyKey is the org-wide ownership unit ("" — no X-Org-Id), the key a request
// carries when it addresses the app rather than one tenant.
const anyKey = ""

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

	if !w.IsWriter(anyKey) {
		t.Fatal("expected single node to be writer")
	}
	if got := w.RedirectTarget(anyKey); got != "http://127.0.0.1:8090" {
		t.Fatalf("unexpected redirect target: %q", got)
	}
}

// The property that must hold is AGREEMENT and UNIQUENESS, not "the
// lowest-sorted id wins" — that was an artifact of sorting, and asserting it
// would forbid the very spreading HRW exists to do. Which node owns a key is
// deliberately unpredictable; that exactly one does, and that every node names
// the same one, is what callers depend on.
func TestQuasarWriter_NodesAgreeOnOneOwnerPerKey(t *testing.T) {
	n1 := startNode(t, "aaa")
	n2 := startNode(t, "zzz")
	defer n1.close()
	defer n2.close()

	n1.w.cfg.Peers = []string{n2.url}
	n2.w.cfg.Peers = []string{n1.url}

	for _, key := range []string{"", "acme", "globex", "org/apps/a/projects/p"} {
		k := key
		if !waitFor(func() bool {
			t1, t2 := n1.w.RedirectTarget(k), n2.w.RedirectTarget(k)
			return t1 != "" && t1 == t2
		}, time.Second) {
			t.Fatalf("key %q: nodes disagree on owner: n1=%q n2=%q",
				k, n1.w.RedirectTarget(k), n2.w.RedirectTarget(k))
		}
		if n1.w.IsWriter(k) == n2.w.IsWriter(k) {
			t.Fatalf("key %q: expected exactly one writer, both=%v", k, n1.w.IsWriter(k))
		}
	}
}

// HRW must actually SPREAD ownership, or it is an expensive sort: over many keys
// both members must own some. This is the regression that would catch a revert to
// a single pinned writer.
func TestQuasarWriter_OwnershipSpreadsAcrossMembers(t *testing.T) {
	n1 := startNode(t, "aaa")
	n2 := startNode(t, "zzz")
	defer n1.close()
	defer n2.close()

	n1.w.cfg.Peers = []string{n2.url}
	n2.w.cfg.Peers = []string{n1.url}

	if !waitFor(func() bool { return len(n1.w.members()) == 2 }, time.Second) {
		t.Fatalf("n1 never saw both members, live=%d", len(n1.w.members()))
	}

	mine := 0
	const n = 200
	for i := 0; i < n; i++ {
		if n1.w.IsWriter(fmt.Sprintf("tenant-%d", i)) {
			mine++
		}
	}
	if mine == 0 || mine == n {
		t.Fatalf("ownership did not spread: node aaa owns %d/%d keys", mine, n)
	}
}

func TestQuasarWriter_LeaseExpiry(t *testing.T) {
	n1 := startNode(t, "aaa")
	n2 := startNode(t, "zzz")
	defer n2.close()

	n1.w.cfg.Peers = []string{n2.url}
	n2.w.cfg.Peers = []string{n1.url}

	// Pick a key n1 owns while both are live, so the failover assertion is about
	// a key that genuinely has to move.
	if !waitFor(func() bool { return len(n2.w.members()) == 2 }, time.Second) {
		t.Fatal("n2 never saw both members")
	}
	key := ""
	for i := 0; i < 200; i++ {
		k := fmt.Sprintf("tenant-%d", i)
		if n2.w.RedirectTarget(k) == n1.url {
			key = k
			break
		}
	}
	if key == "" {
		t.Fatal("no key owned by n1 to fail over")
	}

	n1.close()

	if !waitFor(func() bool { return n2.w.IsWriter(key) }, 2*time.Second) {
		t.Fatalf("n2 did not take over key %q after n1 died, target=%q",
			key, n2.w.RedirectTarget(key))
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
