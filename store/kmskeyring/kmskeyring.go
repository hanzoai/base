// Package kmskeyring binds store.RootSource to Hanzo KMS.
//
// Each org's 32-byte KEK is held in KMS under one fixed secret name, scoped
// per org. The KEK is the ONLY key material in KMS; the per-DB data keys are
// random age identities wrapped under it by store.Keyring and stored as
// sidecars next to the DBs (envelope encryption). Rotating an org KEK in KMS
// therefore never rewrites a DB — see store.Keyring.Rotate.
//
// # Wiring
//
// kmskeyring depends only on the minimal SecretStore contract so the store
// substrate stays orthogonal to any specific KMS transport. Production wires
// the canonical Base→KMS facade (plugins/platform.KMSClient) with a trivial
// adapter:
//
//	type kmsAdapter struct{ c *platform.KMSClient }
//	func (a kmsAdapter) Get(_ context.Context, org, name string) (string, error) {
//	    v, err := a.c.GetSecret(org, name)
//	    if isNotFound(err) { return "", kmskeyring.ErrNotFound }
//	    return v, err
//	}
//	func (a kmsAdapter) Put(_ context.Context, org, name, val string) error {
//	    return a.c.SetSecret(org, name, val)
//	}
//
//	src, _ := kmskeyring.New(kmsAdapter{c: kmsClient})
//	keyring, _ := store.NewKeyring(src, objectStore)
//	mtStore, _ := store.New(store.Options{ObjectStore: objectStore, Keys: keyring})
package kmskeyring

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
)

// KEKSecretName is the fixed KMS secret leaf holding an org's store KEK. One
// KEK per org; the secret's org scoping is the SecretStore's responsibility.
const KEKSecretName = "base/store-kek"

// kekLen is the KEK length in bytes (AES-256).
const kekLen = 32

// ErrNotFound signals an absent secret. Adapters MUST map their backend's
// not-found to this (via errors.Is) so OrgRoot can create-on-first-use.
var ErrNotFound = errors.New("kmskeyring: secret not found")

// SecretStore is the minimal, per-org KMS surface kmskeyring needs.
// Implementations are goroutine-safe. Get returns ErrNotFound (errors.Is)
// when the named secret is absent for the org.
type SecretStore interface {
	Get(ctx context.Context, orgID, name string) (string, error)
	Put(ctx context.Context, orgID, name, value string) error
}

// Source implements store.RootSource over a SecretStore.
type Source struct {
	kms  SecretStore
	name string
}

// New builds a Source over kms, using the canonical KEK secret name.
func New(kms SecretStore) (*Source, error) {
	if kms == nil {
		return nil, errors.New("kmskeyring: SecretStore is required")
	}
	return &Source{kms: kms, name: KEKSecretName}, nil
}

// OrgRoot returns the org's 32-byte KEK, creating and persisting a fresh
// random KEK in KMS on first use, and returning a fresh copy each call (the
// caller may zero it).
//
// Create-on-first-use is a convenience for the lazy path; the correct
// production posture is to PROVISION the KEK once at org-creation time under a
// single writer (or via a KMS compare-and-set Put) so two pods can never race
// two different KEKs into existence. On a create race with a last-writer-wins
// backend, both pods converge by re-reading the authoritative value here.
func (s *Source) OrgRoot(ctx context.Context, orgID string) ([]byte, error) {
	if orgID == "" {
		return nil, errors.New("kmskeyring: empty orgID")
	}

	val, err := s.kms.Get(ctx, orgID, s.name)
	if err == nil {
		return decodeKEK(orgID, val)
	}
	if !errors.Is(err, ErrNotFound) {
		return nil, fmt.Errorf("kmskeyring: get KEK for %s: %w", orgID, err)
	}

	// Absent — mint and persist a fresh KEK.
	kek := make([]byte, kekLen)
	if _, err := rand.Read(kek); err != nil {
		return nil, fmt.Errorf("kmskeyring: generate KEK: %w", err)
	}
	if err := s.kms.Put(ctx, orgID, s.name, hex.EncodeToString(kek)); err != nil {
		return nil, fmt.Errorf("kmskeyring: persist KEK for %s: %w", orgID, err)
	}

	// Re-read the authoritative value to converge if another pod won the
	// create race; adopt whichever KEK KMS now holds.
	val, err = s.kms.Get(ctx, orgID, s.name)
	if err != nil {
		return nil, fmt.Errorf("kmskeyring: reread KEK for %s: %w", orgID, err)
	}
	return decodeKEK(orgID, val)
}

// decodeKEK hex-decodes and length-checks a stored KEK.
func decodeKEK(orgID, val string) ([]byte, error) {
	kek, err := hex.DecodeString(val)
	if err != nil {
		return nil, fmt.Errorf("kmskeyring: decode KEK for %s: %w", orgID, err)
	}
	if len(kek) != kekLen {
		return nil, fmt.Errorf("kmskeyring: KEK for %s must be %d bytes, got %d", orgID, kekLen, len(kek))
	}
	return kek, nil
}
