package core_test

import (
	"testing"

	"github.com/hanzoai/base/core"
)

func TestRecordEmail(t *testing.T) {
	record := core.NewRecord(core.NewAuthCollection("test"))

	if record.Email() != "" {
		t.Fatalf("Expected email %q, got %q", "", record.Email())
	}

	email := "test@example.com"
	record.SetEmail(email)

	if record.Email() != email {
		t.Fatalf("Expected email %q, got %q", email, record.Email())
	}
}

func TestRecordEmailVisibility(t *testing.T) {
	record := core.NewRecord(core.NewAuthCollection("test"))

	if record.EmailVisibility() != false {
		t.Fatalf("Expected emailVisibility %v, got %v", false, record.EmailVisibility())
	}

	record.SetEmailVisibility(true)

	if record.EmailVisibility() != true {
		t.Fatalf("Expected emailVisibility %v, got %v", true, record.EmailVisibility())
	}
}

func TestRecordVerified(t *testing.T) {
	record := core.NewRecord(core.NewAuthCollection("test"))

	if record.Verified() != false {
		t.Fatalf("Expected verified %v, got %v", false, record.Verified())
	}

	record.SetVerified(true)

	if record.Verified() != true {
		t.Fatalf("Expected verified %v, got %v", true, record.Verified())
	}
}

func TestRecordTokenKey(t *testing.T) {
	record := core.NewRecord(core.NewAuthCollection("test"))

	if record.TokenKey() != "" {
		t.Fatalf("Expected tokenKey %q, got %q", "", record.TokenKey())
	}

	tokenKey := "example"

	record.SetTokenKey(tokenKey)

	if record.TokenKey() != tokenKey {
		t.Fatalf("Expected tokenKey %q, got %q", tokenKey, record.TokenKey())
	}

	record.RefreshTokenKey()

	if record.TokenKey() == tokenKey {
		t.Fatalf("Expected tokenKey to be random generated, got %q", tokenKey)
	}

	if len(record.TokenKey()) != 50 {
		t.Fatalf("Expected %d characters, got %d", 50, len(record.TokenKey()))
	}
}
