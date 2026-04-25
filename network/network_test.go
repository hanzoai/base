package network

import (
	"context"
	"os"
	"testing"
)

func TestFromEnvStandaloneDefault(t *testing.T) {
	t.Setenv("BASE_NETWORK", "")
	n, err := FromEnv()
	if err != nil {
		t.Fatalf("FromEnv: %v", err)
	}
	if n.Enabled() {
		t.Fatal("expected standalone (Enabled == false)")
	}
	// All methods must be safe to call on the noop.
	if err := n.Start(context.Background()); err != nil {
		t.Errorf("Start: %v", err)
	}
	if err := n.InstallWALHook(nil, "shard"); err != nil {
		t.Errorf("InstallWALHook: %v", err)
	}
	if _, local := n.WriterFor("x"); !local {
		t.Error("standalone WriterFor should report local")
	}
	if got := n.MembersFor("x"); got != nil {
		t.Errorf("standalone MembersFor = %v, want nil", got)
	}
	if err := n.Stop(context.Background()); err != nil {
		t.Errorf("Stop: %v", err)
	}
}

func TestFromEnvInvalidMode(t *testing.T) {
	t.Setenv("BASE_NETWORK", "quorum-v7")
	if _, err := FromEnv(); err == nil {
		t.Error("expected error for unknown BASE_NETWORK value")
	}
}

func TestFromEnvQuasarMissingShardKey(t *testing.T) {
	t.Setenv("BASE_NETWORK", "quasar")
	os.Unsetenv("BASE_SHARD_KEY")
	t.Setenv("HOSTNAME", "pod-0")
	if _, err := FromEnv(); err == nil {
		t.Error("expected error: BASE_SHARD_KEY required when BASE_NETWORK=quasar")
	}
}

// TestReplicationAboveMembers: Replication=5 with only 3 available members
// is valid — target quorum, router returns what's available. Lets the
// same binary scale 1 → N without Config failing.
func TestReplicationAboveMembers(t *testing.T) {
	t.Setenv("BASE_NETWORK", "quasar")
	t.Setenv("BASE_SHARD_KEY", "user_id")
	t.Setenv("BASE_REPLICATION", "5")
	t.Setenv("BASE_PEERS", "b:9999,c:9999") // 2 peers + self = 3
	t.Setenv("HOSTNAME", "a")
	n, err := FromEnv()
	if err != nil {
		t.Fatalf("FromEnv: %v", err)
	}
	if !n.Enabled() {
		t.Fatal("expected Enabled")
	}
}

// TestSingletonCollapsesToStandalone: a 1/1 pod (no BASE_PEERS) never
// needs consensus or ZAP — same code path as BASE_NETWORK=standalone.
// No listener bound, no self-dial, no reconnect loop.
func TestSingletonCollapsesToStandalone(t *testing.T) {
	t.Setenv("BASE_NETWORK", "quasar")
	t.Setenv("BASE_SHARD_KEY", "user_id")
	t.Setenv("BASE_REPLICATION", "3")
	t.Setenv("BASE_PEERS", "") // self only
	t.Setenv("HOSTNAME", "a")
	n, err := FromEnv()
	if err != nil {
		t.Fatalf("singleton FromEnv: %v", err)
	}
	if n.Enabled() {
		t.Fatal("singleton must collapse to standalone (Enabled=false)")
	}
	if _, local := n.WriterFor("x"); !local {
		t.Error("singleton must report local=true for WriterFor")
	}
}

func TestFromEnvQuasarValid(t *testing.T) {
	t.Setenv("BASE_NETWORK", "quasar")
	t.Setenv("BASE_SHARD_KEY", "user_id")
	t.Setenv("BASE_REPLICATION", "3")
	t.Setenv("BASE_PEERS", "b:9999,c:9999")
	t.Setenv("HOSTNAME", "a")
	n, err := FromEnv()
	if err != nil {
		t.Fatalf("FromEnv: %v", err)
	}
	if !n.Enabled() {
		t.Fatal("expected Enabled")
	}
}

func TestInstallWALHookRejectsBadConn(t *testing.T) {
	cfg := Config{
		Enabled:     true,
		ShardKey:    "user_id",
		Replication: 1,
		NodeID:      "a",
		Role:        RoleValidator,
		Archive:     "off",
		ListenHTTP:  ":8090",
		ListenP2P:   ":9999",
	}
	n, err := NewWithTransport(cfg, nil)
	if err != nil {
		t.Fatalf("NewWithTransport: %v", err)
	}
	if err := n.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer n.Stop(context.Background())

	if err := n.InstallWALHook(42, "shard"); err == nil {
		t.Error("expected error when rawConn lacks commit-hook surface")
	}
}

// fakeHook is the minimal HookRegisterer implementation for testing; it
// captures the callback and lets the test fire commits synthetically.
type fakeHook struct{ cb func() int32 }

func (f *fakeHook) RegisterCommitHook(cb func() int32) { f.cb = cb }

func TestInstallWALHookRegistersCallback(t *testing.T) {
	cfg := Config{
		Enabled:     true,
		ShardKey:    "user_id",
		Replication: 1,
		NodeID:      "a",
		Role:        RoleValidator,
		Archive:     "off",
		ListenHTTP:  ":8090",
		ListenP2P:   ":9999",
	}
	n, err := NewWithTransport(cfg, nil)
	if err != nil {
		t.Fatalf("NewWithTransport: %v", err)
	}
	if err := n.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer n.Stop(context.Background())

	h := &fakeHook{}
	if err := n.InstallWALHook(h, "shard-a"); err != nil {
		t.Fatalf("InstallWALHook: %v", err)
	}
	if h.cb == nil {
		t.Fatal("hook did not register callback")
	}
	if rc := h.cb(); rc != 0 {
		t.Errorf("commit hook returned %d, want 0", rc)
	}
}
