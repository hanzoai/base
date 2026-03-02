// Copyright (C) 2025-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// age-encrypted CRDT op-log.
//
// Each op is sealed with luxfi/age, which uses the X25519 + ML-KEM-768
// hybrid recipient so both classical and PQ adversaries are covered.
// The backend supports multiple recipients — every device that should
// be able to decrypt the log is added as a recipient. New devices
// join by having an existing keyholder re-encrypt a stream of ops to
// a set that now includes them (out-of-scope here; see the rotate-
// recipient helper in luxfi/age for the pattern).
//
// A relay that receives age blobs appends them to the log and
// broadcasts them; replicas that hold the keys decrypt locally.

package crdt

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"

	"github.com/luxfi/age"
)

// AgePrivacy encrypts every op under a set of age recipients and
// decrypts with a single identity held by the local replica.
type AgePrivacy struct {
	recipients []age.Recipient
	identity   age.Identity
}

// NewAgePrivacy constructs the age backend. `identity` is the local
// replica's secret key; `recipients` is the set of public keys of
// every device authorised to decrypt the log. The local identity's
// corresponding recipient should appear in `recipients` so we can
// round-trip our own ops.
func NewAgePrivacy(identity age.Identity, recipients []age.Recipient) (*AgePrivacy, error) {
	if identity == nil {
		return nil, fmt.Errorf("crdt/privacy_age: identity must not be nil")
	}
	if len(recipients) == 0 {
		return nil, fmt.Errorf("crdt/privacy_age: at least one recipient required")
	}
	return &AgePrivacy{recipients: recipients, identity: identity}, nil
}

// Name returns the stable wire tag for this backend.
func (*AgePrivacy) Name() string { return "age/v1" }

// EncryptOp serialises the op as JSON then seals it for every recipient.
func (p *AgePrivacy) EncryptOp(op Operation) ([]byte, error) {
	payload, err := json.Marshal(op)
	if err != nil {
		return nil, fmt.Errorf("crdt/privacy_age: marshal op: %w", err)
	}
	var buf bytes.Buffer
	w, err := age.Encrypt(&buf, p.recipients...)
	if err != nil {
		return nil, fmt.Errorf("crdt/privacy_age: new encryption stream: %w", err)
	}
	if _, err := w.Write(payload); err != nil {
		return nil, fmt.Errorf("crdt/privacy_age: write payload: %w", err)
	}
	if err := w.Close(); err != nil {
		return nil, fmt.Errorf("crdt/privacy_age: close stream: %w", err)
	}
	return buf.Bytes(), nil
}

// DecryptOp unseals a blob produced by EncryptOp.
func (p *AgePrivacy) DecryptOp(blob []byte) (Operation, error) {
	var op Operation
	r, err := age.Decrypt(bytes.NewReader(blob), p.identity)
	if err != nil {
		return op, fmt.Errorf("crdt/privacy_age: open stream: %w", err)
	}
	payload, err := io.ReadAll(r)
	if err != nil {
		return op, fmt.Errorf("crdt/privacy_age: read payload: %w", err)
	}
	if err := json.Unmarshal(payload, &op); err != nil {
		return op, fmt.Errorf("crdt/privacy_age: unmarshal op: %w", err)
	}
	return op, nil
}

// Compile-time check.
var _ Privacy = (*AgePrivacy)(nil)
