package kmskeyring_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/hanzoai/base/store/kmskeyring"
)

// mockKMS is an in-memory SecretStore: a per-(org,name) string map that
// returns ErrNotFound on miss, exactly like the real KMS contract.
type mockKMS struct {
	mu   sync.Mutex
	data map[string]string
	puts int
}

func newMockKMS() *mockKMS { return &mockKMS{data: make(map[string]string)} }

func (m *mockKMS) key(org, name string) string { return org + "\x00" + name }

func (m *mockKMS) Get(_ context.Context, org, name string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	v, ok := m.data[m.key(org, name)]
	if !ok {
		return "", kmskeyring.ErrNotFound
	}
	return v, nil
}

func (m *mockKMS) Put(_ context.Context, org, name, value string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.data[m.key(org, name)] = value
	m.puts++
	return nil
}

func TestOrgRoot_CreateThenReuse(t *testing.T) {
	kms := newMockKMS()
	src, err := kmskeyring.New(kms)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	k1, err := src.OrgRoot(ctx, "acme")
	if err != nil {
		t.Fatal(err)
	}
	if len(k1) != 32 {
		t.Fatalf("KEK len = %d, want 32", len(k1))
	}

	k2, err := src.OrgRoot(ctx, "acme")
	if err != nil {
		t.Fatal(err)
	}
	if string(k1) != string(k2) {
		t.Fatal("second OrgRoot returned a different KEK — not stable")
	}
	if kms.puts != 1 {
		t.Fatalf("expected exactly 1 Put (create-once), got %d", kms.puts)
	}
}

func TestOrgRoot_ReturnsCopy(t *testing.T) {
	kms := newMockKMS()
	src, _ := kmskeyring.New(kms)
	ctx := context.Background()
	k1, _ := src.OrgRoot(ctx, "acme")
	for i := range k1 {
		k1[i] = 0 // caller zeroes its copy
	}
	k2, _ := src.OrgRoot(ctx, "acme")
	allZero := true
	for _, b := range k2 {
		if b != 0 {
			allZero = false
			break
		}
	}
	if allZero {
		t.Fatal("OrgRoot handed back the caller's zeroed slice — not a copy")
	}
}

func TestOrgRoot_DistinctOrgsDistinctKEKs(t *testing.T) {
	kms := newMockKMS()
	src, _ := kmskeyring.New(kms)
	ctx := context.Background()
	a, _ := src.OrgRoot(ctx, "orga")
	b, _ := src.OrgRoot(ctx, "orgb")
	if string(a) == string(b) {
		t.Fatal("distinct orgs share a KEK")
	}
}

func TestOrgRoot_EmptyOrgRejected(t *testing.T) {
	src, _ := kmskeyring.New(newMockKMS())
	if _, err := src.OrgRoot(context.Background(), ""); err == nil {
		t.Fatal("empty orgID must be rejected")
	}
}

func TestOrgRoot_RejectsMalformedKEK(t *testing.T) {
	kms := newMockKMS()
	// Plant a non-hex value under the KEK name.
	_ = kms.Put(context.Background(), "acme", kmskeyring.KEKSecretName, "not-hex-zzzz")
	src, _ := kmskeyring.New(kms)
	if _, err := src.OrgRoot(context.Background(), "acme"); err == nil {
		t.Fatal("malformed (non-hex) KEK must be rejected")
	}
}

func TestOrgRoot_RejectsWrongLengthKEK(t *testing.T) {
	kms := newMockKMS()
	_ = kms.Put(context.Background(), "acme", kmskeyring.KEKSecretName, "abcd") // 2 bytes
	src, _ := kmskeyring.New(kms)
	if _, err := src.OrgRoot(context.Background(), "acme"); err == nil {
		t.Fatal("wrong-length KEK must be rejected")
	}
}

// errKMS returns a non-notfound error to prove OrgRoot does NOT create on
// transient backend errors (fail secure — never mint a second KEK).
type errKMS struct{}

func (errKMS) Get(context.Context, string, string) (string, error) {
	return "", errors.New("kms transport down")
}
func (errKMS) Put(context.Context, string, string, string) error { return nil }

func TestOrgRoot_DoesNotCreateOnTransientError(t *testing.T) {
	src, _ := kmskeyring.New(errKMS{})
	if _, err := src.OrgRoot(context.Background(), "acme"); err == nil {
		t.Fatal("transient KMS error must not be swallowed into a KEK create")
	}
}
