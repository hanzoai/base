package encreplica

import (
	"context"
	"os"
	"path/filepath"
	"strings"
)

// LocalBlobs is a directory-backed Blobs for dev/test. Production swaps in an
// S3- or hanzoai/vfs-backed Blobs; the age encryption boundary lives in Client
// and is identical for every backend.
type LocalBlobs struct{ root string }

// NewLocalBlobs returns a directory-backed Blobs rooted at dir.
func NewLocalBlobs(dir string) *LocalBlobs { return &LocalBlobs{root: dir} }

func (b *LocalBlobs) osPath(key string) string {
	return filepath.Join(b.root, filepath.FromSlash(key))
}

// Put writes key atomically (temp + rename).
func (b *LocalBlobs) Put(_ context.Context, key string, data []byte) error {
	p := b.osPath(key)
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}

// Get returns key's bytes; a missing key yields an error satisfying
// errors.Is(err, os.ErrNotExist).
func (b *LocalBlobs) Get(_ context.Context, key string) ([]byte, error) {
	return os.ReadFile(b.osPath(key))
}

// List returns the full keys of the (non-directory) entries directly under
// prefix. A missing prefix yields an empty slice (not an error).
func (b *LocalBlobs) List(_ context.Context, prefix string) ([]string, error) {
	dir := b.osPath(strings.TrimSuffix(prefix, "/"))
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	keys := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || strings.HasSuffix(e.Name(), ".tmp") {
			continue
		}
		keys = append(keys, prefix+e.Name())
	}
	return keys, nil
}

// Delete removes key; a missing key is not an error.
func (b *LocalBlobs) Delete(_ context.Context, key string) error {
	err := os.Remove(b.osPath(key))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

// DeleteAll removes every blob for this tenant.
func (b *LocalBlobs) DeleteAll(_ context.Context) error {
	err := os.RemoveAll(b.root)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}
