package vault

import (
	"crypto/rand"
	"testing"
)

func testMasterKey() []byte {
	key := make([]byte, 32)
	rand.Read(key)
	return key
}

func TestOpenAndClose(t *testing.T) {
	v, err := Open(SDKConfig{
		DataDir:   t.TempDir(),
		MasterKEK: testMasterKey(),
		OrgID:     "test-org",
	})
	if err != nil {
		t.Fatal(err)
	}
	v.Close()
}

func TestOpenUserDerivesDEK(t *testing.T) {
	v, _ := Open(SDKConfig{
		DataDir:   t.TempDir(),
		MasterKEK: testMasterKey(),
		OrgID:     "test-org",
	})
	defer v.Close()

	s1, err := v.OpenUser("alice")
	if err != nil {
		t.Fatal(err)
	}
	s2, err := v.OpenUser("bob")
	if err != nil {
		t.Fatal(err)
	}

	// Different users get different DEKs.
	if string(s1.dek) == string(s2.dek) {
		t.Fatal("alice and bob should have different DEKs")
	}

	// Same user returns same session.
	s1again, _ := v.OpenUser("alice")
	if s1 != s1again {
		t.Fatal("same user should return same session")
	}
}

func TestPutGetRoundTrip(t *testing.T) {
	v, _ := Open(SDKConfig{
		DataDir:   t.TempDir(),
		MasterKEK: testMasterKey(),
		OrgID:     "test-org",
	})
	defer v.Close()

	s, _ := v.OpenUser("alice")

	plaintext := []byte("hello world")
	if err := s.Put("greeting", plaintext); err != nil {
		t.Fatal(err)
	}

	got, err := s.Get("greeting")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(plaintext) {
		t.Fatalf("got %q, want %q", got, plaintext)
	}
}

func TestGetMissingKey(t *testing.T) {
	v, _ := Open(SDKConfig{
		DataDir:   t.TempDir(),
		MasterKEK: testMasterKey(),
		OrgID:     "test-org",
	})
	defer v.Close()

	s, _ := v.OpenUser("alice")
	_, err := s.Get("nonexistent")
	if err == nil {
		t.Fatal("expected error for missing key")
	}
}

func TestDelete(t *testing.T) {
	v, _ := Open(SDKConfig{
		DataDir:   t.TempDir(),
		MasterKEK: testMasterKey(),
		OrgID:     "test-org",
	})
	defer v.Close()

	s, _ := v.OpenUser("alice")
	s.Put("key", []byte("value"))
	s.Delete("key")

	_, err := s.Get("key")
	if err == nil {
		t.Fatal("expected error after delete")
	}
}

func TestCrossUserIsolation(t *testing.T) {
	v, _ := Open(SDKConfig{
		DataDir:   t.TempDir(),
		MasterKEK: testMasterKey(),
		OrgID:     "test-org",
	})
	defer v.Close()

	alice, _ := v.OpenUser("alice")
	bob, _ := v.OpenUser("bob")

	alice.Put("secret", []byte("alice-data"))
	bob.Put("secret", []byte("bob-data"))

	aliceVal, _ := alice.Get("secret")
	bobVal, _ := bob.Get("secret")

	if string(aliceVal) == string(bobVal) {
		t.Fatal("alice and bob should have independent data")
	}
	if string(aliceVal) != "alice-data" {
		t.Fatalf("alice got %q", aliceVal)
	}
	if string(bobVal) != "bob-data" {
		t.Fatalf("bob got %q", bobVal)
	}
}

func TestCrossUserCannotDecrypt(t *testing.T) {
	v, _ := Open(SDKConfig{
		DataDir:   t.TempDir(),
		MasterKEK: testMasterKey(),
		OrgID:     "test-org",
	})
	defer v.Close()

	alice, _ := v.OpenUser("alice")
	bob, _ := v.OpenUser("bob")

	alice.Put("secret", []byte("classified"))

	// Get alice's raw ciphertext.
	alice.mu.RLock()
	ct := alice.store["secret"]
	alice.mu.RUnlock()

	// Bob's DEK should fail to decrypt alice's ciphertext.
	bobShard := &UserShard{DEK: bob.dek}
	_, err := bobShard.Decrypt(ct)
	if err == nil {
		t.Fatal("bob should not be able to decrypt alice's data")
	}
}

func TestAnchor(t *testing.T) {
	v, _ := Open(SDKConfig{
		DataDir:   t.TempDir(),
		MasterKEK: testMasterKey(),
		OrgID:     "test-org",
	})
	defer v.Close()

	s, _ := v.OpenUser("alice")
	s.Put("a", []byte("1"))
	s.Put("b", []byte("2"))

	receipt, err := s.Anchor()
	if err != nil {
		t.Fatal(err)
	}
	if receipt.MerkleRoot == "" {
		t.Fatal("merkle root should not be empty")
	}
	if receipt.OpCount != 2 {
		t.Fatalf("opcount = %d, want 2", receipt.OpCount)
	}
	if receipt.UserID != "alice" {
		t.Fatalf("user = %q, want alice", receipt.UserID)
	}
}

func TestMergeRemoteOps(t *testing.T) {
	v, _ := Open(SDKConfig{
		DataDir:   t.TempDir(),
		MasterKEK: testMasterKey(),
		OrgID:     "test-org",
	})
	defer v.Close()

	s, _ := v.OpenUser("alice")

	// Simulate remote ops from another device.
	shard := &UserShard{DEK: s.dek}
	encrypted, _ := shard.Encrypt([]byte("remote-value"))

	remoteOps := []Op{
		{Seq: 1, NodeID: "device-2", Key: "synced", Value: encrypted, Time: 1000},
	}
	s.Merge(remoteOps)

	got, err := s.Get("synced")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "remote-value" {
		t.Fatalf("got %q, want remote-value", got)
	}
}

func TestMergeSkipsDuplicateOps(t *testing.T) {
	v, _ := Open(SDKConfig{
		DataDir:   t.TempDir(),
		MasterKEK: testMasterKey(),
		OrgID:     "test-org",
	})
	defer v.Close()

	s, _ := v.OpenUser("alice")

	shard := &UserShard{DEK: s.dek}
	enc1, _ := shard.Encrypt([]byte("first"))
	enc2, _ := shard.Encrypt([]byte("second"))

	s.Merge([]Op{{Seq: 1, NodeID: "d2", Key: "k", Value: enc1}})
	s.Merge([]Op{{Seq: 1, NodeID: "d2", Key: "k", Value: enc2}}) // duplicate seq — should skip

	got, _ := s.Get("k")
	if string(got) != "first" {
		t.Fatalf("got %q, want first (duplicate should be skipped)", got)
	}
}

func TestOrgIsolation(t *testing.T) {
	key := testMasterKey()

	v1, _ := Open(SDKConfig{DataDir: t.TempDir(), MasterKEK: key, OrgID: "org-a"})
	v2, _ := Open(SDKConfig{DataDir: t.TempDir(), MasterKEK: key, OrgID: "org-b"})
	defer v1.Close()
	defer v2.Close()

	s1, _ := v1.OpenUser("alice")
	s2, _ := v2.OpenUser("alice")

	// Same user in different orgs gets different DEKs.
	if string(s1.dek) == string(s2.dek) {
		t.Fatal("same user in different orgs should have different DEKs")
	}
}

func TestInvalidMasterKey(t *testing.T) {
	_, err := Open(SDKConfig{
		DataDir:   t.TempDir(),
		MasterKEK: []byte("too-short"),
		OrgID:     "test-org",
	})
	if err == nil {
		t.Fatal("expected error for short master key")
	}
}
