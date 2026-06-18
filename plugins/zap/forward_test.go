package zap

import (
	"context"
	"io"
	"net"
	"net/http"
	"strconv"
	"testing"
	"time"

	luxlog "github.com/luxfi/log"
	zaplib "github.com/luxfi/zap"
	"github.com/luxfi/zap/forward"
)

// healthHandler builds an http.Handler scoped to the one always-present,
// auth-exempt base route used here: GET /v1/health → 200 + JSON. In
// production the bridged handler is base's fully-wrapped surface
// (e.Server.Handler — the Router.BuildMux mux carrying every middleware and
// route); this scoped std handler is the same shape (an http.Handler) and
// keeps the bridge test free of base's cgo/sqlite app boot. It proves
// forward.Serve drives a real request → response through an http.Handler over
// two live ZAP nodes end-to-end.
func healthHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"message":"API is healthy.","code":200}`))
	})
	return mux
}

// TestForwardBridgeServesHTTPHandler stands up two live ZAP nodes, registers
// the canonical HTTP-over-ZAP terminal (forward.Serve) on the backend, and
// asserts GET /v1/health round-trips through the bridge with status 200 and a
// JSON body. This exercises exactly what plugin.start() wires onto base's node
// via bridgeForward(e.Server.Handler).
func TestForwardBridgeServesHTTPHandler(t *testing.T) {
	bePort := freePort(t)

	gw := zaplib.NewNode(zaplib.NodeConfig{NodeID: "gateway-test", Port: freePort(t), NoDiscovery: true})
	be := zaplib.NewNode(zaplib.NodeConfig{NodeID: "base-test", Port: bePort, NoDiscovery: true})
	if err := gw.Start(); err != nil {
		t.Fatalf("gateway node start: %v", err)
	}
	defer gw.Stop()
	if err := be.Start(); err != nil {
		t.Fatalf("backend node start: %v", err)
	}
	defer be.Stop()

	// Register the Forward terminal on the backend node — the same call
	// bridgeForward makes, with the same kind of http.Handler.
	forward.Serve(be, healthHandler())

	// Gateway dials the backend, then waits until the peer is visible.
	if err := gw.ConnectDirect("127.0.0.1:" + strconv.Itoa(bePort)); err != nil {
		t.Fatalf("gateway dial backend: %v", err)
	}
	waitForPeer(t, gw, "base-test")

	fwd := &forward.Forwarder{Node: gw, Peer: "base-test"}

	req, _ := http.NewRequest(http.MethodGet, "http://base-test/v1/health", nil)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	resp, err := fwd.RoundTrip(req.WithContext(ctx))
	if err != nil {
		t.Fatalf("RoundTrip /v1/health: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: got %d want %d", resp.StatusCode, http.StatusOK)
	}

	body, _ := io.ReadAll(resp.Body)
	if string(body) != `{"message":"API is healthy.","code":200}` {
		t.Fatalf("body did not survive the bridge: %q", body)
	}
}

// TestBridgeForwardNilNode asserts the bridge is a safe no-op when the node is
// absent (mirrors the ZAP-disabled path): bridgeForward must not panic and
// must not touch HTTP behavior. A non-nil handler with a nil node must
// short-circuit on the node guard.
func TestBridgeForwardNilNode(t *testing.T) {
	p := &plugin{logger: luxlog.New("component", "zap-test")}
	p.bridgeForward(healthHandler()) // node is nil → must no-op, not panic
}

// TestBridgeForwardNilHandler asserts a present node with a nil handler also
// no-ops (ZAP up, but no HTTP surface to bridge).
func TestBridgeForwardNilHandler(t *testing.T) {
	n := zaplib.NewNode(zaplib.NodeConfig{NodeID: "nilhandler-test", Port: 0, NoDiscovery: true})
	p := &plugin{node: n, logger: luxlog.New("component", "zap-test")}
	p.bridgeForward(nil) // handler is nil → must no-op, not panic
}

func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve free port: %v", err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

func waitForPeer(t *testing.T, n *zaplib.Node, peer string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		for _, p := range n.Peers() {
			if p == peer {
				return
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("peer %q not connected within deadline (peers=%v)", peer, n.Peers())
}
