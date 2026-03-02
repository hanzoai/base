// Copyright (C) 2020-2026, Hanzo AI Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package kms provides a zero-knowledge encrypted secret management plugin
// for Hanzo Base with FHE query support.
//
// All encryption and decryption happens client-side using the KMS SDK.
// The MPC nodes only store encrypted blobs. Fields configured for encryption
// are transparently encrypted on write and decrypted on read via Base hooks.
package kms

import (
	"fmt"
	"os"
)

// Config configures the KMS plugin.
type Config struct {
	// Nodes is the list of MPC node addresses.
	// Example: ["https://kms-mpc-0:9651", "https://kms-mpc-1:9651", "https://kms-mpc-2:9651"]
	Nodes []string

	// OrgSlug is the organization identifier for key derivation.
	OrgSlug string

	// Threshold is the Shamir threshold (t-of-n) for MPC operations.
	Threshold int

	// Passphrase is used for dev/bootstrap only. In production, derive the
	// CEK from env or from a previous Unlock call. Never hardcode.
	Passphrase string

	// EncryptedCollections maps collection names to lists of fields that
	// should be transparently encrypted at rest.
	//
	// Example:
	//   map[string][]string{
	//       "credentials": {"api_key", "api_secret"},
	//       "tokens":      {"access_token", "refresh_token"},
	//   }
	EncryptedCollections map[string][]string

	// FHESearchable maps collection names to lists of fields that maintain
	// an FHE-encrypted index for equality and range queries without decrypting.
	//
	// Example:
	//   map[string][]string{
	//       "credentials": {"name"},
	//   }
	FHESearchable map[string][]string

	// Enabled controls whether the plugin is active (default true).
	Enabled bool

	// AutoUnlock controls whether the plugin unlocks on startup using
	// Passphrase or the KMS_PASSPHRASE env var (default false).
	// Only use for dev/testing. In production, use the /api/kms/unlock endpoint.
	AutoUnlock bool
}

// DefaultConfig returns a Config with sensible defaults from environment.
func DefaultConfig() Config {
	return Config{
		Enabled:              true,
		EncryptedCollections: make(map[string][]string),
		FHESearchable:        make(map[string][]string),
	}
}

// validate checks that the config is valid for plugin startup.
func (c *Config) validate() error {
	if len(c.Nodes) == 0 {
		return fmt.Errorf("kms: at least one node address is required")
	}
	if c.OrgSlug == "" {
		return fmt.Errorf("kms: org slug is required")
	}
	if c.Threshold < 1 {
		return fmt.Errorf("kms: threshold must be at least 1")
	}
	if c.Threshold > len(c.Nodes) {
		return fmt.Errorf("kms: threshold %d exceeds node count %d", c.Threshold, len(c.Nodes))
	}

	// Validate that FHE fields are a subset of encrypted fields.
	for col, fheFields := range c.FHESearchable {
		encFields, ok := c.EncryptedCollections[col]
		if !ok {
			return fmt.Errorf("kms: FHE collection %q must also be in EncryptedCollections", col)
		}
		encSet := make(map[string]struct{}, len(encFields))
		for _, f := range encFields {
			encSet[f] = struct{}{}
		}
		for _, f := range fheFields {
			if _, ok := encSet[f]; !ok {
				return fmt.Errorf("kms: FHE field %q.%q must also be in EncryptedCollections", col, f)
			}
		}
	}

	return nil
}

// resolvePassphrase returns the passphrase from config or environment.
func (c *Config) resolvePassphrase() string {
	if c.Passphrase != "" {
		return c.Passphrase
	}
	return os.Getenv("KMS_PASSPHRASE")
}
