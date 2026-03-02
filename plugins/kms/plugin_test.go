// Copyright (C) 2020-2026, Hanzo AI Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package kms

import (
	"encoding/base64"
	"strings"
	"testing"

	sdk "github.com/hanzoai/kms/sdk/go"
)

func TestConfigValidate(t *testing.T) {
	tests := []struct {
		name    string
		config  Config
		wantErr bool
	}{
		{
			name: "valid config",
			config: Config{
				Nodes:     []string{"https://node1:9651"},
				OrgSlug:   "test-org",
				Threshold: 1,
				EncryptedCollections: map[string][]string{
					"secrets": {"value"},
				},
			},
			wantErr: false,
		},
		{
			name: "no nodes",
			config: Config{
				OrgSlug:   "test-org",
				Threshold: 1,
			},
			wantErr: true,
		},
		{
			name: "no org slug",
			config: Config{
				Nodes:     []string{"n1"},
				Threshold: 1,
			},
			wantErr: true,
		},
		{
			name: "zero threshold",
			config: Config{
				Nodes:     []string{"n1"},
				OrgSlug:   "test-org",
				Threshold: 0,
			},
			wantErr: true,
		},
		{
			name: "threshold exceeds nodes",
			config: Config{
				Nodes:     []string{"n1"},
				OrgSlug:   "test-org",
				Threshold: 2,
			},
			wantErr: true,
		},
		{
			name: "fhe field not in encrypted",
			config: Config{
				Nodes:     []string{"n1"},
				OrgSlug:   "test-org",
				Threshold: 1,
				EncryptedCollections: map[string][]string{
					"secrets": {"value"},
				},
				FHESearchable: map[string][]string{
					"secrets": {"name"}, // "name" not in EncryptedCollections
				},
			},
			wantErr: true,
		},
		{
			name: "fhe collection not in encrypted",
			config: Config{
				Nodes:     []string{"n1"},
				OrgSlug:   "test-org",
				Threshold: 1,
				EncryptedCollections: map[string][]string{
					"secrets": {"value"},
				},
				FHESearchable: map[string][]string{
					"other": {"name"}, // "other" not in EncryptedCollections
				},
			},
			wantErr: true,
		},
		{
			name: "valid fhe config",
			config: Config{
				Nodes:     []string{"n1"},
				OrgSlug:   "test-org",
				Threshold: 1,
				EncryptedCollections: map[string][]string{
					"secrets": {"name", "value"},
				},
				FHESearchable: map[string][]string{
					"secrets": {"name"},
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	if !cfg.Enabled {
		t.Error("expected Enabled to be true")
	}
	if cfg.EncryptedCollections == nil {
		t.Error("expected EncryptedCollections to be non-nil")
	}
	if cfg.FHESearchable == nil {
		t.Error("expected FHESearchable to be non-nil")
	}
}

func TestIsEncryptedValue(t *testing.T) {
	tests := []struct {
		val  string
		want bool
	}{
		{"enc:v1:abc123", true},
		{"enc:v1:", false},            // too short — no data after prefix
		{"plaintext", false},          // no prefix
		{"", false},                   // empty
		{"enc:v1:a", true},            // minimal valid
		{"ENC:V1:abc", false},         // case sensitive
		{"enc:v2:abc", false},         // wrong version
	}

	for _, tt := range tests {
		got := isEncryptedValue(tt.val)
		if got != tt.want {
			t.Errorf("isEncryptedValue(%q) = %v, want %v", tt.val, got, tt.want)
		}
	}
}

func TestSealOpenField_RoundTrip(t *testing.T) {
	// Use the SDK's key derivation to get a valid 32-byte key.
	masterKey, err := sdk.DeriveMasterKey("test-passphrase", "test-org")
	if err != nil {
		t.Fatalf("derive master key: %v", err)
	}
	defer clear(masterKey)

	cek, err := sdk.DeriveCEK(masterKey, "test-org")
	if err != nil {
		t.Fatalf("derive CEK: %v", err)
	}
	defer clear(cek)

	plaintext := []byte("super-secret-api-key-12345")
	aad := []byte("test-org:secrets:record-1")

	// Seal
	encrypted, err := sealField(cek, plaintext, aad)
	if err != nil {
		t.Fatalf("sealField: %v", err)
	}

	// Verify ciphertext is different from plaintext.
	if string(encrypted) == string(plaintext) {
		t.Fatal("encrypted should differ from plaintext")
	}

	// Open
	decrypted, err := openField(cek, encrypted, aad)
	if err != nil {
		t.Fatalf("openField: %v", err)
	}

	if string(decrypted) != string(plaintext) {
		t.Errorf("decrypted = %q, want %q", string(decrypted), string(plaintext))
	}
}

func TestSealOpenField_WrongKey(t *testing.T) {
	mk1, _ := sdk.DeriveMasterKey("pass1", "org1")
	cek1, _ := sdk.DeriveCEK(mk1, "org1")
	defer clear(mk1)
	defer clear(cek1)

	mk2, _ := sdk.DeriveMasterKey("pass2", "org2")
	cek2, _ := sdk.DeriveCEK(mk2, "org2")
	defer clear(mk2)
	defer clear(cek2)

	plaintext := []byte("secret")
	aad := []byte("aad")

	encrypted, err := sealField(cek1, plaintext, aad)
	if err != nil {
		t.Fatalf("sealField: %v", err)
	}

	_, err = openField(cek2, encrypted, aad)
	if err == nil {
		t.Fatal("expected error decrypting with wrong key")
	}
}

func TestSealOpenField_WrongAAD(t *testing.T) {
	mk, _ := sdk.DeriveMasterKey("pass", "org")
	cek, _ := sdk.DeriveCEK(mk, "org")
	defer clear(mk)
	defer clear(cek)

	encrypted, err := sealField(cek, []byte("secret"), []byte("aad1"))
	if err != nil {
		t.Fatalf("sealField: %v", err)
	}

	_, err = openField(cek, encrypted, []byte("aad2"))
	if err == nil {
		t.Fatal("expected error decrypting with wrong AAD")
	}
}

func TestComputeFHEIndex_Deterministic(t *testing.T) {
	mk, _ := sdk.DeriveMasterKey("pass", "org")
	cek, _ := sdk.DeriveCEK(mk, "org")
	defer clear(mk)
	defer clear(cek)

	idx1, err := computeFHEIndex(cek, "org", "secrets", "name", "my-key")
	if err != nil {
		t.Fatalf("computeFHEIndex: %v", err)
	}

	idx2, err := computeFHEIndex(cek, "org", "secrets", "name", "my-key")
	if err != nil {
		t.Fatalf("computeFHEIndex: %v", err)
	}

	if base64.RawURLEncoding.EncodeToString(idx1) != base64.RawURLEncoding.EncodeToString(idx2) {
		t.Error("FHE index should be deterministic for same input")
	}
}

func TestComputeFHEIndex_DifferentValues(t *testing.T) {
	mk, _ := sdk.DeriveMasterKey("pass", "org")
	cek, _ := sdk.DeriveCEK(mk, "org")
	defer clear(mk)
	defer clear(cek)

	idx1, _ := computeFHEIndex(cek, "org", "secrets", "name", "key-a")
	idx2, _ := computeFHEIndex(cek, "org", "secrets", "name", "key-b")

	if base64.RawURLEncoding.EncodeToString(idx1) == base64.RawURLEncoding.EncodeToString(idx2) {
		t.Error("FHE index should differ for different values")
	}
}

func TestComputeFHEIndex_DifferentFields(t *testing.T) {
	mk, _ := sdk.DeriveMasterKey("pass", "org")
	cek, _ := sdk.DeriveCEK(mk, "org")
	defer clear(mk)
	defer clear(cek)

	idx1, _ := computeFHEIndex(cek, "org", "secrets", "name", "same-val")
	idx2, _ := computeFHEIndex(cek, "org", "secrets", "label", "same-val")

	if base64.RawURLEncoding.EncodeToString(idx1) == base64.RawURLEncoding.EncodeToString(idx2) {
		t.Error("FHE index should differ for different fields even with same value")
	}
}

func TestComputeFHEIndex_DifferentOrgs(t *testing.T) {
	mk1, _ := sdk.DeriveMasterKey("pass", "org-a")
	cek1, _ := sdk.DeriveCEK(mk1, "org-a")
	defer clear(mk1)
	defer clear(cek1)

	mk2, _ := sdk.DeriveMasterKey("pass", "org-b")
	cek2, _ := sdk.DeriveCEK(mk2, "org-b")
	defer clear(mk2)
	defer clear(cek2)

	idx1, _ := computeFHEIndex(cek1, "org-a", "secrets", "name", "same-val")
	idx2, _ := computeFHEIndex(cek2, "org-b", "secrets", "name", "same-val")

	if base64.RawURLEncoding.EncodeToString(idx1) == base64.RawURLEncoding.EncodeToString(idx2) {
		t.Error("FHE index should differ for different orgs")
	}
}

func TestEncryptedPrefix(t *testing.T) {
	if encryptedPrefix != "enc:v1:" {
		t.Errorf("unexpected prefix: %q", encryptedPrefix)
	}
}

func TestFHEIndexPrefix(t *testing.T) {
	if fheIndexPrefix != "fhe:v1:" {
		t.Errorf("unexpected prefix: %q", fheIndexPrefix)
	}
}

func TestResolvePassphrase(t *testing.T) {
	// Config passphrase takes priority.
	c := Config{Passphrase: "from-config"}
	if got := c.resolvePassphrase(); got != "from-config" {
		t.Errorf("expected 'from-config', got %q", got)
	}

	// Empty config falls back to env.
	c2 := Config{}
	t.Setenv("KMS_PASSPHRASE", "from-env")
	if got := c2.resolvePassphrase(); got != "from-env" {
		t.Errorf("expected 'from-env', got %q", got)
	}
}

func TestFieldEncoding_RoundTrip(t *testing.T) {
	mk, _ := sdk.DeriveMasterKey("test", "org")
	cek, _ := sdk.DeriveCEK(mk, "org")
	defer clear(mk)
	defer clear(cek)

	plaintext := "my-secret-value"
	aad := []byte("org:col:id")

	// Simulate what hooks.go does.
	encrypted, err := sealField(cek, []byte(plaintext), aad)
	if err != nil {
		t.Fatalf("sealField: %v", err)
	}

	encoded := encryptedPrefix + base64.RawURLEncoding.EncodeToString(encrypted)

	if !isEncryptedValue(encoded) {
		t.Fatal("encoded value should be detected as encrypted")
	}

	// Decode.
	ciphertext, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(encoded, encryptedPrefix))
	if err != nil {
		t.Fatalf("base64 decode: %v", err)
	}

	decrypted, err := openField(cek, ciphertext, aad)
	if err != nil {
		t.Fatalf("openField: %v", err)
	}

	if string(decrypted) != plaintext {
		t.Errorf("round-trip failed: got %q, want %q", string(decrypted), plaintext)
	}
}
