// At-rest encryption for the per-tenant SQLite substrate.
//
// Every byte a tenant's SQLite DB writes to durable storage is encrypted
// with a key UNIQUE to that tenant's Key tuple (org / app / project / user),
// rooted in Hanzo KMS. There is exactly ONE at-rest encryption boundary:
// luxfi/age (post-quantum hybrid ML-KEM-768 + X25519) — the same primitive
// hanzoai/vfs and hanzoai/replicate speak — so the tenant key composes across
// the whole-file, block (vfs), and WAL-stream (replicate) paths without ever
// double-encrypting the same bytes.
//
// # Key hierarchy (KMS-rooted envelope encryption)
//
//	KMS Vault ── OrgRoot(orgID) ─▶ KEK_org          (32 random bytes, per org, in KMS)
//	                                  │  HKDF-SHA256(salt=domain, info=objectKey)
//	                                  ▼
//	                                 WK               (per-tenant wrapping key, ephemeral)
//	                                  │  AES-256-GCM(AAD=objectKey)
//	                                  ▼
//	     age Hybrid identity (random per DB) ──wrap──▶ sidecar {objectKey}.agekey
//	                    │
//	            Recipient / Identity  ── luxfi/age ──▶ encrypts the SQLite bytes
//
// The DB's data-encryption key is the age identity — a random per-DB value
// generated ONCE and stored only in wrapped form (the sidecar). KMS holds only
// the org KEK. Rotating the org KEK re-wraps the tiny sidecar; the DB
// ciphertext is never rewritten (see Keyring.Rotate).
//
// # Isolation invariant (proven in keyring_test.go)
//
// The FIRST-line cross-tenant boundary is the request's authenticated
// claims->OrgID binding, enforced in MultiTenantStore.Get/ForCtx: a caller
// scoped to org B cannot resolve an org A Key (Get returns ErrCrossTenant).
// Cryptographic separation is the SECOND line, not the first — one shared
// store process serves many orgs and CAN fetch any org's KEK, so the KEK does
// not by itself stop a caller who reaches Get with a foreign OrgID. Given
// correct scoping the crypto then guarantees isolation: KEK_A != KEK_B are
// distinct KMS secrets, each DB has an independent random age identity, and
// each sidecar is AAD-bound to its exact objectKey, so no wrapped blob can be
// replayed across tenants (proven in keyring_test.go).

package store

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"sync"

	"github.com/hanzoai/base/tools/filesystem"
	"github.com/luxfi/age"
)

const (
	// dekKeyLen is the AES-256 wrapping-key / KEK length in bytes.
	dekKeyLen = 32
	// wrapHKDFSalt domain-separates the store wrapping-key derivation from
	// every other HKDF use in the platform. Never reuse across domains.
	wrapHKDFSalt = "hanzo-base.store.tenant-wrap.v1"
	// wrapNonceLen is the AES-256-GCM nonce length for the wrap.
	wrapNonceLen = 12
	// sidecarSuffix is appended to a Key.ObjectKey() to locate its wrapped
	// key material in the object store.
	sidecarSuffix = ".agekey"
)

// ErrNoKeyMaterial is returned when a DB object exists but its wrapped-key
// sidecar cannot be produced — never silently fall back to plaintext.
var ErrNoKeyMaterial = errors.New("store: no wrapped key material for tenant")

// TenantKey is the per-tenant at-rest key: a post-quantum hybrid age identity.
// Recipient encrypts, Identity decrypts. Unique per Key tuple.
type TenantKey struct {
	Recipient age.Recipient
	Identity  age.Identity
}

// KeyProvider yields the per-tenant at-rest key for a Key. Implementations
// MUST return a key unique to k, rooted in KMS, and MUST NOT log key material.
// Resolve is safe for concurrent use.
type KeyProvider interface {
	Resolve(ctx context.Context, k Key) (*TenantKey, error)
}

// RootSource yields the org-scoped root key (KEK) — the single KMS-rooted
// master per org. Implementations MUST return exactly one stable 32-byte KEK
// per orgID and MUST NOT return a shared/global key. The KEK is secret; the
// source is the only component that ever touches KMS.
type RootSource interface {
	OrgRoot(ctx context.Context, orgID string) ([]byte, error)
}

// Keyring is the canonical KeyProvider: KMS-rooted envelope encryption with
// per-DB random age identities. Safe for concurrent use.
type Keyring struct {
	root    RootSource
	sidecar *filesystem.System

	mu    sync.Mutex
	cache map[Key]*TenantKey
}

