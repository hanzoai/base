// Copyright (C) 2020-2026, Hanzo AI Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package vault

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sync"
	"time"
)

// Capability represents a delegated permission from one DID to another.
// Capabilities are signed by the issuer and scoped to specific resources/actions.
//
// Example:
//
//	cap := &Capability{
//	    Issuer:   "did:lux:org:acme",
//	    Subject:  "did:lux:user:alice",
//	    Resource: "vault:acme:*",
//	    Actions:  []string{"read", "write"},
//	    Expires:  time.Now().Add(24 * time.Hour),
//	}
type Capability struct {
	ID        string    // deterministic: SHA-256(issuer + subject + resource + actions)
	Issuer    string    // DID of the issuer
	Subject   string    // DID of the subject (who can use this)
	Resource  string    // vault ID, collection, or key pattern
	Actions   []string  // "read", "write", "sync", "anchor", "share"
	Expires   time.Time // zero value = never expires
	Signature []byte    // signed by issuer (verified externally)
	Revoked   bool      // set by Revoke()
}

// capID computes a deterministic ID for a capability.
func capID(issuer, subject, resource string, actions []string) string {
	h := sha256.New()
	h.Write([]byte(issuer))
	h.Write([]byte(subject))
	h.Write([]byte(resource))
	for _, a := range actions {
		h.Write([]byte(a))
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}

// PolicyEngine manages capability-based access control for vaults.
// Thread-safe. All capabilities are held in memory (persisted to vault shard
// in production via Put/Get).
type PolicyEngine struct {
	caps map[string]*Capability // capID → capability
	mu   sync.RWMutex
}

// NewPolicyEngine creates an empty policy engine.
func NewPolicyEngine() *PolicyEngine {
	return &PolicyEngine{
		caps: make(map[string]*Capability),
	}
}

// Grant issues a capability. The capability ID is computed deterministically.
// Returns an error if required fields are missing.
func (pe *PolicyEngine) Grant(cap *Capability) error {
	if cap.Issuer == "" {
		return fmt.Errorf("vault/policy: issuer required")
	}
	if cap.Subject == "" {
		return fmt.Errorf("vault/policy: subject required")
	}
	if cap.Resource == "" {
		return fmt.Errorf("vault/policy: resource required")
	}
	if len(cap.Actions) == 0 {
		return fmt.Errorf("vault/policy: at least one action required")
	}

	cap.ID = capID(cap.Issuer, cap.Subject, cap.Resource, cap.Actions)
	cap.Revoked = false

	pe.mu.Lock()
	pe.caps[cap.ID] = cap
	pe.mu.Unlock()

	return nil
}

// Check returns true if subject has the given action on resource.
// Checks expiry and revocation.
func (pe *PolicyEngine) Check(subject, resource, action string) bool {
	pe.mu.RLock()
	defer pe.mu.RUnlock()

	now := time.Now()
	for _, cap := range pe.caps {
		if cap.Revoked {
			continue
		}
		if cap.Subject != subject {
			continue
		}
		if !matchResource(cap.Resource, resource) {
			continue
		}
		if !cap.Expires.IsZero() && now.After(cap.Expires) {
			continue
		}
		for _, a := range cap.Actions {
			if a == action {
				return true
			}
		}
	}
	return false
}

// Revoke revokes a capability by ID.
func (pe *PolicyEngine) Revoke(capID string) error {
	pe.mu.Lock()
	defer pe.mu.Unlock()

	cap, ok := pe.caps[capID]
	if !ok {
		return fmt.Errorf("vault/policy: capability %q not found", capID)
	}
	cap.Revoked = true
	return nil
}

// List returns all non-revoked capabilities for a subject.
func (pe *PolicyEngine) List(subject string) []*Capability {
	pe.mu.RLock()
	defer pe.mu.RUnlock()

	var result []*Capability
	for _, cap := range pe.caps {
		if cap.Subject == subject && !cap.Revoked {
			result = append(result, cap)
		}
	}
	return result
}

// matchResource checks if a capability resource pattern matches a target resource.
// Supports trailing wildcard: "vault:org:*" matches "vault:org:anything".
func matchResource(pattern, target string) bool {
	if pattern == target {
		return true
	}
	if len(pattern) > 0 && pattern[len(pattern)-1] == '*' {
		prefix := pattern[:len(pattern)-1]
		return len(target) >= len(prefix) && target[:len(prefix)] == prefix
	}
	return false
}
