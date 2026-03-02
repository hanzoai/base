// Copyright (C) 2020-2026, Hanzo AI Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package vault

import (
	"testing"
	"time"
)

// ─── Confidential Compute ───────────────────────────────────────────────────

func TestConfidentialCount(t *testing.T) {
	v, _ := Open(SDKConfig{
		DataDir:   t.TempDir(),
		MasterKEK: testMasterKey(),
		OrgID:     "test-org",
	})
	defer v.Close()

	s, _ := v.OpenUser("alice")
	s.Put("doc:a", []byte("1"))
	s.Put("doc:b", []byte("2"))
	s.Put("doc:c", []byte("3"))
	s.Put("other:x", []byte("4"))

	engine := &LocalConfidentialEngine{}
	result, err := engine.Execute(s, &ConfidentialQuery{
		Operation: "count",
		Target:    "doc:",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Value.(int) != 3 {
		t.Fatalf("count = %v, want 3", result.Value)
	}
	if result.Encrypted {
		t.Fatal("local engine should return plaintext")
	}

	// Verify proof
	ok, err := engine.Verify(result)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("proof verification failed")
	}
}

func TestConfidentialMatch(t *testing.T) {
	v, _ := Open(SDKConfig{
		DataDir:   t.TempDir(),
		MasterKEK: testMasterKey(),
		OrgID:     "test-org",
	})
	defer v.Close()

	s, _ := v.OpenUser("alice")
	s.Put("status", []byte("active"))

	engine := &LocalConfidentialEngine{}

	// Positive match
	result, err := engine.Execute(s, &ConfidentialQuery{
		Operation: "match",
		Target:    "status",
		Params:    "active",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Value.(bool) != true {
		t.Fatal("expected match")
	}

	// Negative match
	result, err = engine.Execute(s, &ConfidentialQuery{
		Operation: "match",
		Target:    "status",
		Params:    "inactive",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Value.(bool) != false {
		t.Fatal("expected no match")
	}

	// Missing key
	result, err = engine.Execute(s, &ConfidentialQuery{
		Operation: "match",
		Target:    "nonexistent",
		Params:    "anything",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Value.(bool) != false {
		t.Fatal("expected false for missing key")
	}
}

func TestConfidentialPolicyCheck(t *testing.T) {
	pe := NewPolicyEngine()
	pe.Grant(&Capability{
		Issuer:   "did:lux:org:acme",
		Subject:  "alice",
		Resource: "vault:acme:*",
		Actions:  []string{"read"},
	})

	v, _ := Open(SDKConfig{
		DataDir:   t.TempDir(),
		MasterKEK: testMasterKey(),
		OrgID:     "test-org",
	})
	defer v.Close()

	s, _ := v.OpenUser("alice")

	engine := &LocalConfidentialEngine{}

	// Check via confidential query
	result, err := engine.Execute(s, &ConfidentialQuery{
		Operation: "policy_check",
		Target:    "alice:vault:acme:docs:read",
		Params:    pe,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Value.(bool) != true {
		t.Fatal("policy check should pass")
	}

	ok, err := engine.Verify(result)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("proof verification failed")
	}

	// Denied action
	result, err = engine.Execute(s, &ConfidentialQuery{
		Operation: "policy_check",
		Target:    "alice:vault:acme:docs:write",
		Params:    pe,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Value.(bool) != false {
		t.Fatal("policy check should deny write")
	}
}

// ─── Policy Engine ──────────────────────────────────────────────────────────

func TestPolicyGrant_AllowsAccess(t *testing.T) {
	pe := NewPolicyEngine()

	err := pe.Grant(&Capability{
		Issuer:   "did:lux:org:acme",
		Subject:  "did:lux:user:alice",
		Resource: "vault:acme:*",
		Actions:  []string{"read", "write"},
	})
	if err != nil {
		t.Fatal(err)
	}

	if !pe.Check("did:lux:user:alice", "vault:acme:docs", "read") {
		t.Fatal("alice should have read access")
	}
	if !pe.Check("did:lux:user:alice", "vault:acme:keys", "write") {
		t.Fatal("alice should have write access")
	}
}

func TestPolicyCheck_DeniesUnauthorized(t *testing.T) {
	pe := NewPolicyEngine()

	pe.Grant(&Capability{
		Issuer:   "did:lux:org:acme",
		Subject:  "did:lux:user:alice",
		Resource: "vault:acme:docs",
		Actions:  []string{"read"},
	})

	// Bob has no capabilities
	if pe.Check("did:lux:user:bob", "vault:acme:docs", "read") {
		t.Fatal("bob should not have access")
	}

	// Alice lacks write
	if pe.Check("did:lux:user:alice", "vault:acme:docs", "write") {
		t.Fatal("alice should not have write access")
	}

	// Wrong resource
	if pe.Check("did:lux:user:alice", "vault:other:docs", "read") {
		t.Fatal("alice should not have access to other vault")
	}
}

func TestPolicyRevoke(t *testing.T) {
	pe := NewPolicyEngine()

	cap := &Capability{
		Issuer:   "did:lux:org:acme",
		Subject:  "did:lux:user:alice",
		Resource: "vault:acme:*",
		Actions:  []string{"read"},
	}
	pe.Grant(cap)

	// Access works before revoke
	if !pe.Check("did:lux:user:alice", "vault:acme:docs", "read") {
		t.Fatal("should have access before revoke")
	}

	// Revoke
	if err := pe.Revoke(cap.ID); err != nil {
		t.Fatal(err)
	}

	// Access denied after revoke
	if pe.Check("did:lux:user:alice", "vault:acme:docs", "read") {
		t.Fatal("should not have access after revoke")
	}

	// List should be empty
	caps := pe.List("did:lux:user:alice")
	if len(caps) != 0 {
		t.Fatalf("expected 0 caps after revoke, got %d", len(caps))
	}

	// Revoke unknown ID returns error
	if err := pe.Revoke("nonexistent"); err == nil {
		t.Fatal("should error on unknown cap ID")
	}
}

func TestPolicyExpiry(t *testing.T) {
	pe := NewPolicyEngine()

	// Grant a capability that already expired
	pe.Grant(&Capability{
		Issuer:   "did:lux:org:acme",
		Subject:  "did:lux:user:alice",
		Resource: "vault:acme:*",
		Actions:  []string{"read"},
		Expires:  time.Now().Add(-1 * time.Hour), // expired 1 hour ago
	})

	if pe.Check("did:lux:user:alice", "vault:acme:docs", "read") {
		t.Fatal("expired capability should not grant access")
	}

	// Grant a non-expired capability
	pe.Grant(&Capability{
		Issuer:   "did:lux:org:acme",
		Subject:  "did:lux:user:bob",
		Resource: "vault:acme:*",
		Actions:  []string{"read"},
		Expires:  time.Now().Add(1 * time.Hour), // expires in 1 hour
	})

	if !pe.Check("did:lux:user:bob", "vault:acme:docs", "read") {
		t.Fatal("non-expired capability should grant access")
	}
}

// ─── Audit Log ──────────────────────────────────────────────────────────────

func TestAuditLog_RecordsOps(t *testing.T) {
	al := NewAuditLog()

	al.Record("org:alice", "did:lux:user:alice", "put", "key:prefs")
	al.Record("org:alice", "did:lux:user:alice", "get", "key:prefs")
	al.Record("org:bob", "did:lux:user:bob", "put", "key:config")

	if al.Len() != 3 {
		t.Fatalf("len = %d, want 3", al.Len())
	}

	// Filter by vault ID
	aliceEntries := al.GetAuditLog("org:alice", time.Time{})
	if len(aliceEntries) != 2 {
		t.Fatalf("alice entries = %d, want 2", len(aliceEntries))
	}

	bobEntries := al.GetAuditLog("org:bob", time.Time{})
	if len(bobEntries) != 1 {
		t.Fatalf("bob entries = %d, want 1", len(bobEntries))
	}

	// Verify hash chain integrity
	if !al.Verify() {
		t.Fatal("audit log hash chain verification failed")
	}

	// First entry has no prev hash
	if aliceEntries[0].PrevHash != "" {
		t.Fatal("first entry should have empty PrevHash")
	}

	// Second entry links to first
	if aliceEntries[1].PrevHash == "" {
		t.Fatal("second entry should have non-empty PrevHash")
	}
}

func TestAuditLog_MerkleRoot(t *testing.T) {
	al := NewAuditLog()

	// Empty log has no root
	if al.MerkleRoot() != "" {
		t.Fatal("empty log should have empty merkle root")
	}

	al.Record("org:alice", "did:lux:user:alice", "put", "key:a")
	root1 := al.MerkleRoot()
	if root1 == "" {
		t.Fatal("merkle root should not be empty after record")
	}

	al.Record("org:alice", "did:lux:user:alice", "put", "key:b")
	root2 := al.MerkleRoot()
	if root2 == "" {
		t.Fatal("merkle root should not be empty")
	}
	if root2 == root1 {
		t.Fatal("merkle root should change after new entry")
	}

	// Root is deterministic (verify hash chain)
	if !al.Verify() {
		t.Fatal("hash chain verification failed")
	}

	// Root equals last entry's hash
	entries := al.GetAuditLog("org:alice", time.Time{})
	lastHash := entries[len(entries)-1].Hash
	if al.MerkleRoot() != lastHash {
		t.Fatalf("merkle root %q != last hash %q", al.MerkleRoot(), lastHash)
	}
}
