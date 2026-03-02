// Copyright (C) 2020-2026, Hanzo AI Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package vault

import (
	"bytes"
	"crypto/rand"
	"path/filepath"
	"sync"
	"testing"
)

// ─── Sync Provider ──────────────────────────────────────────────────────────

func TestLocalSyncProvider_PushPull(t *testing.T) {
	dir := t.TempDir()
	sp, err := NewLocalSyncProvider(dir)
	if err != nil {
		t.Fatal(err)
	}

	ops := []Op{
		{Seq: 1, NodeID: "d1", Key: "a", Value: []byte("v1"), Time: 100},
		{Seq: 2, NodeID: "d1", Key: "b", Value: []byte("v2"), Time: 200},
		{Seq: 3, NodeID: "d1", Key: "c", Value: []byte("v3"), Time: 300},
	}

	if err := sp.Push("vault-1", ops); err != nil {
		t.Fatal(err)
	}

	// Pull all ops (since=0).
	got, err := sp.Pull("vault-1", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d ops, want 3", len(got))
	}

	// Pull only ops after seq 1.
	got, err = sp.Pull("vault-1", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d ops, want 2 (since=1)", len(got))
	}
	if got[0].Key != "b" || got[1].Key != "c" {
		t.Fatalf("unexpected ops: %v", got)
	}

	// Pull from empty vault returns nothing.
	got, err = sp.Pull("vault-2", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("expected 0 ops from empty vault, got %d", len(got))
	}
}

func TestLocalSyncProvider_Subscribe(t *testing.T) {
	dir := t.TempDir()
	sp, err := NewLocalSyncProvider(dir)
	if err != nil {
		t.Fatal(err)
	}

	var received []Op
	var mu sync.Mutex

	if err := sp.Subscribe("vault-1", func(ops []Op) {
		mu.Lock()
		received = append(received, ops...)
		mu.Unlock()
	}); err != nil {
		t.Fatal(err)
	}

	ops := []Op{
		{Seq: 1, NodeID: "d1", Key: "x", Value: []byte("y")},
	}
	sp.Push("vault-1", ops)

	mu.Lock()
	defer mu.Unlock()
	if len(received) != 1 {
		t.Fatalf("subscriber got %d ops, want 1", len(received))
	}
	if received[0].Key != "x" {
		t.Fatalf("subscriber got key %q, want x", received[0].Key)
	}
}

// ─── Storage Provider ───────────────────────────────────────────────────────

func TestLocalStorageProvider_PutGetSnapshot(t *testing.T) {
	dir := t.TempDir()
	sp, err := NewLocalStorageProvider(dir)
	if err != nil {
		t.Fatal(err)
	}

	data := []byte("encrypted-sqlite-snapshot-data")
	hash, err := sp.PutSnapshot("vault-1", data)
	if err != nil {
		t.Fatal(err)
	}
	if hash == "" {
		t.Fatal("hash should not be empty")
	}

	got, err := sp.GetSnapshot("vault-1", hash)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, data) {
		t.Fatalf("got %q, want %q", got, data)
	}

	// Get with wrong hash fails.
	_, err = sp.GetSnapshot("vault-1", "badhash")
	if err == nil {
		t.Fatal("expected error for bad hash")
	}
}

func TestLocalStorageProvider_ContentAddressed(t *testing.T) {
	dir := t.TempDir()
	sp, err := NewLocalStorageProvider(dir)
	if err != nil {
		t.Fatal(err)
	}

	data := []byte("same-content")

	// Same content → same hash.
	hash1, err := sp.PutBlob("key1", data)
	if err != nil {
		t.Fatal(err)
	}
	hash2, err := sp.PutBlob("key2", data)
	if err != nil {
		t.Fatal(err)
	}
	if hash1 != hash2 {
		t.Fatalf("same content produced different hashes: %s != %s", hash1, hash2)
	}

	// Different content → different hash.
	hash3, err := sp.PutBlob("key3", []byte("different"))
	if err != nil {
		t.Fatal(err)
	}
	if hash1 == hash3 {
		t.Fatal("different content should produce different hashes")
	}

	// Retrieve by hash.
	got, err := sp.GetBlob(hash1)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, data) {
		t.Fatalf("got %q, want %q", got, data)
	}
}

