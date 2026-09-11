// Copyright (c) 2025, Hanzo Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package network

import (
	"context"
	"net"
	"strconv"
	"testing"
	"time"
)

// TestIsSelfPeer verifies that BASE_PEERS entries are correctly matched
// against the local NodeID even when the peer entry carries the full
// headless-Service FQDN + :port (which the operator emits).
//
// Regression: operator emits
//
//	BASE_PEERS=base-0.base-network.hanzo.svc.cluster.local:9999,...
//	BASE_NODE_ID=base-0
//
// Plain equality failed to skip self → transport dialed its own pod →
// luxfi/zap detected dup NodeID and closed → 3s reconnect loop burned
// CPU and logs without ever replicating a frame.
func TestIsSelfPeer(t *testing.T) {
	z := &zapTransport{self: "base-0"}

	cases := []struct {
		peer string
		self bool
	}{
		// Empty / exact match — trivially self.
		{"", true},
		{"base-0", true},

		// FQDN + port — the operator-emitted shape.
		{"base-0.base-network.hanzo.svc.cluster.local:9999", true},

		// Short hostname + port, no domain — also self.
		{"base-0:9999", true},

		// Different ordinal — not self.
		{"base-1.base-network.hanzo.svc.cluster.local:9999", false},
		{"base-2:9999", false},

		// Completely different name — not self.
		{"some-other-pod.some-other.svc:9999", false},
	}
	for _, tc := range cases {
		if got := z.isSelfPeer(tc.peer); got != tc.self {
			t.Errorf("isSelfPeer(%q) = %v, want %v", tc.peer, got, tc.self)
		}
	}
}

// TestStartListensWhereListenP2PSays starts the transport from the environment
// and dials it as a peer on another host would, on this machine's non-loopback
// address. With BASE_LISTEN_P2P on loopback nothing answers there; with its
// host left empty something does, which shows the probe can see an exposure at
// all.
func TestStartListensWhereListenP2PSays(t *testing.T) {
	lan := lanIPv4(t)

	start := func(t *testing.T, listen string) {
		t.Helper()
		t.Setenv("BASE_NETWORK", "quasar")
		t.Setenv("BASE_SHARD_KEY", "user_id")
		t.Setenv("BASE_NODE_ID", "bind-test")
		t.Setenv("BASE_PEERS", "")
		t.Setenv("BASE_LISTEN_P2P", listen)
		cfg, err := ConfigFromEnv()
		if err != nil {
			t.Fatalf("ConfigFromEnv: %v", err)
		}
		z := newZapTransport(cfg)
		if err := z.Start(context.Background(), func(Envelope) {}); err != nil {
			t.Fatalf("Start: %v", err)
		}
		t.Cleanup(func() { _ = z.Stop(context.Background()) })
	}

	t.Run("loopback", func(t *testing.T) {
		port := freePort(t)
		start(t, net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
		if !dials("127.0.0.1", port) {
			t.Fatal("nothing answers on loopback")
		}
		if dials(lan, port) {
			t.Fatalf("the peer listener answers on %s:%d with BASE_LISTEN_P2P on loopback", lan, port)
		}
	})

	t.Run("every interface", func(t *testing.T) {
		port := freePort(t)
		start(t, ":"+strconv.Itoa(port))
		if !dials(lan, port) {
			t.Fatalf("nothing answers on %s:%d with BASE_LISTEN_P2P on every interface", lan, port)
		}
	})
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

func dials(host string, port int) bool {
	c, err := net.DialTimeout("tcp", net.JoinHostPort(host, strconv.Itoa(port)), time.Second)
	if err != nil {
		return false
	}
	c.Close()
	return true
}

// lanIPv4 is an address of this machine that is not loopback — the one a peer
// on another host would dial.
func lanIPv4(t *testing.T) string {
	t.Helper()
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		t.Skipf("interface addresses: %v", err)
	}
	for _, a := range addrs {
		if ipn, ok := a.(*net.IPNet); ok {
			if ip := ipn.IP.To4(); ip != nil && !ip.IsLoopback() && !ip.IsLinkLocalUnicast() {
				return ip.String()
			}
		}
	}
	t.Skip("no non-loopback IPv4 address to dial")
	return ""
}
