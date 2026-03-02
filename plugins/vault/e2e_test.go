package vault

import (
	"crypto/rand"
	"fmt"
	"sync"
	"testing"
)

// ─── E2E: Full user lifecycle ────────────────────────────────────────────────
//
// Simulates: register → create vault → store data → sync across devices →
// anchor to chain → verify isolation → revoke device
//
// In production:
//   - "register" = WebAuthn passkey enrollment on I-Chain
//   - "unlock" = biometric assertion → K-Chain DEK unwrap
//   - "sync" = CRDT ops over ZAP between devices
//   - "anchor" = merkle root committed to I-Chain

func TestE2E_FullUserLifecycle(t *testing.T) {
	masterKey := make([]byte, 32)
	rand.Read(masterKey)

	v, err := Open(SDKConfig{
		DataDir:   t.TempDir(),
		MasterKEK: masterKey,
		OrgID:     "zoo-labs",
	})
	if err != nil {
		t.Fatal(err)
	}
	defer v.Close()

	// 1. User registers (passkey enrollment → DID created on I-Chain)
	session, err := v.OpenUser("user-did:lux:zoo:alice")
	if err != nil {
		t.Fatal(err)
	}

	// 2. Store private data (biometric unlock → DEK available)
	if err := session.Put("profile", []byte(`{"name":"Alice","email":"alice@zoo.ngo"}`)); err != nil {
		t.Fatal(err)
	}
	if err := session.Put("api_key", []byte("sk_live_abc123")); err != nil {
		t.Fatal(err)
	}
	if err := session.Put("preferences", []byte(`{"theme":"dark","lang":"en"}`)); err != nil {
		t.Fatal(err)
	}

	// 3. Verify reads
	profile, err := session.Get("profile")
	if err != nil {
		t.Fatal(err)
	}
	if string(profile) != `{"name":"Alice","email":"alice@zoo.ngo"}` {
		t.Fatalf("profile = %q", profile)
	}

	// 4. Anchor to chain
	receipt, err := session.Anchor()
	if err != nil {
		t.Fatal(err)
	}
	if receipt.OpCount != 3 {
		t.Fatalf("opcount = %d, want 3", receipt.OpCount)
	}
	if receipt.MerkleRoot == "" {
		t.Fatal("merkle root empty")
	}

	t.Logf("anchored: root=%s ops=%d user=%s", receipt.MerkleRoot, receipt.OpCount, receipt.UserID)
}

func TestE2E_MultiDeviceSync(t *testing.T) {
	masterKey := make([]byte, 32)
	rand.Read(masterKey)

	v, _ := Open(SDKConfig{
		DataDir:   t.TempDir(),
		MasterKEK: masterKey,
		OrgID:     "zoo-labs",
	})
	defer v.Close()

	// Device 1: laptop
	device1, _ := v.OpenUser("alice")
	device1.Put("doc", []byte("v1: written on laptop"))

	// Device 2: phone (same user, same DEK)
	// In production: phone does WebAuthn assertion → gets same DEK from K-Chain
	device2Session := &Session{
		userID:  "alice-phone",
		orgID:   "zoo-labs",
		dek:     device1.dek, // same DEK (derived from same user)
		store:   make(map[string][]byte),
		oplog:   make([]Op, 0),
		version: make(map[string]uint64),
	}

	// Sync: device1 pushes ops, device2 merges
	device1.mu.RLock()
	ops := make([]Op, len(device1.oplog))
	copy(ops, device1.oplog)
	device1.mu.RUnlock()

	device2Session.Merge(ops)

	// Device 2 can now read device 1's data (same DEK)
	got, err := device2Session.Get("doc")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "v1: written on laptop" {
		t.Fatalf("device2 got %q", got)
	}

	// Device 2 writes, syncs back
	device2Session.Put("doc", []byte("v2: edited on phone"))
	device2Session.mu.RLock()
	phoneOps := make([]Op, len(device2Session.oplog))
	copy(phoneOps, device2Session.oplog)
	device2Session.mu.RUnlock()

	device1.Merge(phoneOps)

	got2, _ := device1.Get("doc")
	if string(got2) != "v2: edited on phone" {
		t.Fatalf("device1 after sync got %q", got2)
	}
}

