package org

import (
	"encoding/base64"
	"encoding/hex"
	"testing"

	"github.com/hanzoai/cek"
	"github.com/hanzoai/namespace"
)

// testMaster is the master key testOrgDB installs. cek requires exactly 32
// bytes, which is what this is.
const testMaster = "test-master-key-32-bytes-long!!!"

// TestOrgDB_DEK_IsCEKDerivation states the compatibility claim as an assertion
// rather than as a comment. base once carried a comment saying its derivation
// was "compatible with hanzoai/sqlite's CEK derivation" while the two produced
// different keys, and a comment cannot fail. This can: if OrgDEK or UserDEK
// ever stops being exactly cek.DeriveKey of the same namespace, this test goes
// red on the next run instead of staying wrong for a year.
func TestOrgDB_DEK_IsCEKDerivation(t *testing.T) {
	db, _ := testOrgDB(t)
	master := []byte(testMaster)

	t.Run("org", func(t *testing.T) {
		want, err := cek.DeriveKey(master, namespace.MustOrg("acme"), orgDEKSubsystem)
		if err != nil {
			t.Fatalf("cek.DeriveKey: %v", err)
		}
		got, err := db.OrgDEK("acme")
		if err != nil {
			t.Fatalf("OrgDEK: %v", err)
		}
		if got != hex.EncodeToString(want) {
			t.Fatalf("org DEK is not the cek derivation:\n got %s\nwant %s", got, hex.EncodeToString(want))
		}
	})

	// The user namespace carries the org, which is what keeps acme/alice and
	// globex/alice different keys. Deriving from the bare user id would make
	// them the same key wherever two orgs share a user id.
	t.Run("user", func(t *testing.T) {
		ns, err := namespace.Of("acme/user-001")
		if err != nil {
			t.Fatalf("namespace.Of: %v", err)
		}
		want, err := cek.DeriveKey(master, ns, userDEKSubsystem)
		if err != nil {
			t.Fatalf("cek.DeriveKey: %v", err)
		}
		got, err := db.UserDEK("acme", "user-001")
		if err != nil {
			t.Fatalf("UserDEK: %v", err)
		}
		if got != hex.EncodeToString(want) {
			t.Fatalf("user DEK is not the cek derivation:\n got %s\nwant %s", got, hex.EncodeToString(want))
		}
	})
}

// The S3 keys are the same derivation as the database keys, under a different
// subsystem. That is what keeps an org's objects and its database on separate
// keys while there is still only one way to derive either.
func TestOrgStorage_SSEKey_IsCEKDerivation(t *testing.T) {
	s := &OrgStorage{MasterKey: testMaster}
	master := []byte(testMaster)

	t.Run("org", func(t *testing.T) {
		want, err := cek.DeriveKey(master, namespace.MustOrg("acme"), s3Subsystem)
		if err != nil {
			t.Fatalf("cek.DeriveKey: %v", err)
		}
		got, err := s.OrgSSEKey("acme")
		if err != nil {
			t.Fatalf("OrgSSEKey: %v", err)
		}
		if got != base64.StdEncoding.EncodeToString(want) {
			t.Fatalf("org SSE key is not the cek derivation:\n got %s\nwant %s",
				got, base64.StdEncoding.EncodeToString(want))
		}
	})

	t.Run("user", func(t *testing.T) {
		ns, err := namespace.Of("acme/user-001")
		if err != nil {
			t.Fatalf("namespace.Of: %v", err)
		}
		want, err := cek.DeriveKey(master, ns, s3Subsystem)
		if err != nil {
			t.Fatalf("cek.DeriveKey: %v", err)
		}
		got, err := s.UserSSEKey("acme", "user-001")
		if err != nil {
			t.Fatalf("UserSSEKey: %v", err)
		}
		if got != base64.StdEncoding.EncodeToString(want) {
			t.Fatalf("user SSE key is not the cek derivation:\n got %s\nwant %s",
				got, base64.StdEncoding.EncodeToString(want))
		}
	})

	// An org's objects and its database must not share a key.
	t.Run("distinct from the database key", func(t *testing.T) {
		db, _ := testOrgDB(t)
		dek, err := db.OrgDEK("acme")
		if err != nil {
			t.Fatalf("OrgDEK: %v", err)
		}
		sse, err := s.OrgSSEKey("acme")
		if err != nil {
			t.Fatalf("OrgSSEKey: %v", err)
		}
		raw, err := base64.StdEncoding.DecodeString(sse)
		if err != nil {
			t.Fatalf("decode: %v", err)
		}
		if hex.EncodeToString(raw) == dek {
			t.Fatal("the S3 key and the database key for one org must differ")
		}
	})
}
