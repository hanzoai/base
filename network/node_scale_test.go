// Copyright (c) 2025, Hanzo Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.
//
// End-to-end: node + Membership + router. Covers the scale-1→N→1
// contract that every Base service needs:
//
//   - singleton start → WriterFor returns local
//   - Membership change → router rebuilds → WriterFor may flip
//   - scale-down → member drops out → router recomputes
//
// This is the integration story for consumers (BD, ATS, TA, IAM).
// If these tests pass, `kubectl scale` (or `docker compose scale`)
// works without pod restart.

package network

import (
	"context"
	"testing"
	"time"
)

// TestNodeScaleUp: node starts singleton, operator scales to 3,
// router picks up new members, WriterFor flips appropriately.
func TestNodeScaleUp(t *testing.T) {
	cfg := Config{
		Enabled:     true,
		ShardKey:    "user_id",
		Replication: 3,
		NodeID:      "a",
		Peers:       []string{"b", "c"},
		Role:        RoleValidator,
		Archive:     "off",
		ListenHTTP:  ":8090",
		ListenP2P:   ":9999",
	}
	n, err := newNodeWithTransport(cfg, &nopTransport{})
	if err != nil {
		t.Fatalf("newNodeWithTransport: %v", err)
	}

	mem := NewStaticMembership("a", []string{"a"})
	n.SetMembership(mem)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := n.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer n.Stop(context.Background())

	// Singleton: only self is member → WriterFor(x) is local.
	waitForMemberCount(t, n, 1)
	_, local := n.WriterFor("user-7")
	if !local {
		t.Errorf("singleton WriterFor(user-7): expected local=true")
	}

	// Scale to 3: self + b + c.
	mem.Set([]string{"b", "c"})

	waitForMemberCount(t, n, 3)

	// Router now knows 3 members. WriterFor is deterministic via
	// rendezvous hash — some shard IDs land local, others don't.
	// We just assert the set is stable and of size 3.
	ms := n.MembersFor("user-7")
	if len(ms) != 3 {
		t.Errorf("MembersFor after scale-up: got %d, want 3", len(ms))
	}
}

// TestNodeScaleDown: node starts with 3 peers, one drops out,
// router rebuilds without that member.
func TestNodeScaleDown(t *testing.T) {
	cfg := Config{
		Enabled:     true,
		ShardKey:    "user_id",
		Replication: 3,
		NodeID:      "a",
		Peers:       []string{"b", "c"},
		Role:        RoleValidator,
		Archive:     "off",
		ListenHTTP:  ":8090",
		ListenP2P:   ":9999",
	}
	n, err := newNodeWithTransport(cfg, &nopTransport{})
	if err != nil {
		t.Fatalf("newNodeWithTransport: %v", err)
	}

	mem := NewStaticMembership("a", []string{"b", "c"})
	n.SetMembership(mem)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := n.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer n.Stop(context.Background())

	waitForMemberCount(t, n, 3)

	// c drops out (pod terminated).
	mem.Set([]string{"b"})

	waitForMemberCount(t, n, 2)
	ms := n.MembersFor("user-42")
	if len(ms) > 2 {
		t.Errorf("MembersFor after scale-down: got %d, want <= 2", len(ms))
	}
	for _, m := range ms {
		if m == "c" {
			t.Errorf("scaled-down member %q still in MembersFor", m)
		}
	}
}

// TestNodeScaleDownToOne: collapse all the way back to singleton.
// Router must have exactly self and WriterFor returns local for
// every shard.
func TestNodeScaleDownToOne(t *testing.T) {
	cfg := Config{
		Enabled:     true,
		ShardKey:    "user_id",
		Replication: 3,
		NodeID:      "a",
		Peers:       []string{"b", "c"},
		Role:        RoleValidator,
		Archive:     "off",
		ListenHTTP:  ":8090",
		ListenP2P:   ":9999",
	}
	n, err := newNodeWithTransport(cfg, &nopTransport{})
	if err != nil {
		t.Fatalf("newNodeWithTransport: %v", err)
	}

	mem := NewStaticMembership("a", []string{"b", "c"})
	n.SetMembership(mem)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := n.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer n.Stop(context.Background())

	waitForMemberCount(t, n, 3)

	// All peers drop out (cluster scaled to 1 during low traffic).
	mem.Set(nil)

	waitForMemberCount(t, n, 1)

	for _, shard := range []string{"user-1", "user-2", "user-99", "org-7"} {
		_, local := n.WriterFor(shard)
		if !local {
			t.Errorf("WriterFor(%q) after scale-to-1: expected local=true", shard)
		}
	}
}

// TestNodeMembershipNotSet: node without a Membership still works
// (static Peers list from Config). Back-compat for any call site
// that doesn't wire a Membership.
func TestNodeMembershipNotSet(t *testing.T) {
	cfg := Config{
		Enabled:     true,
		ShardKey:    "user_id",
		Replication: 3,
		NodeID:      "a",
		Peers:       []string{"b", "c"},
		Role:        RoleValidator,
		Archive:     "off",
		ListenHTTP:  ":8090",
		ListenP2P:   ":9999",
	}
	n, err := newNodeWithTransport(cfg, &nopTransport{})
	if err != nil {
		t.Fatalf("newNodeWithTransport: %v", err)
	}
	// Deliberately NOT calling SetMembership.

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := n.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer n.Stop(context.Background())

	// Static Peers gave us self + b + c = 3.
	ms := n.MembersFor("x")
	if len(ms) != 3 {
		t.Errorf("MembersFor without Membership: got %d, want 3", len(ms))
	}
}

// waitForMemberCount polls n.MembersFor until the count matches,
// bounded by a 500ms deadline. Membership updates propagate through
// a channel; this keeps tests non-flaky without hard-coded sleeps.
func waitForMemberCount(t *testing.T, n *node, want int) {
	t.Helper()
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if len(n.MembersFor("probe")) == want {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("waitForMemberCount: got %d, want %d", len(n.MembersFor("probe")), want)
}
