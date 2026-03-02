// Copyright (C) 2025-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package crdt

import (
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/luxfi/age"
)

// // Attack: malicious peer sends SyncMessage{Ops: [...], Envelopes: nil}
// to a replica whose document is configured with age privacy.
// non-plaintext. Current code (resolveOps) silently accepts them.


// 
func TestTagSpoofPlaintextOnAgeDoc(t *testing.T) {
	id, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatalf("generate identity: %v", err)
	}
	priv, err := NewAgePrivacy(id, []age.Recipient{id.Recipient()})
	if err != nil {
		t.Fatalf("NewAgePrivacy: %v", err)
	}

	doc := NewDocument("doc", "n", WithPrivacy(priv))

	// Attacker sends envelope tagged plaintext/v1 with JSON payload.
	fakeEnv := OpEnvelope{
		PrivacyTag: "plaintext/v1",
		Ciphertext: []byte(`{"field":"body","fieldType":0,"data":"AQID"}`),
	}
	_, err = doc.OpenOps([]OpEnvelope{fakeEnv})
	if err == nil {
		t.Fatal("VULNERABLE: plaintext-tagged envelope accepted on age doc")
	}
	if !errors.Is(err, ErrPrivacyMismatch) {
		t.Fatalf("expected ErrPrivacyMismatch, got: %v", err)
	}
}

func TestWrongKeySameTag(t *testing.T) {
	idA, _ := age.GenerateX25519Identity()
	idB, _ := age.GenerateX25519Identity()

	privA, _ := NewAgePrivacy(idA, []age.Recipient{idA.Recipient()})
	privB, _ := NewAgePrivacy(idB, []age.Recipient{idB.Recipient()})

	// Seal with key A.
	docA := NewDocument("doc", "n", WithPrivacy(privA))
	ops := []Operation{{Field: "f", FieldType: FieldTypeCounter, Data: []byte("42")}}
	envs, err := docA.SealOps(ops)
	if err != nil {
		t.Fatalf("SealOps: %v", err)
	}

	// Try to open with key B (same tag "age/v1" but wrong key).
	docB := NewDocument("doc", "n", WithPrivacy(privB))
	_, err = docB.OpenOps(envs)
	if err == nil {
		t.Fatal("VULNERABLE: age ciphertext decrypted with wrong key — no error")
	}
	// Must be a decryption error, NOT silent garbage.
	t.Logf("Correctly rejected wrong-key ciphertext: %v", err)
}

// // Same key K used for doc A and doc B. Envelope sealed for A can be
// applied to B because OpEnvelope has no doc-id binding.


// dispatches to sm.HandleSync. It does NOT check privacy at all.
// If the underlying doc has age privacy, the bridge bypasses it by
// sending bare Ops through HandleSync.


// 
func TestConcurrentSealOpsRace(t *testing.T) {
	id, _ := age.GenerateX25519Identity()
	priv, _ := NewAgePrivacy(id, []age.Recipient{id.Recipient()})
	doc := NewDocument("race-doc", "n", WithPrivacy(priv))

	ops := []Operation{
		{Field: "f", FieldType: FieldTypeCounter, Data: []byte("data")},
	}

	var wg sync.WaitGroup
	errs := make(chan error, 16)

	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			envs, err := doc.SealOps(ops)
			if err != nil {
				errs <- fmt.Errorf("SealOps goroutine %d: %w", i, err)
				return
			}
			_, err = doc.OpenOps(envs)
			if err != nil {
				errs <- fmt.Errorf("OpenOps goroutine %d: %w", i, err)
			}
		}(i)
	}

	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("race error: %v", err)
	}
}

// 
func TestEmptyEnvelopesEmptyOps(t *testing.T) {
	sm := NewSyncManager(nil)
	sm.GetOrCreateDocument("empty-doc", "server")

	msg := SyncMessage{
		Type:      SyncUpdate,
		DocID:     "empty-doc",
		Envelopes: []OpEnvelope{},
	}
	raw, _ := json.Marshal(msg)

	resp, err := sm.HandleSync("client", raw)
	if err != nil {
		t.Fatalf("empty sync should not error: %v", err)
	}
	// resp should be nil (no-op).
	if resp != nil {
		t.Fatalf("expected nil response for empty sync, got %d bytes", len(resp))
	}
}

func TestNilEnvelopesNilOps(t *testing.T) {
	sm := NewSyncManager(nil)
	sm.GetOrCreateDocument("nil-doc", "server")

	msg := SyncMessage{
		Type:  SyncUpdate,
		DocID: "nil-doc",
	}
	raw, _ := json.Marshal(msg)

	resp, err := sm.HandleSync("client", raw)
	if err != nil {
		t.Fatalf("nil sync should not error: %v", err)
	}
	if resp != nil {
		t.Fatalf("expected nil response, got %d bytes", len(resp))
	}
}

// // Document.Encode() produces a gob snapshot in plaintext regardless
// of the privacy backend. Decode() reconstructs without privacy.

