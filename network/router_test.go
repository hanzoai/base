package network

import "testing"

func TestRouterDeterministic(t *testing.T) {
	members := []NodeID{"a", "b", "c", "d", "e"}
	r1 := newRouter(members, 3)
	r2 := newRouter(append([]NodeID{"e", "b"}, "a", "c", "d"), 3)

	for _, shard := range []string{"u1", "u2", "u3", "u4", "u5"} {
		a := r1.membersFor(shard)
		b := r2.membersFor(shard)
		if len(a) != len(b) {
			t.Fatalf("shard %s: length mismatch %v vs %v", shard, a, b)
		}
		for i := range a {
			if a[i] != b[i] {
				t.Fatalf("shard %s: order mismatch at %d: %v vs %v", shard, i, a, b)
			}
		}
	}
}

func TestRouterReplicationCapped(t *testing.T) {
	r := newRouter([]NodeID{"a", "b"}, 5) // replication > members
	ms := r.membersFor("x")
	if len(ms) != 2 {
		t.Errorf("len(membersFor) = %d, want 2 (capped at member count)", len(ms))
	}
}

func TestRouterEmptyMembers(t *testing.T) {
	r := newRouter(nil, 3)
	if r.ownerOf("x") != "" {
		t.Error("owner of empty ring should be \"\"")
	}
	if len(r.membersFor("x")) != 0 {
		t.Error("members of empty ring should be empty")
	}
}

func TestRouterOwnerIsFirstMember(t *testing.T) {
	r := newRouter([]NodeID{"a", "b", "c"}, 3)
	for _, shard := range []string{"q", "r", "s"} {
		ms := r.membersFor(shard)
		if r.ownerOf(shard) != ms[0] {
			t.Errorf("ownerOf(%s) = %q but membersFor[0] = %q", shard, r.ownerOf(shard), ms[0])
		}
	}
}

func TestRouterBalancedEnough(t *testing.T) {
	// 5 members, 1000 shards. Owner-counts should be within a reasonable band.
	members := []NodeID{"a", "b", "c", "d", "e"}
	r := newRouter(members, 1)

	counts := map[NodeID]int{}
	for i := 0; i < 1000; i++ {
		counts[r.ownerOf(shardName(i))]++
	}
	// Expect each in [150, 250] (perfect is 200). Rendezvous hash bias is small.
	for _, m := range members {
		if c := counts[m]; c < 120 || c > 280 {
			t.Errorf("member %s: %d shards, out of balance band", m, c)
		}
	}
}

func shardName(i int) string {
	const letters = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := [4]byte{
		letters[(i>>12)%len(letters)],
		letters[(i>>8)%len(letters)],
		letters[(i>>4)%len(letters)],
		letters[i%len(letters)],
	}
	return string(b[:])
}

func TestQuorum(t *testing.T) {
	cases := []struct {
		n    int
		want int
	}{
		{0, 1},
		{1, 1},
		{2, 2},
		{3, 2},
		{4, 3},
		{5, 3},
		{7, 4},
	}
	for _, c := range cases {
		if got := quorum(c.n); got != c.want {
			t.Errorf("quorum(%d) = %d, want %d", c.n, got, c.want)
		}
	}
}
