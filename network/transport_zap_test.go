// Copyright (c) 2025, Hanzo Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package network

import "testing"

// TestIsSelfPeer verifies that BASE_PEERS entries are correctly matched
// against the local NodeID even when the peer entry carries the full
// headless-Service FQDN + :port (which the operator emits).
//
// Regression: operator emits
//
//	BASE_PEERS=liquid-bd-0.liquid-bd-network.liquidity.svc.cluster.local:9999,...
//	BASE_NODE_ID=liquid-bd-0
//
// Plain equality failed to skip self → transport dialed its own pod →
// luxfi/zap detected dup NodeID and closed → 3s reconnect loop burned
// CPU and logs without ever replicating a frame.
func TestIsSelfPeer(t *testing.T) {
	z := &zapTransport{self: "liquid-bd-0"}

	cases := []struct {
		peer string
		self bool
	}{
		// Empty / exact match — trivially self.
		{"", true},
		{"liquid-bd-0", true},

		// FQDN + port — the operator-emitted shape.
		{"liquid-bd-0.liquid-bd-network.liquidity.svc.cluster.local:9999", true},

		// Short hostname + port, no domain — also self.
		{"liquid-bd-0:9999", true},

		// Different ordinal — not self.
		{"liquid-bd-1.liquid-bd-network.liquidity.svc.cluster.local:9999", false},
		{"liquid-bd-2:9999", false},

		// Completely different name — not self.
		{"some-other-pod.some-other.svc:9999", false},
	}
	for _, tc := range cases {
		if got := z.isSelfPeer(tc.peer); got != tc.self {
			t.Errorf("isSelfPeer(%q) = %v, want %v", tc.peer, got, tc.self)
		}
	}
}
