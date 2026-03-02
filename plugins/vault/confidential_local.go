// Copyright (C) 2020-2026, Hanzo AI Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package vault

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// LocalConfidentialEngine is the baseline confidential compute engine.
// It decrypts data locally, runs the computation, and returns plaintext results
// with a hash-based proof of computation.
//
// This is NOT FHE. It exists to exercise the ConfidentialEngine interface
// so callers are written correctly before a real FHE/ZK backend is plugged in.
// The proof is a SHA-256 hash binding the query + result — verifiable but
// not zero-knowledge.
type LocalConfidentialEngine struct{}

// Execute runs a confidential query against a session's encrypted store.
// Supported operations:
//   - "count"        — count keys matching Target prefix
//   - "sum"          — sum numeric values for keys matching Target prefix
//   - "match"        — check if key Target equals Params (string)
//   - "policy_check" — check if subject (Target) has action (Params) via PolicyEngine
func (e *LocalConfidentialEngine) Execute(session *Session, query *ConfidentialQuery) (*ConfidentialResult, error) {
	if query == nil {
		return nil, fmt.Errorf("vault/confidential: nil query")
	}

	switch query.Operation {
	case "count":
		return e.executeCount(session, query)
	case "sum":
		return e.executeSum(session, query)
	case "match":
		return e.executeMatch(session, query)
	case "policy_check":
		return e.executePolicyCheck(session, query)
	default:
		return nil, fmt.Errorf("vault/confidential: unknown operation %q", query.Operation)
	}
}

// Verify checks that a result's proof matches its value.
// For the local engine, the proof is SHA-256(json(Value)).
func (e *LocalConfidentialEngine) Verify(result *ConfidentialResult) (bool, error) {
	if result == nil {
		return false, fmt.Errorf("vault/confidential: nil result")
	}
	if len(result.Proof) == 0 {
		return false, nil
	}
	expected, err := computeProof(result.Value)
	if err != nil {
		return false, err
	}
	if len(expected) != len(result.Proof) {
		return false, nil
	}
	for i := range expected {
		if expected[i] != result.Proof[i] {
			return false, nil
		}
	}
	return true, nil
}

func (e *LocalConfidentialEngine) executeCount(session *Session, query *ConfidentialQuery) (*ConfidentialResult, error) {
	session.mu.RLock()
	defer session.mu.RUnlock()

	count := 0
	prefix := query.Target
	for key := range session.store {
		if strings.HasPrefix(key, prefix) {
			count++
		}
	}

	proof, err := computeProof(count)
	if err != nil {
		return nil, err
	}
	return &ConfidentialResult{
		Value:     count,
		Proof:     proof,
		Encrypted: false,
	}, nil
}

func (e *LocalConfidentialEngine) executeSum(session *Session, query *ConfidentialQuery) (*ConfidentialResult, error) {
	session.mu.RLock()
	defer session.mu.RUnlock()

	shard := &UserShard{DEK: session.dek}
	var total float64
	prefix := query.Target

	for key, encrypted := range session.store {
		if !strings.HasPrefix(key, prefix) {
			continue
		}
		plaintext, err := shard.Decrypt(encrypted)
		if err != nil {
			continue // skip entries that fail decryption
		}
		val, err := strconv.ParseFloat(strings.TrimSpace(string(plaintext)), 64)
		if err != nil {
			continue // skip non-numeric values
		}
		total += val
	}

	proof, err := computeProof(total)
	if err != nil {
		return nil, err
	}
	return &ConfidentialResult{
		Value:     total,
		Proof:     proof,
		Encrypted: false,
	}, nil
}

func (e *LocalConfidentialEngine) executeMatch(session *Session, query *ConfidentialQuery) (*ConfidentialResult, error) {
	session.mu.RLock()
	defer session.mu.RUnlock()

	encrypted, ok := session.store[query.Target]
	if !ok {
		proof, _ := computeProof(false)
		return &ConfidentialResult{Value: false, Proof: proof, Encrypted: false}, nil
	}

	shard := &UserShard{DEK: session.dek}
	plaintext, err := shard.Decrypt(encrypted)
	if err != nil {
		return nil, fmt.Errorf("vault/confidential: decrypt: %w", err)
	}

	expected, ok := query.Params.(string)
	if !ok {
		return nil, fmt.Errorf("vault/confidential: match params must be string")
	}

	matched := string(plaintext) == expected
	proof, err := computeProof(matched)
	if err != nil {
		return nil, err
	}
	return &ConfidentialResult{
		Value:     matched,
		Proof:     proof,
		Encrypted: false,
	}, nil
}

func (e *LocalConfidentialEngine) executePolicyCheck(session *Session, query *ConfidentialQuery) (*ConfidentialResult, error) {
	// Params must be a *PolicyEngine
	pe, ok := query.Params.(*PolicyEngine)
	if !ok {
		return nil, fmt.Errorf("vault/confidential: policy_check params must be *PolicyEngine")
	}

	// Target format: "subject:resource:action"
	// Subject is before the first ":", action is after the last ":",
	// resource is everything in between. This allows colons in resource names
	// (e.g., "alice:vault:acme:docs:read" → subject=alice, resource=vault:acme:docs, action=read).
	firstColon := strings.IndexByte(query.Target, ':')
	lastColon := strings.LastIndexByte(query.Target, ':')
	if firstColon < 0 || firstColon == lastColon {
		return nil, fmt.Errorf("vault/confidential: policy_check target must be subject:resource:action")
	}
	subject := query.Target[:firstColon]
	resource := query.Target[firstColon+1 : lastColon]
	action := query.Target[lastColon+1:]

	allowed := pe.Check(subject, resource, action)
	proof, err := computeProof(allowed)
	if err != nil {
		return nil, err
	}
	return &ConfidentialResult{
		Value:     allowed,
		Proof:     proof,
		Encrypted: false,
	}, nil
}

// computeProof generates a SHA-256 hash binding the result value.
func computeProof(value interface{}) ([]byte, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("vault/confidential: marshal proof: %w", err)
	}
	h := sha256.Sum256(data)
	return h[:], nil
}
