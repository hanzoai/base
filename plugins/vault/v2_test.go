// Copyright (C) 2020-2026, Hanzo AI Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package vault

import (
	"bytes"
	"testing"
)

func TestSharedVault_MembersCanAccess(t *testing.T) {
	v, _ := Open(SDKConfig{
		DataDir:   t.TempDir(),
		MasterKEK: testMasterKey(),
		OrgID:     "test-org",
	})
	defer v.Close()

	ss, err := v.OpenSharedVault("team-vault", []string{"alice", "bob"})
	if err != nil {
		t.Fatal(err)
	}

	// Alice writes.
	if err := ss.PutAs("alice", "doc", []byte("shared secret")); err != nil {
		t.Fatal(err)
	}

	// Bob reads.
	got, err := ss.GetAs("bob", "doc")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "shared secret" {
		t.Fatalf("got %q, want %q", got, "shared secret")
	}
}

func TestSharedVault_NonMemberCannotDecrypt(t *testing.T) {
	v, _ := Open(SDKConfig{
		DataDir:   t.TempDir(),
		MasterKEK: testMasterKey(),
		OrgID:     "test-org",
	})
	defer v.Close()

	ss, _ := v.OpenSharedVault("team-vault", []string{"alice", "bob"})

	if err := ss.PutAs("alice", "doc", []byte("secret")); err != nil {
		t.Fatal(err)
	}

	// Eve is not a member.
	_, err := ss.GetAs("eve", "doc")
	if err == nil {
		t.Fatal("non-member should not be able to read shared vault")
	}

	err = ss.PutAs("eve", "hack", []byte("evil"))
	if err == nil {
		t.Fatal("non-member should not be able to write shared vault")
	}
}

func TestMultiDeviceEnroll_DifferentDeviceKeys(t *testing.T) {
	v, _ := Open(SDKConfig{
		DataDir:   t.TempDir(),
		MasterKEK: testMasterKey(),
		OrgID:     "test-org",
	})
	defer v.Close()

	v.OpenUser("alice")

	dk1, err := v.EnrollDevice("alice", "laptop")
	if err != nil {
		t.Fatal(err)
	}
	dk2, err := v.EnrollDevice("alice", "phone")
	if err != nil {
		t.Fatal(err)
	}

	if bytes.Equal(dk1.Key, dk2.Key) {
		t.Fatal("different devices should get different keys")
	}
	if len(dk1.Key) != 32 || len(dk2.Key) != 32 {
		t.Fatal("device keys should be 32 bytes")
	}

	// Same device ID produces same key (deterministic).
	dk1again, _ := v.EnrollDevice("alice", "laptop")
	if !bytes.Equal(dk1.Key, dk1again.Key) {
		t.Fatal("same device should produce same key")
	}
}

func TestMultiDeviceRevoke_OtherDevicesSurvive(t *testing.T) {
	v, _ := Open(SDKConfig{
		DataDir:   t.TempDir(),
		MasterKEK: testMasterKey(),
		OrgID:     "test-org",
	})
	defer v.Close()

	session, _ := v.OpenUser("alice")
	session.Put("data", []byte("important"))

	dk1, _ := v.EnrollDevice("alice", "laptop")
	_, _ = v.EnrollDevice("alice", "phone")

	// Revoke phone.
	if err := v.RevokeDevice("alice", "phone"); err != nil {
		t.Fatal(err)
	}

	// Laptop key still works (unchanged).
	dk1after, _ := v.EnrollDevice("alice", "laptop")
	if !bytes.Equal(dk1.Key, dk1after.Key) {
		t.Fatal("revoking phone should not affect laptop key")
	}

	// User data still accessible.
	got, err := session.Get("data")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "important" {
		t.Fatalf("got %q", got)
	}
}

