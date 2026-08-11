package org

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/hanzoai/base/core"
)

// TestOrgBaseIsEncryptedAtRest reads the bytes rather than the code path.
//
// "Encryption is wired" is a claim about a call; "this file is not a SQLite
// database" is a claim about the disk, and only the second one is worth
// anything. A plaintext SQLite file opens with the string "SQLite format 3" at
// offset zero, so its absence is the whole assertion — and its presence would
// mean a deployment that configured a master key was still writing tenant data
// in the clear.
func TestOrgBaseIsEncryptedAtRest(t *testing.T) {
	dir := t.TempDir()

	const master = "0123456789abcdef0123456789abcdef" // 32 bytes, as KMS hands out

	keyed := NewOrgDB(nil, master)
	dek, err := keyed.OrgDEK("acme")
	if err != nil {
		t.Fatal(err)
	}
	if dek == "" {
		t.Fatal("a master key was configured and no org key came back")
	}

	// The same org under a different master derives a different key: one
	// tenant's file must not open with another tenant's — or another
	// deployment's.
	other := NewOrgDB(nil, "fedcba9876543210fedcba9876543210")
	otherDEK, err := other.OrgDEK("acme")
	if err != nil {
		t.Fatal(err)
	}
	if otherDEK == dek {
		t.Fatal("two different master keys derived the same org key")
	}

	// And two orgs under one master differ, which is what makes a leaked key
	// worth one tenant instead of all of them.
	sibling, err := keyed.OrgDEK("globex")
	if err != nil {
		t.Fatal(err)
	}
	if sibling == dek {
		t.Fatal("two orgs derived the same key")
	}

	// Dev mode stays dev mode: no master, no key, no encryption, and no error.
	plain := NewOrgDB(nil, "")
	devDEK, err := plain.OrgDEK("acme")
	if err != nil {
		t.Fatalf("an unconfigured master key is dev mode, not a failure: %v", err)
	}
	if devDEK != "" {
		t.Fatalf("dev mode produced a key: %q", devDEK)
	}

	// Now the bytes. Open a keyed database through the same call the Base open
	// path uses, write something recognisable, close it, and look at the file.
	connect, err := encryptedConnect(keyed, "acme")
	if err != nil {
		t.Fatal(err)
	}
	if connect == nil {
		t.Fatal("a configured master key produced no encrypted connector")
	}

	path := filepath.Join(dir, "org.db")
	db, err := connect(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.NewQuery("CREATE TABLE secrets (v TEXT)").Execute(); err != nil {
		t.Fatal(err)
	}
	if _, err := db.NewQuery("INSERT INTO secrets (v) VALUES ('CANARY_PLAINTEXT')").Execute(); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) == 0 {
		t.Fatal("the keyed database wrote no file")
	}
	if bytes.HasPrefix(raw, []byte("SQLite format 3")) {
		t.Fatal("the file on disk is a plaintext SQLite database")
	}
	if bytes.Contains(raw, []byte("CANARY_PLAINTEXT")) {
		t.Fatal("a value written through the keyed handle is readable in the file")
	}

	// Control, so the two assertions above cannot pass for some unrelated
	// reason. Dev mode returns no connector, the Base opens on the default
	// plaintext path, and that file DOES carry the header and the value — which
	// is what makes their absence above evidence of encryption rather than
	// evidence of an empty file.
	control, err := encryptedConnect(plain, "acme")
	if err != nil {
		t.Fatal(err)
	}
	if control != nil {
		t.Fatal("dev mode produced an encrypted connector")
	}

	clearPath := filepath.Join(dir, "clear.db")
	clear, err := core.DefaultDBConnect(clearPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := clear.NewQuery("CREATE TABLE secrets (v TEXT)").Execute(); err != nil {
		t.Fatal(err)
	}
	if _, err := clear.NewQuery("INSERT INTO secrets (v) VALUES ('CANARY_PLAINTEXT')").Execute(); err != nil {
		t.Fatal(err)
	}
	if err := clear.Close(); err != nil {
		t.Fatal(err)
	}

	clearRaw, err := os.ReadFile(clearPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(clearRaw, []byte("SQLite format 3")) {
		t.Fatal("the control is not a plaintext SQLite file, so the check above proves nothing")
	}
	if !bytes.Contains(clearRaw, []byte("CANARY_PLAINTEXT")) {
		t.Fatal("the control does not carry the value, so the check above proves nothing")
	}
}
