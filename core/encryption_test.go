package core

import (
	"encoding/hex"
	"testing"
)

func TestDeriveKey_Valid(t *testing.T) {
	masterKey := make([]byte, 32)
	for i := range masterKey {
		masterKey[i] = byte(i)
	}

	key, err := DeriveKey(masterKey, PrincipalOrg, "acme")
	if err != nil {
		t.Fatal(err)
	}
	if len(key) != 32 {
		t.Fatalf("expected 32 bytes, got %d", len(key))
	}
}

func TestDeriveKey_Deterministic(t *testing.T) {
	masterKey := make([]byte, 32)
	for i := range masterKey {
		masterKey[i] = byte(i)
	}

	key1, _ := DeriveKey(masterKey, PrincipalOrg, "acme")
	key2, _ := DeriveKey(masterKey, PrincipalOrg, "acme")

	if hex.EncodeToString(key1) != hex.EncodeToString(key2) {
		t.Fatal("same inputs should produce same key")
	}
}

func TestDeriveKey_UniquePerOrg(t *testing.T) {
	masterKey := make([]byte, 32)
	for i := range masterKey {
		masterKey[i] = byte(i)
	}

	key1, _ := DeriveKey(masterKey, PrincipalOrg, "acme")
	key2, _ := DeriveKey(masterKey, PrincipalOrg, "globex")

	if hex.EncodeToString(key1) == hex.EncodeToString(key2) {
		t.Fatal("different orgs should have different keys")
	}
}

func TestDeriveKey_DomainSeparation(t *testing.T) {
	masterKey := make([]byte, 32)
	for i := range masterKey {
		masterKey[i] = byte(i)
	}

	orgKey, _ := DeriveKey(masterKey, PrincipalOrg, "acme")
	userKey, _ := DeriveKey(masterKey, PrincipalUser, "acme")

	if hex.EncodeToString(orgKey) == hex.EncodeToString(userKey) {
		t.Fatal("org and user keys should differ even with same ID")
	}
}

func TestDeriveKey_InvalidMasterKeyLength(t *testing.T) {
	short := make([]byte, 16)
	if _, err := DeriveKey(short, PrincipalOrg, "acme"); err == nil {
		t.Fatal("should reject non-32-byte master key")
	}
}

func TestDeriveKey_EmptyPrincipalID(t *testing.T) {
	masterKey := make([]byte, 32)
	if _, err := DeriveKey(masterKey, PrincipalOrg, ""); err == nil {
		t.Fatal("should reject empty principal ID")
	}
}

func TestDeriveKeyHex(t *testing.T) {
	masterKey := make([]byte, 32)
	for i := range masterKey {
		masterKey[i] = byte(i)
	}

	hexKey, err := DeriveKeyHex(masterKey, PrincipalOrg, "acme")
	if err != nil {
		t.Fatal(err)
	}
	if len(hexKey) != 64 { // 32 bytes = 64 hex chars
		t.Fatalf("expected 64 hex chars, got %d", len(hexKey))
	}

	// Verify it decodes to 32 bytes
	decoded, err := hex.DecodeString(hexKey)
	if err != nil {
		t.Fatal(err)
	}
	if len(decoded) != 32 {
		t.Fatalf("expected 32 decoded bytes, got %d", len(decoded))
	}
}