func TestE2E_OrgMultiTenant(t *testing.T) {
	masterKey := make([]byte, 32)
	rand.Read(masterKey)

	// Two orgs, same infrastructure
	orgA, _ := Open(SDKConfig{DataDir: t.TempDir(), MasterKEK: masterKey, OrgID: "org-a"})
	orgB, _ := Open(SDKConfig{DataDir: t.TempDir(), MasterKEK: masterKey, OrgID: "org-b"})
	defer orgA.Close()
	defer orgB.Close()

	alice_a, _ := orgA.OpenUser("alice")
	alice_b, _ := orgB.OpenUser("alice")

	alice_a.Put("secret", []byte("org-a-secret"))
	alice_b.Put("secret", []byte("org-b-secret"))

	// Same user, different orgs → different DEKs → different data
	val_a, _ := alice_a.Get("secret")
	val_b, _ := alice_b.Get("secret")

	if string(val_a) != "org-a-secret" || string(val_b) != "org-b-secret" {
		t.Fatal("org isolation broken")
	}

	// Cross-org decryption must fail
	alice_a.mu.RLock()
	ct := alice_a.store["secret"]
	alice_a.mu.RUnlock()

	bobShard := &UserShard{DEK: alice_b.dek}
	_, err := bobShard.Decrypt(ct)
	if err == nil {
		t.Fatal("cross-org decrypt should fail")
	}
}

func TestE2E_ConcurrentAccess(t *testing.T) {
	masterKey := make([]byte, 32)
	rand.Read(masterKey)

	v, _ := Open(SDKConfig{
		DataDir:   t.TempDir(),
		MasterKEK: masterKey,
		OrgID:     "zoo-labs",
	})
	defer v.Close()

	session, _ := v.OpenUser("alice")

	// 50 concurrent writers
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			key := fmt.Sprintf("key-%d", n)
			val := fmt.Sprintf("value-%d", n)
			session.Put(key, []byte(val))
		}(i)
	}
	wg.Wait()

	// All 50 should be readable
	for i := 0; i < 50; i++ {
		key := fmt.Sprintf("key-%d", i)
		val, err := session.Get(key)
		if err != nil {
			t.Fatalf("missing key-%d: %v", i, err)
		}
		if string(val) != fmt.Sprintf("value-%d", i) {
			t.Fatalf("key-%d = %q", i, val)
		}
	}

	// Oplog should have exactly 50 entries
	session.mu.RLock()
	opCount := len(session.oplog)
	session.mu.RUnlock()
	if opCount != 50 {
		t.Fatalf("oplog = %d, want 50", opCount)
	}
}

func TestE2E_AnchorDeterministic(t *testing.T) {
	masterKey := make([]byte, 32)
	rand.Read(masterKey)

	v, _ := Open(SDKConfig{
		DataDir:   t.TempDir(),
		MasterKEK: masterKey,
		OrgID:     "zoo-labs",
	})
	defer v.Close()

	session, _ := v.OpenUser("alice")
	session.Put("a", []byte("1"))
	session.Put("b", []byte("2"))

	r1, _ := session.Anchor()
	r2, _ := session.Anchor()

	// Same state → same merkle root
	if r1.MerkleRoot != r2.MerkleRoot {
		t.Fatalf("anchor not deterministic: %s != %s", r1.MerkleRoot, r2.MerkleRoot)
	}

	// New write → different root
	session.Put("c", []byte("3"))
	r3, _ := session.Anchor()
	if r3.MerkleRoot == r1.MerkleRoot {
		t.Fatal("anchor should change after new write")
	}
}

func TestE2E_DeviceRevocation(t *testing.T) {
	masterKey := make([]byte, 32)
	rand.Read(masterKey)

	v, _ := Open(SDKConfig{
		DataDir:   t.TempDir(),
		MasterKEK: masterKey,
		OrgID:     "zoo-labs",
	})
	defer v.Close()

	session, _ := v.OpenUser("alice")
	session.Put("secret", []byte("sensitive"))

	// Simulate device revocation: close session, zero DEK
	session.close()

	// After close, reads should fail
	_, err := session.Get("secret")
	if err == nil {
		t.Fatal("reads should fail after session close (device revoked)")
	}
}