func TestLocalStorageProvider_PutGetBlob(t *testing.T) {
	dir := t.TempDir()
	sp, err := NewLocalStorageProvider(dir)
	if err != nil {
		t.Fatal(err)
	}

	// Store a large-ish blob.
	blob := make([]byte, 4096)
	rand.Read(blob)

	hash, err := sp.PutBlob("big-blob", blob)
	if err != nil {
		t.Fatal(err)
	}

	got, err := sp.GetBlob(hash)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, blob) {
		t.Fatal("blob round-trip failed")
	}
}

// ─── Recovery Provider ──────────────────────────────────────────────────────

func TestLocalRecoveryProvider_StoreAndFetch(t *testing.T) {
	dir := t.TempDir()
	rp, err := NewLocalRecoveryProvider(dir)
	if err != nil {
		t.Fatal(err)
	}

	shares := [][]byte{
		[]byte("share-0-encrypted"),
		[]byte("share-1-encrypted"),
		[]byte("share-2-encrypted"),
	}

	for i, s := range shares {
		if err := rp.StoreShare("alice", i, s); err != nil {
			t.Fatalf("store share %d: %v", i, err)
		}
	}

	got, err := rp.FetchShares("alice", []int{0, 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d shares, want 2", len(got))
	}
	if !bytes.Equal(got[0], shares[0]) {
		t.Fatalf("share 0: got %q, want %q", got[0], shares[0])
	}
	if !bytes.Equal(got[1], shares[2]) {
		t.Fatalf("share 2: got %q, want %q", got[1], shares[2])
	}

	// Fetch non-existent share fails.
	_, err = rp.FetchShares("alice", []int{99})
	if err == nil {
		t.Fatal("expected error for missing share")
	}
}

// ─── Export/Import ──────────────────────────────────────────────────────────

func TestExportImport_RoundTrip(t *testing.T) {
	key := testMasterKey()
	v, err := Open(SDKConfig{
		DataDir:   t.TempDir(),
		MasterKEK: key,
		OrgID:     "test-org",
	})
	if err != nil {
		t.Fatal(err)
	}
	defer v.Close()

	session, err := v.OpenUser("alice")
	if err != nil {
		t.Fatal(err)
	}

	// Write some data.
	session.Put("profile", []byte(`{"name":"Alice"}`))
	session.Put("settings", []byte(`{"theme":"dark"}`))

	// Export.
	bundle, err := ExportVault(session)
	if err != nil {
		t.Fatal(err)
	}

	// Import with the same DEK.
	session.mu.RLock()
	dek := make([]byte, 32)
	copy(dek, session.dek)
	session.mu.RUnlock()

	imported, err := ImportVault(bundle, dek)
	if err != nil {
		t.Fatal(err)
	}

	// Verify data survives round-trip.
	profile, err := imported.Get("profile")
	if err != nil {
		t.Fatal(err)
	}
	if string(profile) != `{"name":"Alice"}` {
		t.Fatalf("profile = %q", profile)
	}

	settings, err := imported.Get("settings")
	if err != nil {
		t.Fatal(err)
	}
	if string(settings) != `{"theme":"dark"}` {
		t.Fatalf("settings = %q", settings)
	}

	// Oplog is preserved.
	imported.mu.RLock()
	opCount := len(imported.oplog)
	imported.mu.RUnlock()
	if opCount != 2 {
		t.Fatalf("oplog count = %d, want 2", opCount)
	}
}

func TestExportImport_WrongDEKFails(t *testing.T) {
	key := testMasterKey()
	v, _ := Open(SDKConfig{
		DataDir:   t.TempDir(),
		MasterKEK: key,
		OrgID:     "test-org",
	})
	defer v.Close()

	session, _ := v.OpenUser("alice")
	session.Put("secret", []byte("classified"))

	bundle, err := ExportVault(session)
	if err != nil {
		t.Fatal(err)
	}

	// Import with wrong DEK.
	wrongDEK := testMasterKey()
	imported, err := ImportVault(bundle, wrongDEK)
	if err != nil {
		t.Fatal(err) // import itself succeeds (it's just JSON)
	}

	// But decryption with the wrong key fails.
	_, err = imported.Get("secret")
	if err == nil {
		t.Fatal("decryption with wrong DEK should fail")
	}
}

func TestExportVault_ClosedSessionFails(t *testing.T) {
	key := testMasterKey()
	v, _ := Open(SDKConfig{
		DataDir:   t.TempDir(),
		MasterKEK: key,
		OrgID:     "test-org",
	})
	defer v.Close()

	session, _ := v.OpenUser("alice")
	session.close()

	_, err := ExportVault(session)
	if err == nil {
		t.Fatal("export of closed session should fail")
	}
}

func TestImportVault_BadDEKLength(t *testing.T) {
	_, err := ImportVault([]byte(`{}`), []byte("short"))
	if err == nil {
		t.Fatal("expected error for short DEK")
	}
}

// ─── Metering ───────────────────────────────────────────────────────────────

func TestUsageMetering_CountsOps(t *testing.T) {
	m := NewMeter()

	m.RecordPut("v1")
	m.RecordPut("v1")
	m.RecordPut("v1")
	m.RecordGet("v1")
	m.RecordGet("v1")
	m.RecordSync("v1")
	m.RecordAnchor("v1")

	u := m.GetUsage("v1")
	if u.VaultID != "v1" {
		t.Fatalf("vaultID = %q", u.VaultID)
	}
	if u.Puts != 3 {
		t.Fatalf("puts = %d, want 3", u.Puts)
	}
	if u.Gets != 2 {
		t.Fatalf("gets = %d, want 2", u.Gets)
	}
	if u.Syncs != 1 {
		t.Fatalf("syncs = %d, want 1", u.Syncs)
	}
	if u.Anchors != 1 {
		t.Fatalf("anchors = %d, want 1", u.Anchors)
	}
}

func TestUsageMetering_IsolatesVaults(t *testing.T) {
	m := NewMeter()

	m.RecordPut("v1")
	m.RecordPut("v2")
	m.RecordPut("v2")

	u1 := m.GetUsage("v1")
	u2 := m.GetUsage("v2")

	if u1.Puts != 1 {
		t.Fatalf("v1 puts = %d, want 1", u1.Puts)
	}
	if u2.Puts != 2 {
		t.Fatalf("v2 puts = %d, want 2", u2.Puts)
	}
}

func TestUsageMetering_Reset(t *testing.T) {
	m := NewMeter()

	m.RecordPut("v1")
	m.RecordGet("v1")
	m.Reset("v1")

	u := m.GetUsage("v1")
	if u.Puts != 0 || u.Gets != 0 {
		t.Fatalf("expected zeroes after reset, got puts=%d gets=%d", u.Puts, u.Gets)
	}
}

func TestUsageMetering_Concurrent(t *testing.T) {
	m := NewMeter()
	var wg sync.WaitGroup

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			m.RecordPut("v1")
		}()
	}
	wg.Wait()

	u := m.GetUsage("v1")
	if u.Puts != 100 {
		t.Fatalf("puts = %d, want 100", u.Puts)
	}
}