func TestCollectionSharing_RecipientCanDecryptCollection(t *testing.T) {
	v, _ := Open(SDKConfig{
		DataDir:   t.TempDir(),
		MasterKEK: testMasterKey(),
		OrgID:     "test-org",
	})
	defer v.Close()

	alice, _ := v.OpenUser("alice")

	// Alice stores data in a collection.
	if err := alice.PutCollection("photos", "sunset", []byte("image-data")); err != nil {
		t.Fatal(err)
	}

	// Alice shares "photos" collection with bob.
	token := alice.ShareCollection("photos", "bob")

	// Bob uses the token to decrypt the collection.
	alice.mu.RLock()
	store := alice.store
	alice.mu.RUnlock()

	got, err := GetWithToken(token, store, "sunset")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "image-data" {
		t.Fatalf("got %q, want %q", got, "image-data")
	}
}

func TestCollectionSharing_RecipientCannotDecryptOtherCollections(t *testing.T) {
	v, _ := Open(SDKConfig{
		DataDir:   t.TempDir(),
		MasterKEK: testMasterKey(),
		OrgID:     "test-org",
	})
	defer v.Close()

	alice, _ := v.OpenUser("alice")

	// Alice stores data in two collections.
	alice.PutCollection("photos", "sunset", []byte("image-data"))
	alice.PutCollection("secrets", "key", []byte("top-secret"))

	// Share only "photos" with bob.
	photosToken := alice.ShareCollection("photos", "bob")

	alice.mu.RLock()
	store := alice.store
	alice.mu.RUnlock()

	// Bob can read photos.
	_, err := GetWithToken(photosToken, store, "sunset")
	if err != nil {
		t.Fatal("bob should be able to read shared collection")
	}

	// Bob cannot read secrets — the token's DEK is for "photos", not "secrets".
	// Create a fake token with the photos DEK but pointing at secrets collection.
	fakeToken := &ShareToken{
		Collection:    "secrets",
		RecipientID:   "bob",
		CollectionDEK: photosToken.CollectionDEK, // wrong DEK for secrets
	}
	_, err = GetWithToken(fakeToken, store, "key")
	if err == nil {
		t.Fatal("bob should not be able to decrypt other collections with photos token")
	}
}

func TestRecovery_ThresholdRecovers(t *testing.T) {
	v, _ := Open(SDKConfig{
		DataDir:   t.TempDir(),
		MasterKEK: testMasterKey(),
		OrgID:     "test-org",
	})
	defer v.Close()

	session, _ := v.OpenUser("alice")
	originalDEK := make([]byte, len(session.dek))
	copy(originalDEK, session.dek)

	// Split into 3-of-3 shares.
	shares, err := session.CreateRecoveryShares(3, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(shares) != 3 {
		t.Fatalf("got %d shares, want 3", len(shares))
	}

	// Each share should be 32 bytes.
	for i, s := range shares {
		if len(s) != 32 {
			t.Fatalf("share %d: got %d bytes, want 32", i, len(s))
		}
	}

	// All 3 shares recover the DEK.
	recovered, err := RecoverFromShares(shares)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(recovered, originalDEK) {
		t.Fatal("recovered DEK does not match original")
	}
}

func TestRecovery_BelowThresholdFails(t *testing.T) {
	v, _ := Open(SDKConfig{
		DataDir:   t.TempDir(),
		MasterKEK: testMasterKey(),
		OrgID:     "test-org",
	})
	defer v.Close()

	session, _ := v.OpenUser("alice")
	originalDEK := make([]byte, len(session.dek))
	copy(originalDEK, session.dek)

	// Split into 3-of-3 shares.
	shares, err := session.CreateRecoveryShares(3, 3)
	if err != nil {
		t.Fatal(err)
	}

	// Only 2 of 3 shares — cannot recover correctly.
	partial, err := RecoverFromShares(shares[:2])
	if err != nil {
		t.Fatal("RecoverFromShares should not error with 2 shares, it just produces wrong output")
	}
	if bytes.Equal(partial, originalDEK) {
		t.Fatal("2 of 3 shares should NOT recover the correct DEK")
	}
}
