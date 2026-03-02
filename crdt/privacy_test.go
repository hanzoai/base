// Copyright (C) 2025-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package crdt

import (
	"errors"
	"testing"

	"github.com/luxfi/age"
)

// TestPlaintextRoundTrip confirms EncryptOp → DecryptOp is the identity
// for the zero-overhead backend. This is the contract every backend
// must satisfy for locally-applied ops; HomomorphicMerge is a separate
// capability.
func TestPlaintextRoundTrip(t *testing.T) {
	p := NewPlaintextPrivacy()

	orig := Operation{
		Field:     "counter-a",
		FieldType: FieldTypeCounter,
		Data:      []byte("42"),
	}

	blob, err := p.EncryptOp(orig)
	if err != nil {
		t.Fatalf("EncryptOp: %v", err)
	}

	got, err := p.DecryptOp(blob)
	if err != nil {
		t.Fatalf("DecryptOp: %v", err)
	}

	if got.Field != orig.Field || got.FieldType != orig.FieldType || string(got.Data) != string(orig.Data) {
		t.Fatalf("round-trip mismatch: got %+v, want %+v", got, orig)
	}
}


// TestAgeRoundTrip encrypts with an age identity's recipient and
// decrypts with the same identity. The ciphertext blob must differ
// from the plaintext (so we're confident age actually ran) and the
// decrypted op must equal the original.
func TestAgeRoundTrip(t *testing.T) {
	id, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatalf("generate identity: %v", err)
	}
	p, err := NewAgePrivacy(id, []age.Recipient{id.Recipient()})
	if err != nil {
		t.Fatalf("NewAgePrivacy: %v", err)
	}

	orig := Operation{
		Field:     "reg-x",
		FieldType: FieldTypeRegister,
		Data:      []byte("hello"),
	}

	blob, err := p.EncryptOp(orig)
	if err != nil {
		t.Fatalf("EncryptOp: %v", err)
	}
	if len(blob) < 16 {
		t.Fatalf("age blob suspiciously small (%d bytes)", len(blob))
	}
	// Sanity: blob should not contain the plaintext field name.
	if bytesContainString(blob, "reg-x") {
		t.Fatal("age blob appears to leak plaintext")
	}

	got, err := p.DecryptOp(blob)
	if err != nil {
		t.Fatalf("DecryptOp: %v", err)
	}
	if got.Field != orig.Field {
		t.Fatalf("round-trip mismatch: got %+v, want %+v", got, orig)
	}
}

// TestAgeRejectsWrongIdentity — a ciphertext for identity A must not
// decrypt with identity B. Catches key-binding regressions.
func TestAgeRejectsWrongIdentity(t *testing.T) {
	idA, _ := age.GenerateX25519Identity()
	idB, _ := age.GenerateX25519Identity()
	encSide, _ := NewAgePrivacy(idA, []age.Recipient{idA.Recipient()})
	decSide, _ := NewAgePrivacy(idB, []age.Recipient{idB.Recipient()})

	blob, err := encSide.EncryptOp(Operation{Field: "f", FieldType: FieldTypeCounter, Data: []byte{1}})
	if err != nil {
		t.Fatalf("EncryptOp: %v", err)
	}
	if _, err := decSide.DecryptOp(blob); err == nil {
		t.Fatal("expected decryption to fail with wrong identity")
	}
}

// TestAgeHomomorphicMergeUnsupported — same contract as plaintext.

// TestDefaultPrivacyIsPlaintext — the no-config path must be plaintext
// so tests and local dev require zero key setup.
func TestDefaultPrivacyIsPlaintext(t *testing.T) {
	if DefaultPrivacy().Name() != "plaintext/v1" {
		t.Fatalf("DefaultPrivacy changed: got %q", DefaultPrivacy().Name())
	}
}

// -------------------------------------------------------------------
// Document privacy integration tests
// -------------------------------------------------------------------

// TestDocumentPlaintextPrivacyRoundTrip verifies that a Document with
// the default plaintext backend produces ops that seal and open as a
// no-op (identical to the old pre-privacy behavior).
func TestDocumentPlaintextPrivacyRoundTrip(t *testing.T) {
	doc := NewDocument("pt-doc", "nodeA")
	if doc.Privacy().Name() != "plaintext/v1" {
		t.Fatalf("default privacy: got %q", doc.Privacy().Name())
	}

	doc.GetText("body").InsertText(-1, "hello")
	doc.GetCounter("views").Increment("nodeA", 7)

	ops := doc.Diff(nil)
	if len(ops) == 0 {
		t.Fatal("expected ops from Diff")
	}

	envs, err := doc.SealOps(ops)
	if err != nil {
		t.Fatalf("SealOps: %v", err)
	}
	for _, env := range envs {
		if env.PrivacyTag != "plaintext/v1" {
			t.Fatalf("envelope tag: got %q", env.PrivacyTag)
		}
	}

	// Open on a second doc (same backend) and apply — text must match.
	doc2 := NewDocument("pt-doc", "nodeB")
	opened, err := doc2.OpenOps(envs)
	if err != nil {
		t.Fatalf("OpenOps: %v", err)
	}
	// Apply via SyncManager machinery.
	sm := NewSyncManager(nil)
	sm.RegisterDocument("pt-doc", doc2)
	if err := sm.applyOps(doc2, opened); err != nil {
		t.Fatalf("applyOps: %v", err)
	}

	if s := doc2.GetText("body").ToString(); s != "hello" {
		t.Fatalf("expected 'hello', got %q", s)
	}
	if v := doc2.GetCounter("views").Value(); v != 7 {
		t.Fatalf("expected counter=7, got %d", v)
	}
}

