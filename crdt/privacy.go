// Copyright (C) 2025-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Privacy backends for the CRDT op-log.
//
// A CRDT document carries a stream of `Operation`s. Before those ops
// leave the process — to be persisted, gossiped, or replicated — they
// pass through a Privacy backend. Three shapes are supported:
//
//	plaintext    No encryption. Fast, debuggable, appropriate for
//	             self-hosted single-user Base.
//
//	age          End-to-end encryption. Each op is sealed under the
//	             recipients' age public keys (ML-KEM-768 + X25519
//	             hybrid). The server/relay sees ciphertext only and
//	             cannot compute anything on it. Appropriate for
//	             multi-device personal sync.
//	             Build with: go build -tags cgo (no extra tag needed;
//	             luxfi/age is pure-Go).
//
//	fhe          Fully homomorphic encryption via luxfi/fhe. The server
//	             can merge ciphertexts without decrypting. Orders of
//	             magnitude more expensive than plaintext or age, but the
//	             only option when an untrusted third party must compute
//	             on the data (e.g. cross-tenant analytics).
//	             Build with: go build -tags fhe
//
// The CRDT machinery is identical across backends; only the encode /
// decode / homomorphic-merge hooks differ. This lets callers pick the
// privacy model without changing application code.
//
// Reference: papers/fhe/fhecrdt/main.tex; docs/CRDT-PRIVACY-MODELS.md.

package crdt

import (
	"encoding/json"
)

// Privacy is the pluggable encryption boundary for a CRDT op-log.
//
// Every op produced by the local replica is sealed with EncryptOp
// before persistence or gossip. Every op consumed from the log is
// opened with DecryptOp before application. One interface, one path.
type Privacy interface {
	// Name returns a stable identifier carried in every OpEnvelope so a
	// replica can refuse ops from an incompatible backend.
	Name() string

	// EncryptOp seals an operation for storage and transport.
	EncryptOp(op Operation) ([]byte, error)

	// DecryptOp unseals an op produced by EncryptOp.
	DecryptOp(blob []byte) (Operation, error)
}

// PlaintextPrivacy is the zero-overhead backend: it just encodes the
// op as JSON and reports no encryption. It exists so the CRDT layer
// has one code path; there are no `if privacy == nil` branches.
type PlaintextPrivacy struct{}

// NewPlaintextPrivacy returns the no-op encryption backend.
func NewPlaintextPrivacy() *PlaintextPrivacy { return &PlaintextPrivacy{} }

func (PlaintextPrivacy) Name() string                           { return "plaintext/v1" }
func (PlaintextPrivacy) EncryptOp(op Operation) ([]byte, error)  { return json.Marshal(op) }
func (PlaintextPrivacy) DecryptOp(b []byte) (Operation, error) {
	var op Operation
	err := json.Unmarshal(b, &op)
	return op, err
}

// DefaultPrivacy returns a Privacy implementation suitable for the
// current build. The default is plaintext so tests and dev loops
// require no key material. Production callers pass an explicit
// NewAgePrivacy(...) or NewFHEPrivacy(...) to NewDocument().
func DefaultPrivacy() Privacy { return NewPlaintextPrivacy() }

// Compile-time check.
var _ Privacy = (*PlaintextPrivacy)(nil)
