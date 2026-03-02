package core

import (
	"crypto/hkdf"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

// PrincipalType identifies the type of principal for CEK derivation.
type PrincipalType string

const (
	PrincipalOrg  PrincipalType = "org"
	PrincipalUser PrincipalType = "user"
)

// DeriveKey derives a 256-bit Content Encryption Key for a principal
// from a master key using HKDF-SHA256 (RFC 5869).
//
// The info string is "{principalType}:{principalID}" ensuring domain
// separation between orgs and users even with identical IDs.
//
// Compatible with hanzoai/sqlite's CEK derivation.
func DeriveKey(masterKey []byte, principalType PrincipalType, principalID string) ([]byte, error) {
	if len(masterKey) != 32 {
		return nil, fmt.Errorf("core/cek: master key must be 32 bytes, got %d", len(masterKey))
	}
	if principalID == "" {
		return nil, fmt.Errorf("core/cek: principal ID cannot be empty")
	}

	info := string(principalType) + ":" + principalID
	key, err := hkdf.Key(sha256.New, masterKey, nil, info, 32)
	if err != nil {
		return nil, fmt.Errorf("core/cek: hkdf: %w", err)
	}
	return key, nil
}

// DeriveKeyHex derives a CEK and returns the hex-encoded string.
// Convenience wrapper for callers that need the key as a string.
func DeriveKeyHex(masterKey []byte, principalType PrincipalType, principalID string) (string, error) {
	key, err := DeriveKey(masterKey, principalType, principalID)
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(key), nil
}