func TestSnapshotBypassesPrivacy(t *testing.T) {
	id, _ := age.GenerateX25519Identity()
	priv, _ := NewAgePrivacy(id, []age.Recipient{id.Recipient()})

	doc := NewDocument("snap-doc", "nodeA", WithPrivacy(priv))
	doc.GetText("title").InsertText(-1, "top-secret")
	doc.GetRegister("class").Set("classified", Timestamp{Time: 1, NodeID: "nodeA"})

	data, err := doc.Encode()
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	// The encoded blob must NOT contain plaintext when age backend is active.
	if bytesContainString(data, "top-secret") {
		t.Fatal("VULNERABLE: Encode() leaks plaintext for age-configured doc")
	}

	// Decode without privacy opts must fail (sealed snapshot).
	_, err = Decode(data, "nodeB")
	if err == nil {
		t.Fatal("VULNERABLE: sealed snapshot decoded without privacy backend")
	}

	// Decode with correct privacy opts must reconstruct the document.
	doc2, err := Decode(data, "nodeB", WithPrivacy(priv))
	if err != nil {
		t.Fatalf("Decode with privacy: %v", err)
	}
	if doc2.Privacy().Name() != "age/v1" {
		t.Fatalf("decoded doc privacy: %q, expected age/v1", doc2.Privacy().Name())
	}
	title := doc2.GetText("title").ToString()
	if title != "top-secret" {
		t.Fatalf("decoded title: %q, expected top-secret", title)
	}
}

// // WSCRDTSyncPayload has no Envelopes field. All WS traffic is bare Ops.
// This means every WS client bypasses Privacy even after resolveOps is
// fixed, unless the WS handler is also updated.



// TestBareOpsStillWorkForPlaintextDoc confirms the fix is surgical —
// plaintext documents continue to accept legacy bare-Ops messages for
// backwards compatibility with callers that predate the privacy layer.

func TestCrossDocReplayRejected(t *testing.T) {
	id, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatalf("identity: %v", err)
	}
	priv, err := NewAgePrivacy(id, []age.Recipient{id.Recipient()})
	if err != nil {
		t.Fatalf("NewAgePrivacy: %v", err)
	}

	// Doc A seals a real op.
	docA := NewDocument("doc-A", "nodeA", WithPrivacy(priv))
	docA.GetText("body").InsertText(-1, "secret")
	opsA := docA.Diff(nil)
	envsA, err := docA.SealOps(opsA)
	if err != nil {
		t.Fatalf("SealOps: %v", err)
	}
	if len(envsA) == 0 {
		t.Fatal("no envelopes sealed")
	}

	// Attacker captures envsA and hands them to doc B (same key).
	docB := NewDocument("doc-B", "nodeB", WithPrivacy(priv))
	_, err = docB.OpenOps(envsA)
	if err == nil {
		t.Fatal("doc B accepted an envelope sealed for doc A — cross-doc replay possible")
	}
	if !errors.Is(err, ErrDocIDReplay) {
		t.Fatalf("expected ErrDocIDReplay, got: %v", err)
	}

	// Sanity: doc A can still open its own envelopes.
	if _, err := docA.OpenOps(envsA); err != nil {
		t.Fatalf("doc A must still open its own envelopes: %v", err)
	}
}

// TestDocIDBindingRoundTripsUnchanged proves the wrap/unwrap preserves
// Operation semantics — the `Field`, `FieldType`, and original `Data`
// bytes round-trip through Seal → Open identically.
func TestDocIDBindingRoundTripsUnchanged(t *testing.T) {
	id, _ := age.GenerateX25519Identity()
	priv, _ := NewAgePrivacy(id, []age.Recipient{id.Recipient()})
	doc := NewDocument("rt-doc", "nodeA", WithPrivacy(priv))

	original := []Operation{
		{Field: "counter-a", FieldType: FieldTypeCounter, Data: []byte(`42`)},
		{Field: "reg-x", FieldType: FieldTypeRegister, Data: []byte(`{"v":"hello"}`)},
	}

	envs, err := doc.SealOps(original)
	if err != nil {
		t.Fatalf("SealOps: %v", err)
	}
	opened, err := doc.OpenOps(envs)
	if err != nil {
		t.Fatalf("OpenOps: %v", err)
	}
	if len(opened) != len(original) {
		t.Fatalf("count mismatch: got %d, want %d", len(opened), len(original))
	}
	for i := range original {
		if opened[i].Field != original[i].Field ||
			opened[i].FieldType != original[i].FieldType ||
			string(opened[i].Data) != string(original[i].Data) {
			t.Fatalf("op %d mismatch: got %+v, want %+v", i, opened[i], original[i])
		}
	}
}

// TestRawBlobRejectedAsMissingDocID — a ciphertext without the docID
// header is rejected. Even with valid PrivacyTag and AEAD, the unwrap
// fails and OpenOps returns ErrDocIDReplay.
func TestRawBlobRejectedAsMissingDocID(t *testing.T) {
	id, _ := age.GenerateX25519Identity()
	priv, _ := NewAgePrivacy(id, []age.Recipient{id.Recipient()})
	doc := NewDocument("check", "nodeA", WithPrivacy(priv))

	// Encrypt an op directly via the Privacy backend, bypassing SealOps —
	// this produces a ciphertext with no docID wrapper.
	raw := Operation{Field: "x", FieldType: FieldTypeCounter, Data: []byte(`1`)}
	rawCT, err := priv.EncryptOp(raw)
	if err != nil {
		t.Fatalf("EncryptOp: %v", err)
	}

	envs := []OpEnvelope{{PrivacyTag: priv.Name(), Ciphertext: rawCT}}
	_, err = doc.OpenOps(envs)
	if err == nil {
		t.Fatal("OpenOps accepted an envelope with no docID wrapper")
	}
	if !errors.Is(err, ErrDocIDReplay) {
		t.Fatalf("expected ErrDocIDReplay, got: %v", err)
	}
}
