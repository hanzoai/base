package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strings"
)

// KeyType classifies a presented credential. bootnode accepts three credential
// shapes: its own project keys (bn_), IAM bearer JWTs, and IAM-managed API
// keys (pk-/sk-/hk-).
type KeyType int

const (
	// KeyUnknown is an unrecognized credential shape.
	KeyUnknown KeyType = iota
	// KeyBootnode is a bootnode-issued project key (bn_…), hashed in
	// _bootnode_api_keys.
	KeyBootnode
	// KeyIAMPublishable is an IAM publishable key (pk-…), read-only.
	KeyIAMPublishable
	// KeyIAMSecret is an IAM secret key (sk-…), full access.
	KeyIAMSecret
	// KeyIAMService is an IAM service key (hk-…), full access.
	KeyIAMService
	// KeyJWT is an IAM bearer JWT (three dot-separated segments).
	KeyJWT
)

// BootnodeKeyPrefix is the prefix for bootnode-issued project keys.
const BootnodeKeyPrefix = "bn_"

// keyPrefixLen is the number of leading characters stored for display.
const keyPrefixLen = 12

// ClassifyCredential determines the [KeyType] of a presented credential
// without any network call. isBearer indicates the credential arrived via an
// Authorization: Bearer header (vs an X-API-Key header).
func ClassifyCredential(cred string, isBearer bool) KeyType {
	switch {
	case strings.HasPrefix(cred, BootnodeKeyPrefix):
		return KeyBootnode
	case strings.HasPrefix(cred, "pk-"):
		return KeyIAMPublishable
	case strings.HasPrefix(cred, "sk-"):
		return KeyIAMSecret
	case strings.HasPrefix(cred, "hk-"):
		return KeyIAMService
	case isBearer || strings.Count(cred, ".") == 2:
		return KeyJWT
	default:
		return KeyUnknown
	}
}

// GenerateKey mints a new bootnode project API key. It returns the raw key
// (shown to the user exactly once), the salted hash to persist, and the prefix
// to persist for display.
//
// The raw key is 32 bytes of crypto/rand entropy, URL-safe base64 encoded,
// prefixed with bn_. The stored hash is SHA-256(rawKey + salt) — the same
// construction as the Python bootnode so previously-issued keys verify
// unchanged. SHA-256+salt (not bcrypt) is correct here: the key is a
// high-entropy random token, not a low-entropy human password, so the
// brute-force resistance bcrypt buys is irrelevant and its cost would only
// slow every authenticated request.
func GenerateKey(salt string) (rawKey, hash, prefix string, err error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", "", "", fmt.Errorf("auth: generate key entropy: %w", err)
	}
	rawKey = BootnodeKeyPrefix + base64.RawURLEncoding.EncodeToString(buf)
	hash = HashKey(rawKey, salt)
	prefix = rawKey
	if len(prefix) > keyPrefixLen {
		prefix = prefix[:keyPrefixLen]
	}
	return rawKey, hash, prefix, nil
}

// HashKey computes the salted SHA-256 hash of a raw key for storage and
// lookup. Deterministic: the same (rawKey, salt) always yields the same hash,
// which is required because the hash is the lookup index.
func HashKey(rawKey, salt string) string {
	sum := sha256.Sum256([]byte(rawKey + salt))
	return hex.EncodeToString(sum[:])
}

// VerifyKey reports whether rawKey hashes (with salt) to storedHash, using a
// constant-time comparison to avoid leaking the hash via timing.
func VerifyKey(rawKey, salt, storedHash string) bool {
	got := HashKey(rawKey, salt)
	return subtle.ConstantTimeCompare([]byte(got), []byte(storedHash)) == 1
}
