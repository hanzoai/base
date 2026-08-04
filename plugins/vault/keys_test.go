package vault

import (
	"bytes"
	"testing"

	"github.com/hanzoai/cek"
	"github.com/hanzoai/namespace"
)

// TestVaultKey_IsCEKDerivation states the derivation as an assertion rather
// than as a comment. This plugin's package doc has claimed a key hierarchy in
// prose before while the code did something else; prose cannot go red.
func TestVaultKey_IsCEKDerivation(t *testing.T) {
	master := testMasterKey()
	const org, user = "did:lux:org:acme", "did:lux:user:alice"

	ns, err := namespace.OrgProject(org, user)
	if err != nil {
		t.Fatalf("namespace.OrgProject: %v", err)
	}
	want, err := cek.DeriveKey(master, ns, userSubsystem)
	if err != nil {
		t.Fatalf("cek.DeriveKey: %v", err)
	}

	got, err := vaultKey(master, org, user, userSubsystem)
	if err != nil {
		t.Fatalf("vaultKey: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("user DEK is not the cek derivation:\n got %x\nwant %x", got, want)
	}
	if len(got) != cek.KeyLen {
		t.Fatalf("expected a %d-byte key, got %d", cek.KeyLen, len(got))
	}
}

// The org is in the namespace, which is what replaced the org KEK. Without it,
// two orgs that use the same user id would open each other's vaults.
func TestVaultKey_BoundToOrg(t *testing.T) {
	master := testMasterKey()
	const user = "did:lux:user:alice"

	a, err := vaultKey(master, "did:lux:org:acme", user, userSubsystem)
	if err != nil {
		t.Fatalf("vaultKey: %v", err)
	}
	b, err := vaultKey(master, "did:lux:org:globex", user, userSubsystem)
	if err != nil {
		t.Fatalf("vaultKey: %v", err)
	}
	if bytes.Equal(a, b) {
		t.Fatal("the same user id in two orgs must not derive the same key")
	}
}

// A user vault and a shared vault of the same name are different files and must
// be different keys; the subsystem is what separates them.
func TestVaultKey_UserVsShared(t *testing.T) {
	master := testMasterKey()
	const org, name = "did:lux:org:acme", "widgets"

	user, err := vaultKey(master, org, name, userSubsystem)
	if err != nil {
		t.Fatalf("vaultKey: %v", err)
	}
	shared, err := vaultKey(master, org, name, sharedSubsystem)
	if err != nil {
		t.Fatalf("vaultKey: %v", err)
	}
	if bytes.Equal(user, shared) {
		t.Fatal("a user vault and a shared vault of the same name must not share a key")
	}
}

// Sanitize is injective, and this is the property that matters for a key: two
// DIDs that differ only in punctuation must not fold onto one vault.
func TestVaultKey_DistinctIDsDistinctKeys(t *testing.T) {
	master := testMasterKey()
	const org = "did:lux:org:acme"

	seen := map[string]string{}
	for _, id := range []string{
		"did:lux:user:alice",
		"did-lux-user-alice",
		"did:lux:user:Alice",
		"did:lux:user:alice2",
	} {
		k, err := vaultKey(master, org, id, userSubsystem)
		if err != nil {
			t.Fatalf("vaultKey(%q): %v", id, err)
		}
		if prev, dup := seen[string(k)]; dup {
			t.Fatalf("%q and %q derived the same key", prev, id)
		}
		seen[string(k)] = id
	}
}
