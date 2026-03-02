// Copyright (C) 2020-2026, Hanzo AI Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package vault

// Confidential compute interface for vault.
//
// This is an OPT-IN layer for running computations on encrypted data.
// It is NOT the default path. Normal vault operations (Put/Get/Sync/Anchor)
// work on plaintext after DEK decryption as before.
//
// Use confidential queries when you need:
//   - Encrypted policy checks (does user X have capability Y?)
//   - Aggregations without exposing individual records
//   - Pattern matching on sensitive data
//   - Compliance checks that should not reveal underlying data
//
// The baseline LocalConfidentialEngine decrypts locally, computes, and
// returns results. It exists so the interface is exercised now. Swap in
// a real FHE (TFHE, OpenFHE) or ZK (Groth16, PLONK) engine later
// without changing callers.

// ConfidentialQuery describes a computation to run on encrypted data.
type ConfidentialQuery struct {
	Operation string      // "sum", "count", "match", "policy_check"
	Target    string      // collection or key pattern
	Params    interface{} // query-specific parameters
}

// ConfidentialResult holds the output of a confidential computation.
type ConfidentialResult struct {
	Value     interface{} // result (may be encrypted or plaintext depending on engine)
	Proof     []byte      // hash-based proof of correct computation (optional)
	Encrypted bool        // whether Value is still encrypted
}

// ConfidentialEngine processes queries on encrypted data.
// Implementations range from "decrypt locally and compute" (LocalConfidentialEngine)
// to full FHE/ZK backends. The interface is the same.
type ConfidentialEngine interface {
	Execute(session *Session, query *ConfidentialQuery) (*ConfidentialResult, error)
	Verify(result *ConfidentialResult) (bool, error)
}