// NewKeyring builds a Keyring over a RootSource (KMS) and an object store for
// the wrapped-key sidecars (the SAME durable store as the DBs in production).
func NewKeyring(root RootSource, sidecar *filesystem.System) (*Keyring, error) {
	if root == nil {
		return nil, errors.New("store: keyring RootSource is required")
	}
	if sidecar == nil {
		return nil, errors.New("store: keyring sidecar store is required")
	}
	return &Keyring{root: root, sidecar: sidecar, cache: make(map[Key]*TenantKey)}, nil
}

// Resolve returns the per-tenant key for k, loading-and-unwrapping the existing
// sidecar or generating-and-wrapping a fresh identity on first use. In normal
// operation the sidecar is written at first hydrate (before any DB object
// exists), so a DB object without a sidecar can only be hostile injection —
// which surfaces downstream as an age-decrypt failure (ErrCorruptDB).
func (kr *Keyring) Resolve(ctx context.Context, k Key) (*TenantKey, error) {
	if err := k.Valid(); err != nil {
		return nil, fmt.Errorf("store: keyring invalid key %s: %w", k, err)
	}
	kr.mu.Lock()
	defer kr.mu.Unlock()

	if tk, ok := kr.cache[k]; ok {
		return tk, nil
	}

	sk := k.ObjectKey() + sidecarSuffix
	blob, ok, err := kr.sidecarGet(sk)
	if err != nil {
		return nil, fmt.Errorf("store: keyring load sidecar %s: %w", sk, err)
	}

	var tk *TenantKey
	if ok {
		tk, err = kr.unwrap(ctx, k, blob)
	} else {
		tk, err = kr.generate(ctx, k, sk)
	}
	if err != nil {
		return nil, err
	}
	kr.cache[k] = tk
	return tk, nil
}

// Rotate re-wraps k's key material from prevRoot's KEK to newRoot's KEK,
// leaving the DB ciphertext AND the underlying age identity unchanged. This is
// how an org KEK rotation propagates: only the tiny sidecar is rewritten, so
// no DB is re-encrypted. After Rotate, the old KEK can no longer unwrap the
// sidecar. Idempotent per (k, newRoot).
func (kr *Keyring) Rotate(ctx context.Context, k Key, prevRoot, newRoot RootSource) error {
	if err := k.Valid(); err != nil {
		return fmt.Errorf("store: keyring rotate invalid key %s: %w", k, err)
	}
	sk := k.ObjectKey() + sidecarSuffix
	blob, ok, err := kr.sidecarGet(sk)
	if err != nil {
		return fmt.Errorf("store: keyring rotate load %s: %w", sk, err)
	}
	if !ok {
		return fmt.Errorf("%w: %s", ErrNoKeyMaterial, k)
	}

	aad := []byte(k.ObjectKey())

	prevWK, err := deriveWrapKey(ctx, prevRoot, k)
	if err != nil {
		return err
	}
	defer wipe(prevWK)
	plain, err := aeadOpen(prevWK, blob, aad)
	if err != nil {
		return fmt.Errorf("store: keyring rotate unwrap %s (wrong prev KEK or tampered): %w", k, err)
	}
	defer wipe(plain)

	newWK, err := deriveWrapKey(ctx, newRoot, k)
	if err != nil {
		return err
	}
	defer wipe(newWK)
	rewrapped, err := aeadSeal(newWK, plain, aad)
	if err != nil {
		return err
	}
	if err := kr.sidecar.Upload(rewrapped, sk); err != nil {
		return fmt.Errorf("store: keyring rotate persist %s: %w", sk, err)
	}

	kr.mu.Lock()
	delete(kr.cache, k)
	kr.mu.Unlock()
	return nil
}

// generate mints a fresh random hybrid identity, wraps it under the org KEK,
// and persists the sidecar. Caller holds kr.mu.
func (kr *Keyring) generate(ctx context.Context, k Key, sk string) (*TenantKey, error) {
	id, err := age.GenerateHybridIdentity()
	if err != nil {
		return nil, fmt.Errorf("store: keyring generate identity: %w", err)
	}
	idStr := []byte(id.String())
	defer wipe(idStr)

	wk, err := deriveWrapKey(ctx, kr.root, k)
	if err != nil {
		return nil, err
	}
	defer wipe(wk)

	blob, err := aeadSeal(wk, idStr, []byte(k.ObjectKey()))
	if err != nil {
		return nil, err
	}
	if err := kr.sidecar.Upload(blob, sk); err != nil {
		return nil, fmt.Errorf("store: keyring persist sidecar %s: %w", sk, err)
	}
	return &TenantKey{Recipient: id.Recipient(), Identity: id}, nil
}

