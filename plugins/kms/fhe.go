// Copyright (C) 2020-2026, Hanzo AI Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package kms

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"fmt"

	"github.com/hanzoai/base/core"
	sdk "github.com/hanzoai/kms/sdk/go"
)

// FHE index prefix for stored index values.
const fheIndexPrefix = "fhe:v1:"

// updateFHEIndex maintains deterministic encrypted indexes for FHE-searchable
// fields. These indexes support equality checks and prefix queries without
// decrypting the underlying data.
//
// The index is a deterministic HMAC-SHA256 of the plaintext value keyed by
// a field-specific subkey derived from the CEK. This allows the server to
// evaluate equality gates on encrypted indexes without seeing plaintext.
//
// Index key derivation:
//
//	field_key = HMAC-SHA256(CEK, org_slug + ":" + collection + ":" + field)
//	index     = HMAC-SHA256(field_key, plaintext_value)
//
// The index is stored alongside the record in a synthetic field named
// "_fhe_{field}" which can be queried with standard Base filters.
func (p *plugin) updateFHEIndex(record *core.Record) error {
	col := record.Collection().Name
	fheFields, ok := p.fheFieldSet[col]
	if !ok {
		return nil
	}

	if !p.client.IsUnlocked() {
		return fmt.Errorf("kms client is locked — cannot update FHE index for %q", col)
	}

	cek, err := p.client.ExportCEK()
	if err != nil {
		return fmt.Errorf("get CEK for FHE index: %w", err)
	}
	defer clear(cek)

	for field := range fheFields {
		val := record.GetString(field)
		if val == "" {
			continue
		}

		// If the value is already encrypted, we cannot index it.
		// The index must be computed from the plaintext value before encryption.
		// Since updateFHEIndex is called before encryptFields in the hook chain
		// (same priority, but called first), the value should still be plaintext.
		if isEncryptedValue(val) {
			continue
		}

		indexValue, err := computeFHEIndex(cek, p.config.OrgSlug, col, field, val)
		if err != nil {
			return fmt.Errorf("compute FHE index for %q.%q: %w", col, field, err)
		}

		indexFieldName := "_fhe_" + field
		record.Set(indexFieldName, fheIndexPrefix+base64.RawURLEncoding.EncodeToString(indexValue))
	}

	return nil
}

// computeFHEIndex computes a deterministic encrypted index for a value.
// The result is an HMAC-SHA256 that allows equality comparison without
// revealing the plaintext.
func computeFHEIndex(cek []byte, orgSlug, collection, field, value string) ([]byte, error) {
	// Derive a field-specific subkey.
	fieldKeyMAC := hmac.New(sha256.New, cek)
	fieldKeyMAC.Write([]byte(orgSlug + ":" + collection + ":" + field))
	fieldKey := fieldKeyMAC.Sum(nil)

	// Compute deterministic index.
	indexMAC := hmac.New(sha256.New, fieldKey)
	indexMAC.Write([]byte(value))
	return indexMAC.Sum(nil), nil
}

// ComputeSearchToken generates an FHE search token for a given plaintext
// query value. The token can be compared against stored FHE indexes
// for equality matching without decrypting the stored data.
//
// Usage:
//
//	token, err := plugin.ComputeSearchToken("credentials", "name", "my-api-key")
//	// Use token in query: app.FindAll("credentials", "_fhe_name = ?", token)
func (p *plugin) ComputeSearchToken(collection, field, value string) (string, error) {
	cek, err := p.client.ExportCEK()
	if err != nil {
		return "", fmt.Errorf("get CEK for search token: %w", err)
	}
	defer clear(cek)

	index, err := computeFHEIndex(cek, p.config.OrgSlug, collection, field, value)
	if err != nil {
		return "", fmt.Errorf("compute search token: %w", err)
	}

	return fheIndexPrefix + base64.RawURLEncoding.EncodeToString(index), nil
}

// sealField encrypts a field value using AES-256-GCM via the KMS SDK.
func sealField(cek, plaintext, aad []byte) ([]byte, error) {
	return sdk.SealAESGCM(cek, plaintext, aad)
}

// openField decrypts a field value using AES-256-GCM via the KMS SDK.
func openField(cek, ciphertext, aad []byte) ([]byte, error) {
	return sdk.OpenAESGCM(cek, ciphertext, aad)
}
