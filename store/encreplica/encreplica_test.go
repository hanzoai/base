package encreplica_test

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/hanzoai/base/store/encreplica"
	"github.com/luxfi/age"
)

func TestLocalBlobs_PutGetListDelete(t *testing.T) {
	ctx := context.Background()
	b := encreplica.NewLocalBlobs(t.TempDir())

	if err := b.Put(ctx, "ltx/0/a", []byte("one")); err != nil {
		t.Fatal(err)
	}
	if err := b.Put(ctx, "ltx/0/b", []byte("two")); err != nil {
		t.Fatal(err)
	}
	got, err := b.Get(ctx, "ltx/0/a")
	if err != nil || string(got) != "one" {
		t.Fatalf("get: %q %v", got, err)
	}

	keys, err := b.List(ctx, "ltx/0/")
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 2 {
		t.Fatalf("list: want 2, got %d (%v)", len(keys), keys)
	}

	if err := b.Delete(ctx, "ltx/0/a"); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Get(ctx, "ltx/0/a"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("deleted key must be ErrNotExist, got %v", err)
	}
}

func TestLocalBlobs_MissingIsErrNotExistAndEmptyList(t *testing.T) {
	ctx := context.Background()
	b := encreplica.NewLocalBlobs(t.TempDir())
	if _, err := b.Get(ctx, "ltx/0/missing"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing key must be ErrNotExist, got %v", err)
	}
	keys, err := b.List(ctx, "ltx/9/")
	if err != nil {
		t.Fatalf("list of missing prefix must not error: %v", err)
	}
	if len(keys) != 0 {
		t.Fatalf("list of missing prefix must be empty, got %v", keys)
	}
}

func TestNew_RequiresBackendAndKey(t *testing.T) {
	id, err := age.GenerateHybridIdentity()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := encreplica.New(nil, id.Recipient(), id); err == nil {
		t.Fatal("New must require a backend")
	}
	if _, err := encreplica.New(encreplica.NewLocalBlobs(t.TempDir()), nil, id); err == nil {
		t.Fatal("New must require a recipient")
	}
	if _, err := encreplica.New(encreplica.NewLocalBlobs(t.TempDir()), id.Recipient(), nil); err == nil {
		t.Fatal("New must require an identity")
	}
}
