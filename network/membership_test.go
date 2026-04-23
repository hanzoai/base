// Copyright (c) 2025, Hanzo Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package network

import (
	"context"
	"sync"
	"testing"
	"time"
)

// fakeResolver is a test Resolver: hostname → addresses map. Safe for
// concurrent use; Set rewrites the table atomically.
type fakeResolver struct {
	mu    sync.Mutex
	table map[string][]string
}

func (f *fakeResolver) LookupHost(_ context.Context, host string) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	addrs, ok := f.table[host]
	if !ok {
		return nil, &hostNotFound{host: host}
	}
	return append([]string(nil), addrs...), nil
}

func (f *fakeResolver) Set(host string, addrs []string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.table == nil {
		f.table = map[string][]string{}
	}
	f.table[host] = addrs
}

type hostNotFound struct{ host string }

func (h *hostNotFound) Error() string { return "no such host: " + h.host }

// TestDNSMembershipSingleton: BASE_PEERS empty → Members() = [self].
func TestDNSMembershipSingleton(t *testing.T) {
	m := newDNSMembership(context.Background(), "liquid-bd-0", nil, &fakeResolver{}, time.Hour)
	defer m.Close()
	got := m.Members()
	if len(got) != 1 || got[0] != "liquid-bd-0" {
		t.Errorf("singleton: Members() = %v, want [liquid-bd-0]", got)
	}
}

// TestDNSMembershipHeadless: one headless-Service-style seed resolves
// to multiple IPs → one member per IP.
func TestDNSMembershipHeadless(t *testing.T) {
	r := &fakeResolver{}
	r.Set("bd.liquidity.svc.cluster.local", []string{"10.0.0.1", "10.0.0.2", "10.0.0.3"})
	m := newDNSMembership(
		context.Background(),
		"liquid-bd-0",
		[]string{"bd.liquidity.svc.cluster.local:9651"},
		r,
		time.Hour,
	)
	defer m.Close()

	got := m.Members()
	if len(got) != 4 { // 3 IPs + self
		t.Errorf("Members(): got %d, want 4", len(got))
	}
	// Port must be carried onto resolved addresses.
	for _, id := range got {
		if id == "liquid-bd-0" {
			continue
		}
		if !stringsHasSuffix(string(id), ":9651") {
			t.Errorf("member %q missing :9651 port", id)
		}
	}
}

// TestDNSMembershipCompose: three static compose service names, each
// resolving to one IP → 4 members (3 + self).
func TestDNSMembershipCompose(t *testing.T) {
	r := &fakeResolver{}
	r.Set("base-0", []string{"172.18.0.2"})
	r.Set("base-1", []string{"172.18.0.3"})
	r.Set("base-2", []string{"172.18.0.4"})

	m := newDNSMembership(
		context.Background(),
		"base-0",
		[]string{"base-0:9651", "base-1:9651", "base-2:9651"},
		r,
		time.Hour,
	)
	defer m.Close()

	got := m.Members()
	if len(got) != 4 {
		t.Errorf("Members(): got %d, want 4 (3 peers + self)", len(got))
	}
}

// TestDNSMembershipScaleUp: membership grows when DNS starts resolving
// new addresses. Simulates kubectl scale sts --replicas=3 landing.
func TestDNSMembershipScaleUp(t *testing.T) {
	r := &fakeResolver{}
	r.Set("bd.svc", []string{"10.0.0.1"})

	m := newDNSMembership(
		context.Background(),
		"liquid-bd-0",
		[]string{"bd.svc:9651"},
		r,
		10*time.Millisecond,
	)
	defer m.Close()

	initial := m.Members()
	if len(initial) != 2 {
		t.Fatalf("initial: got %d, want 2", len(initial))
	}

	watch := m.Watch(context.Background())
	<-watch // drain initial snapshot

	r.Set("bd.svc", []string{"10.0.0.1", "10.0.0.2", "10.0.0.3"})

	select {
	case snap := <-watch:
		if len(snap) != 4 {
			t.Errorf("after scale-up: got %d members, want 4", len(snap))
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("membership watcher did not fire on DNS change")
	}
}

// TestDNSMembershipScaleDown: membership shrinks when DNS stops
// resolving an address. Simulates pod termination.
func TestDNSMembershipScaleDown(t *testing.T) {
	r := &fakeResolver{}
	r.Set("bd.svc", []string{"10.0.0.1", "10.0.0.2", "10.0.0.3"})

	m := newDNSMembership(
		context.Background(),
		"liquid-bd-0",
		[]string{"bd.svc:9651"},
		r,
		10*time.Millisecond,
	)
	defer m.Close()

	if len(m.Members()) != 4 {
		t.Fatalf("initial: got %d, want 4", len(m.Members()))
	}

	watch := m.Watch(context.Background())
	<-watch // drain

	r.Set("bd.svc", []string{"10.0.0.1"}) // 2 pods terminated

	select {
	case snap := <-watch:
		if len(snap) != 2 {
			t.Errorf("after scale-down: got %d, want 2", len(snap))
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("membership watcher did not fire on DNS shrink")
	}
}

// TestStaticMembership: in-process test-only impl, fixed set plus self.
func TestStaticMembership(t *testing.T) {
	m := NewStaticMembership("a", []string{"b", "c"})
	got := m.Members()
	if len(got) != 3 {
		t.Errorf("Members(): got %d, want 3", len(got))
	}
}

func stringsHasSuffix(s, suffix string) bool {
	return len(s) >= len(suffix) && s[len(s)-len(suffix):] == suffix
}