// ─── Provider Registry ──────────────────────────────────────────────────────

func TestProviderRegistry_Register(t *testing.T) {
	reg := NewProviderRegistry()
	dir := t.TempDir()

	sp, err := NewLocalSyncProvider(filepath.Join(dir, "sync"))
	if err != nil {
		t.Fatal(err)
	}
	st, err := NewLocalStorageProvider(filepath.Join(dir, "storage"))
	if err != nil {
		t.Fatal(err)
	}
	rp, err := NewLocalRecoveryProvider(filepath.Join(dir, "recovery"))
	if err != nil {
		t.Fatal(err)
	}

	reg.RegisterSync("local", sp)
	reg.RegisterStorage("local", st)
	reg.RegisterRecovery("local", rp)

	// Retrieve registered providers.
	gotSync, err := reg.GetSync("local")
	if err != nil {
		t.Fatal(err)
	}
	if gotSync != sp {
		t.Fatal("sync provider mismatch")
	}

	gotStorage, err := reg.GetStorage("local")
	if err != nil {
		t.Fatal(err)
	}
	if gotStorage != st {
		t.Fatal("storage provider mismatch")
	}

	gotRecovery, err := reg.GetRecovery("local")
	if err != nil {
		t.Fatal(err)
	}
	if gotRecovery != rp {
		t.Fatal("recovery provider mismatch")
	}

	// Unknown provider returns error.
	_, err = reg.GetSync("missing")
	if err == nil {
		t.Fatal("expected error for missing sync provider")
	}
	_, err = reg.GetStorage("missing")
	if err == nil {
		t.Fatal("expected error for missing storage provider")
	}
	_, err = reg.GetRecovery("missing")
	if err == nil {
		t.Fatal("expected error for missing recovery provider")
	}
}
