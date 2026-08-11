package org

import (
	"os"
	"testing"
)

// TestBaseReportsWhatIsOnDisk pins the one property that makes /v1/bases worth
// asking: it reports the filesystem, not a table.
//
// The handler it replaced read the local _orgs collection, which IAM owns and
// this package never writes — so it answered "no such org" for every org,
// including the caller's own, while that org's Base sat on disk being written
// to. A view built from a table nobody fills cannot be wrong in a way anyone
// notices; a view built from the file either finds it or does not.
func TestBaseReportsWhatIsOnDisk(t *testing.T) {
	db, _ := testOrgDB(t)
	p := &plugin{orgDB: db}

	// An org nobody has used yet: no Base, and that is an answer rather than
	// an error — it is what every org looks like before its first request.
	if got := p.base("acme"); got.Exists || got.Bytes != 0 {
		t.Fatalf("unused org: got %+v, want a Base that does not exist", got)
	}

	// A place to put one is not one. Provisioning makes the directory; opening
	// the Base makes the file, and only the file is a Base.
	if _, err := db.ProvisionOrg("acme"); err != nil {
		t.Fatal(err)
	}
	if got := p.base("acme"); got.Exists {
		t.Fatalf("an empty directory was reported as a Base: %+v", got)
	}

	if err := os.WriteFile(db.OrgDBPath("acme"), []byte("a base"), 0600); err != nil {
		t.Fatal(err)
	}

	got := p.base("acme")
	if !got.Exists {
		t.Fatalf("a Base on disk was reported missing: %+v", got)
	}
	if got.Org != "acme" {
		t.Fatalf("org %q, want acme", got.Org)
	}

	// A different org does not inherit it — the isolation is the whole point.
	if other := p.base("globex"); other.Exists {
		t.Fatalf("globex saw acme's Base: %+v", other)
	}
}
