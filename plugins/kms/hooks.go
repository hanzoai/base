// Copyright (C) 2020-2026, Hanzo AI Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package kms

import (
	"encoding/base64"
	"fmt"

	"github.com/hanzoai/base/core"
	"github.com/hanzoai/base/tools/hook"
)

// registerHooks binds record lifecycle hooks for transparent field-level
// encryption. Configured fields are encrypted before write and decrypted
// after read, making encryption invisible to the application layer.
func (p *plugin) registerHooks() {
	// Encrypt on create (before DB write).
	p.app.OnRecordCreate().Bind(&hook.Handler[*core.RecordEvent]{
		Id: "__kmsEncryptCreate__",
		Func: func(e *core.RecordEvent) error {
			if err := p.encryptFields(e.Record); err != nil {
				return fmt.Errorf("kms: encrypt on create: %w", err)
			}
			if err := p.updateFHEIndex(e.Record); err != nil {
				p.logger.Warn("kms: fhe index update on create failed", "error", err)
			}
			return e.Next()
		},
		Priority: -50, // Run before most user hooks.
	})

	// Encrypt on update (before DB write).
	p.app.OnRecordUpdate().Bind(&hook.Handler[*core.RecordEvent]{
		Id: "__kmsEncryptUpdate__",
		Func: func(e *core.RecordEvent) error {
			if err := p.encryptFields(e.Record); err != nil {
				return fmt.Errorf("kms: encrypt on update: %w", err)
			}
			if err := p.updateFHEIndex(e.Record); err != nil {
				p.logger.Warn("kms: fhe index update on update failed", "error", err)
			}
			return e.Next()
		},
		Priority: -50,
	})

	// Decrypt on enrich (after DB read, before API response).
	p.app.OnRecordEnrich().Bind(&hook.Handler[*core.RecordEnrichEvent]{
		Id: "__kmsDecryptEnrich__",
		Func: func(e *core.RecordEnrichEvent) error {
			if err := p.decryptFields(e.Record); err != nil {
				p.logger.Warn("kms: decrypt on enrich failed",
					"collection", e.Record.Collection().Name,
					"record", e.Record.Id,
					"error", err,
				)
				// Don't fail the request — return encrypted data rather than error.
			}
			return e.Next()
		},
		Priority: -50,
	})
}

// encryptFields encrypts the configured fields on a record in-place.
// Fields are stored as base64-encoded ciphertext in the DB.
func (p *plugin) encryptFields(record *core.Record) error {
	col := record.Collection().Name
	fields, ok := p.encFieldSet[col]
	if !ok {
		return nil
	}

	if !p.client.IsUnlocked() {
		return fmt.Errorf("kms client is locked — cannot encrypt fields for %q", col)
	}

	cek, err := p.client.ExportCEK()
	if err != nil {
		return fmt.Errorf("get CEK: %w", err)
	}
	defer clear(cek)

	aad := []byte(p.config.OrgSlug + ":" + col + ":" + record.Id)

	for field := range fields {
		val := record.GetString(field)
		if val == "" {
			continue
		}

		// Skip if already encrypted (base64 with our prefix).
		if isEncryptedValue(val) {
			continue
		}

		encrypted, err := sealField(cek, []byte(val), aad)
		if err != nil {
			return fmt.Errorf("encrypt field %q: %w", field, err)
		}

		record.Set(field, encryptedPrefix+base64.RawURLEncoding.EncodeToString(encrypted))
	}

	return nil
}

// decryptFields decrypts the configured fields on a record in-place.
func (p *plugin) decryptFields(record *core.Record) error {
	col := record.Collection().Name
	fields, ok := p.encFieldSet[col]
	if !ok {
		return nil
	}

	if !p.client.IsUnlocked() {
		return nil // Return encrypted data if locked.
	}

	cek, err := p.client.ExportCEK()
	if err != nil {
		return fmt.Errorf("get CEK: %w", err)
	}
	defer clear(cek)

	aad := []byte(p.config.OrgSlug + ":" + col + ":" + record.Id)

	for field := range fields {
		val := record.GetString(field)
		if val == "" {
			continue
		}

		if !isEncryptedValue(val) {
			continue
		}

		ciphertext, err := base64.RawURLEncoding.DecodeString(val[len(encryptedPrefix):])
		if err != nil {
			return fmt.Errorf("decode field %q: %w", field, err)
		}

		plaintext, err := openField(cek, ciphertext, aad)
		if err != nil {
			return fmt.Errorf("decrypt field %q: %w", field, err)
		}

		record.Set(field, string(plaintext))
	}

	return nil
}

const encryptedPrefix = "enc:v1:"

// isEncryptedValue checks if a value has our encryption prefix.
func isEncryptedValue(val string) bool {
	return len(val) > len(encryptedPrefix) && val[:len(encryptedPrefix)] == encryptedPrefix
}