// unwrap decrypts an existing sidecar under the org KEK. Caller holds kr.mu.
func (kr *Keyring) unwrap(ctx context.Context, k Key, blob []byte) (*TenantKey, error) {
	wk, err := deriveWrapKey(ctx, kr.root, k)
	if err != nil {
		return nil, err
	}
	defer wipe(wk)

	idStr, err := aeadOpen(wk, blob, []byte(k.ObjectKey()))
	if err != nil {
		return nil, fmt.Errorf("store: keyring unwrap %s (wrong org KEK or tampered sidecar): %w", k, err)
	}
	defer wipe(idStr)

	id, err := age.ParseHybridIdentity(string(idStr))
	if err != nil {
		return nil, fmt.Errorf("store: keyring parse identity %s: %w", k, err)
	}
	return &TenantKey{Recipient: id.Recipient(), Identity: id}, nil
}

// sidecarGet reads a wrapped-key blob; ok=false (nil err) means absent.
func (kr *Keyring) sidecarGet(sk string) (blob []byte, ok bool, err error) {
	exists, err := kr.sidecar.Exists(sk)
	if err != nil || !exists {
		return nil, false, err
	}
	r, err := kr.sidecar.GetReader(sk)
	if err != nil {
		return nil, false, err
	}
	defer r.Close()
	b, err := io.ReadAll(r)
	if err != nil {
		return nil, false, err
	}
	return b, true, nil
}

// deriveWrapKey computes the per-tenant AES-256 wrapping key from the org KEK.
// WK = HKDF-SHA256(secret=KEK_org, salt=domain, info=objectKey). The objectKey
// (which begins with the orgID and encodes scope + tenant id) binds the
// wrapping key to exactly one DB, so distinct tenants derive distinct keys.
func deriveWrapKey(ctx context.Context, root RootSource, k Key) ([]byte, error) {
	kek, err := root.OrgRoot(ctx, k.OrgID)
	if err != nil {
		return nil, fmt.Errorf("store: keyring org root %s: %w", k.OrgID, err)
	}
	defer wipe(kek)
	if len(kek) != dekKeyLen {
		return nil, fmt.Errorf("store: keyring org root must be %d bytes, got %d", dekKeyLen, len(kek))
	}
	wk, err := hkdf.Key(sha256.New, kek, []byte(wrapHKDFSalt), k.ObjectKey(), dekKeyLen)
	if err != nil {
		return nil, fmt.Errorf("store: keyring hkdf: %w", err)
	}
	return wk, nil
}

// aeadSeal AES-256-GCM-encrypts plaintext with a random nonce, binding aad.
// Output is nonce || ciphertext || tag.
func aeadSeal(key, plaintext, aad []byte) ([]byte, error) {
	gcm, err := newGCM(key)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, wrapNonceLen)
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("store: keyring nonce: %w", err)
	}
	return gcm.Seal(nonce, nonce, plaintext, aad), nil
}

// aeadOpen reverses aeadSeal, authenticating aad.
func aeadOpen(key, data, aad []byte) ([]byte, error) {
	gcm, err := newGCM(key)
	if err != nil {
		return nil, err
	}
	if len(data) < wrapNonceLen {
		return nil, errors.New("store: keyring wrapped blob too short")
	}
	return gcm.Open(nil, data[:wrapNonceLen], data[wrapNonceLen:], aad)
}

func newGCM(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("store: keyring aes: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("store: keyring gcm: %w", err)
	}
	return gcm, nil
}

// sealDB age-encrypts SQLite bytes to the tenant recipient (the sole at-rest
// boundary for the whole-file durable path).
func sealDB(tk *TenantKey, plain []byte) ([]byte, error) {
	var buf bytes.Buffer
	w, err := age.Encrypt(&buf, tk.Recipient)
	if err != nil {
		return nil, fmt.Errorf("store: age encrypt: %w", err)
	}
	if _, err := w.Write(plain); err != nil {
		return nil, fmt.Errorf("store: age write: %w", err)
	}
	if err := w.Close(); err != nil {
		return nil, fmt.Errorf("store: age finalize: %w", err)
	}
	return buf.Bytes(), nil
}

// openDBBytes age-decrypts SQLite bytes with the tenant identity.
func openDBBytes(tk *TenantKey, ciphertext []byte) ([]byte, error) {
	r, err := age.Decrypt(bytes.NewReader(ciphertext), tk.Identity)
	if err != nil {
		return nil, fmt.Errorf("store: age decrypt: %w", err)
	}
	return io.ReadAll(r)
}

// wipe zeroes key material. Best-effort; Go may keep copies, but we minimize
// the residency window for KEK/DEK/WK bytes.
func wipe(b []byte) {
	for i := range b {
		b[i] = 0
	}
}
