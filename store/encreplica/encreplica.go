// Package encreplica is a replicate.ReplicaClient that PQ-encrypts every LTX
// segment with a per-tenant age key BEFORE it touches durable storage, and
// decrypts on read — the SOLE at-rest boundary for the replica stream, using
// the SAME key as the whole-file path (store.TenantKey). This is what makes
// hanzoai/replicate safe for multi-tenant data: without it the replica stream
// is plaintext and every tenant's SQLite pages hit the backend unencrypted.
//
// # Why a client and not replicate's built-in age
//
// replicate v0.8.0's Replica.AgeRecipients encrypts the LTX BEFORE the file/s3
// client calls ltx.PeekHeader for a timestamp, so those clients reject the
// ciphertext. Encrypting INSIDE the client — after the plaintext header is
// read — is both the working path and the correct single-boundary design.
//
// # On-storage framing (self-describing, backend-agnostic)
//
//	[8 bytes BE plaintext length][8 bytes BE unix-milli timestamp][age ciphertext]
//
// The plaintext length and timestamp are read from the plaintext LTX header at
// write time and stored in the clear PREFIX so LTXFiles can report an accurate
// FileInfo (size + CreatedAt) — which replicate's restore requires (it checks
// Size >= ltx.HeaderSize and drives a resumable reader off Size) — WITHOUT
// leaking any tenant data (the LTX body is entirely inside the age ciphertext).
//
// The backend is any path-addressed Blobs store: local-fs for dev/test, S3 or
// hanzoai/vfs in production (durability/scale, Pillar 3). Encryption is in this
// client, identical for every backend.
package encreplica

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"path"
	"time"

	"github.com/hanzoai/ltx"
	"github.com/hanzoai/replicate"
	"github.com/luxfi/age"
)

// prefixLen is the plaintext-length + timestamp header stored before the age
// ciphertext (two big-endian int64s).
const prefixLen = 16

var _ replicate.ReplicaClient = (*Client)(nil)

// Blobs is the minimal path-addressed byte store the client persists to.
// Keys use "/" separators. Get returns an error satisfying errors.Is(err,
// os.ErrNotExist) when the key is absent. Implementations are goroutine-safe.
type Blobs interface {
	Put(ctx context.Context, key string, data []byte) error
	Get(ctx context.Context, key string) ([]byte, error)
	List(ctx context.Context, prefix string) ([]string, error)
	Delete(ctx context.Context, key string) error
	DeleteAll(ctx context.Context) error
}

// Client is the per-tenant age-encrypting replica client.
type Client struct {
	blobs     Blobs
	recipient age.Recipient
	identity  age.Identity
	logger    *slog.Logger
}

// New builds an encrypting client over blobs for one tenant. recipient
// encrypts, identity decrypts — both are the tenant's key (store.TenantKey).
func New(blobs Blobs, recipient age.Recipient, identity age.Identity) (*Client, error) {
	if blobs == nil {
		return nil, errors.New("encreplica: blobs backend is required")
	}
	if recipient == nil || identity == nil {
		return nil, errors.New("encreplica: tenant recipient and identity are required")
	}
	return &Client{blobs: blobs, recipient: recipient, identity: identity, logger: slog.Default()}, nil
}

// Type identifies this client.
func (c *Client) Type() string { return "encrypted-age" }

// SetLogger sets the logger.
func (c *Client) SetLogger(l *slog.Logger) { c.logger = l }

// Init is a no-op — the backend is created lazily on first Put.
func (c *Client) Init(ctx context.Context) error { return nil }

// key returns the storage key for an LTX file.
func (c *Client) key(level int, minTXID, maxTXID ltx.TXID) string {
	return path.Join("ltx", fmt.Sprintf("%d", level), ltx.FormatFilename(minTXID, maxTXID))
}

// levelPrefix returns the storage prefix for a level.
func (c *Client) levelPrefix(level int) string {
	return path.Join("ltx", fmt.Sprintf("%d", level)) + "/"
}

// WriteLTXFile reads the plaintext LTX, records size+timestamp from its header,
// age-encrypts the body, and stores [prefix][ciphertext].
func (c *Client) WriteLTXFile(ctx context.Context, level int, minTXID, maxTXID ltx.TXID, rd io.Reader) (*ltx.FileInfo, error) {
	plain, err := io.ReadAll(rd)
	if err != nil {
		return nil, fmt.Errorf("encreplica: read plaintext ltx: %w", err)
	}
	hdr, _, err := ltx.PeekHeader(bytes.NewReader(plain))
	if err != nil {
		return nil, fmt.Errorf("encreplica: peek ltx header: %w", err)
	}

	ct, err := ageSeal(c.recipient, plain)
	if err != nil {
		return nil, err
	}

	blob := make([]byte, prefixLen+len(ct))
	binary.BigEndian.PutUint64(blob[0:8], uint64(len(plain)))
	binary.BigEndian.PutUint64(blob[8:16], uint64(hdr.Timestamp))
	copy(blob[prefixLen:], ct)

	if err := c.blobs.Put(ctx, c.key(level, minTXID, maxTXID), blob); err != nil {
		return nil, fmt.Errorf("encreplica: put: %w", err)
	}
	return &ltx.FileInfo{
		Level:     level,
		MinTXID:   minTXID,
		MaxTXID:   maxTXID,
		Size:      int64(len(plain)),
		CreatedAt: time.UnixMilli(hdr.Timestamp).UTC(),
	}, nil
}