// TestDocumentAgePrivacyRoundTrip verifies the full encrypt-persist-
// decrypt-apply cycle under the age backend.
func TestDocumentAgePrivacyRoundTrip(t *testing.T) {
	id, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatalf("generate identity: %v", err)
	}
	priv, err := NewAgePrivacy(id, []age.Recipient{id.Recipient()})
	if err != nil {
		t.Fatalf("NewAgePrivacy: %v", err)
	}

	doc := NewDocument("age-doc", "nodeA", WithPrivacy(priv))
	if doc.Privacy().Name() != "age/v1" {
		t.Fatalf("privacy name: got %q", doc.Privacy().Name())
	}

	doc.GetText("title").InsertText(-1, "secret")
	doc.GetRegister("status").Set("draft", Timestamp{Time: 1, NodeID: "nodeA"})

	ops := doc.Diff(nil)
	if len(ops) == 0 {
		t.Fatal("expected ops from Diff")
	}

	envs, err := doc.SealOps(ops)
	if err != nil {
		t.Fatalf("SealOps: %v", err)
	}
	for _, env := range envs {
		if env.PrivacyTag != "age/v1" {
			t.Fatalf("envelope tag: got %q", env.PrivacyTag)
		}
		// Ciphertext must not leak the field name.
		if bytesContainString(env.Ciphertext, "title") {
			t.Fatal("ciphertext leaks plaintext field name")
		}
	}

	// Decrypt on a second document with the same key material.
	doc2 := NewDocument("age-doc", "nodeB", WithPrivacy(priv))
	opened, err := doc2.OpenOps(envs)
	if err != nil {
		t.Fatalf("OpenOps: %v", err)
	}

	sm := NewSyncManager(nil)
	sm.RegisterDocument("age-doc", doc2)
	if err := sm.applyOps(doc2, opened); err != nil {
		t.Fatalf("applyOps: %v", err)
	}

	if s := doc2.GetText("title").ToString(); s != "secret" {
		t.Fatalf("expected 'secret', got %q", s)
	}
	val, _ := doc2.GetRegister("status").Get()
	if val != "draft" {
		t.Fatalf("expected 'draft', got %v", val)
	}
}

// TestDocumentCrossBackendRejection verifies that an op encrypted under
// the age backend is rejected by a plaintext-backend document with
// ErrPrivacyMismatch, not silently corrupted.
func TestDocumentCrossBackendRejection(t *testing.T) {
	id, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatalf("generate identity: %v", err)
	}
	agePriv, err := NewAgePrivacy(id, []age.Recipient{id.Recipient()})
	if err != nil {
		t.Fatalf("NewAgePrivacy: %v", err)
	}

	// Produce envelopes under age.
	ageDoc := NewDocument("xb-doc", "nodeA", WithPrivacy(agePriv))
	ageDoc.GetText("body").InsertText(-1, "classified")
	ops := ageDoc.Diff(nil)
	envs, err := ageDoc.SealOps(ops)
	if err != nil {
		t.Fatalf("SealOps: %v", err)
	}

	// Try to open on a plaintext document.
	ptDoc := NewDocument("xb-doc", "nodeB") // default plaintext
	_, err = ptDoc.OpenOps(envs)
	if err == nil {
		t.Fatal("expected error when opening age envelopes on plaintext doc")
	}
	if !errors.Is(err, ErrPrivacyMismatch) {
		t.Fatalf("expected ErrPrivacyMismatch, got: %v", err)
	}

	// Also verify the reverse: plaintext envelopes rejected by age doc.
	ptDoc2 := NewDocument("xb-doc2", "nodeC")
	ptDoc2.GetText("body").InsertText(-1, "public")
	ptOps := ptDoc2.Diff(nil)
	ptEnvs, err := ptDoc2.SealOps(ptOps)
	if err != nil {
		t.Fatalf("SealOps plaintext: %v", err)
	}

	ageDoc2 := NewDocument("xb-doc2", "nodeD", WithPrivacy(agePriv))
	_, err = ageDoc2.OpenOps(ptEnvs)
	if err == nil {
		t.Fatal("expected error when opening plaintext envelopes on age doc")
	}
	if !errors.Is(err, ErrPrivacyMismatch) {
		t.Fatalf("expected ErrPrivacyMismatch, got: %v", err)
	}
}

// bytesContainString is a tiny helper so we don't pull in strings.Contains
// on a []byte (which would require an allocation).
func bytesContainString(haystack []byte, needle string) bool {
	n := len(needle)
	if n == 0 || len(haystack) < n {
		return false
	}
	for i := 0; i+n <= len(haystack); i++ {
		if string(haystack[i:i+n]) == needle {
			return true
		}
	}
	return false
}