// OpenLTXFile returns a reader over the DECRYPTED LTX plaintext, honoring the
// requested offset/size (into the plaintext).
func (c *Client) OpenLTXFile(ctx context.Context, level int, minTXID, maxTXID ltx.TXID, offset, size int64) (io.ReadCloser, error) {
	blob, err := c.blobs.Get(ctx, c.key(level, minTXID, maxTXID))
	if err != nil {
		return nil, err // preserves os.ErrNotExist for replicate
	}
	plainLen, _, ct, err := decodeBlob(blob)
	if err != nil {
		return nil, err
	}
	plain, err := ageOpen(c.identity, ct)
	if err != nil {
		return nil, fmt.Errorf("encreplica: decrypt ltx %d/%s-%s (wrong tenant key or tampered): %w",
			level, minTXID, maxTXID, err)
	}
	if int64(len(plain)) != plainLen {
		return nil, fmt.Errorf("encreplica: ltx length mismatch: header=%d decrypted=%d", plainLen, len(plain))
	}
	if offset > int64(len(plain)) {
		offset = int64(len(plain))
	}
	plain = plain[offset:]
	if size > 0 && size < int64(len(plain)) {
		plain = plain[:size]
	}
	return io.NopCloser(bytes.NewReader(plain)), nil
}

// LTXFiles enumerates stored LTX files at a level, reading the cleartext prefix
// (size + timestamp) so restore gets accurate FileInfo without decrypting.
func (c *Client) LTXFiles(ctx context.Context, level int, seek ltx.TXID, useMetadata bool) (ltx.FileIterator, error) {
	keys, err := c.blobs.List(ctx, c.levelPrefix(level))
	if err != nil {
		return nil, err
	}
	infos := make([]*ltx.FileInfo, 0, len(keys))
	for _, k := range keys {
		minTXID, maxTXID, perr := ltx.ParseFilename(path.Base(k))
		if perr != nil || minTXID < seek {
			continue
		}
		blob, gerr := c.blobs.Get(ctx, k)
		if gerr != nil {
			return nil, gerr
		}
		plainLen, tsMillis, _, derr := decodeBlob(blob)
		if derr != nil {
			return nil, derr
		}
		infos = append(infos, &ltx.FileInfo{
			Level:     level,
			MinTXID:   minTXID,
			MaxTXID:   maxTXID,
			Size:      plainLen,
			CreatedAt: time.UnixMilli(tsMillis).UTC(),
		})
	}
	return ltx.NewFileInfoSliceIterator(infos), nil
}

// DeleteLTXFiles removes the named LTX files.
func (c *Client) DeleteLTXFiles(ctx context.Context, a []*ltx.FileInfo) error {
	for _, info := range a {
		if err := c.blobs.Delete(ctx, c.key(info.Level, info.MinTXID, info.MaxTXID)); err != nil {
			return err
		}
	}
	return nil
}

// DeleteAll removes every LTX file for this tenant.
func (c *Client) DeleteAll(ctx context.Context) error { return c.blobs.DeleteAll(ctx) }

// decodeBlob splits the stored [plainLen][ts][ciphertext] framing.
func decodeBlob(blob []byte) (plainLen, tsMillis int64, ct []byte, err error) {
	if len(blob) < prefixLen {
		return 0, 0, nil, fmt.Errorf("encreplica: blob too short (%d < %d)", len(blob), prefixLen)
	}
	plainLen = int64(binary.BigEndian.Uint64(blob[0:8]))
	tsMillis = int64(binary.BigEndian.Uint64(blob[8:16]))
	return plainLen, tsMillis, blob[prefixLen:], nil
}

// ageSeal age-encrypts plaintext to the tenant recipient.
func ageSeal(recipient age.Recipient, plaintext []byte) ([]byte, error) {
	var buf bytes.Buffer
	w, err := age.Encrypt(&buf, recipient)
	if err != nil {
		return nil, fmt.Errorf("encreplica: age encrypt: %w", err)
	}
	if _, err := w.Write(plaintext); err != nil {
		return nil, fmt.Errorf("encreplica: age write: %w", err)
	}
	if err := w.Close(); err != nil {
		return nil, fmt.Errorf("encreplica: age finalize: %w", err)
	}
	return buf.Bytes(), nil
}

// ageOpen age-decrypts ciphertext with the tenant identity.
func ageOpen(identity age.Identity, ciphertext []byte) ([]byte, error) {
	r, err := age.Decrypt(bytes.NewReader(ciphertext), identity)
	if err != nil {
		return nil, fmt.Errorf("encreplica: age decrypt: %w", err)
	}
	return io.ReadAll(r)
}
