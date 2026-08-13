package org

import (
	"os"
	"sync"
	"testing"
	"time"

	"github.com/hanzoai/base/core"
	"github.com/hanzoai/base/tests"
)

// Opening an org's Base is a migration run on a fresh file, and it is by far
// the longest thing the registry does. This pins that it happens off the path
// every other org takes: while one org is being opened for the first time, a
// lookup of an already-open Base answers at once.
//
// The lock the registry needs is the one over its map. Holding it across the
// open instead makes any org's first request the whole process's latency.
func TestOpeningOneBaseDoesNotStallAnother(t *testing.T) {
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatal(err)
	}
	defer app.Cleanup()

	b := newBases(&plugin{app: app, orgDB: NewOrgDB(app, "")})
	defer b.close()

	if _, err := b.base("warm"); err != nil {
		t.Fatal(err)
	}

	// Time one cold open, so what follows is measured against this machine
	// rather than against a constant.
	start := time.Now()
	if _, err := b.base("first"); err != nil {
		t.Fatal(err)
	}
	cold := time.Since(start)

	done := make(chan struct{})
	go func() {
		defer close(done)
		if _, err := b.base("second"); err != nil {
			t.Error(err)
		}
	}()

	var worst time.Duration
	start = time.Now()
loop:
	for {
		select {
		case <-done:
			break loop
		default:
		}
		at := time.Now()
		if _, err := b.base("warm"); err != nil {
			t.Fatal(err)
		}
		if d := time.Since(at); d > worst {
			worst = d
		}
	}
	overlap := time.Since(start)

	// If the second open finished before anything could be measured against
	// it, the run proves nothing and says so rather than passing.
	if overlap < cold/2 {
		t.Fatalf("the second open finished in %v against a cold open of %v — too fast to observe", overlap, cold)
	}
	if worst > cold/2 {
		t.Fatalf("a lookup of an open Base waited %v while another org opened, which takes %v", worst, cold)
	}
}

// Two callers naming the same org get the same Base, and it is opened once.
// The registry is what makes an org's Base a single thing; opening it twice
// would put two sets of handles on one file.
func TestOneBasePerOrg(t *testing.T) {
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatal(err)
	}
	defer app.Cleanup()

	b := newBases(&plugin{app: app, orgDB: NewOrgDB(app, "")})
	defer b.close()

	const callers = 8
	got := make([]core.App, callers)

	var wg sync.WaitGroup
	wg.Add(callers)
	for i := range got {
		go func() {
			defer wg.Done()
			app, err := b.base("acme")
			if err != nil {
				t.Error(err)
				return
			}
			got[i] = app
		}()
	}
	wg.Wait()

	for i, app := range got {
		if app == nil {
			t.Fatalf("caller %d got no Base", i)
		}
		if app != got[0] {
			t.Fatalf("caller %d got a second Base for one org", i)
		}
	}
}

// A Base that could not be opened is not remembered as one. Keeping the failure
// would answer every later request for that org with it, so a transient reason
// would outlive itself and last the life of the process.
func TestAFailedOpenIsNotRemembered(t *testing.T) {
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatal(err)
	}
	defer app.Cleanup()

	orgDB := NewOrgDB(app, "")
	b := newBases(&plugin{app: app, orgDB: orgDB})
	defer b.close()

	// A file where the org's directory belongs: nothing can be provisioned
	// there, and the open fails.
	if err := os.MkdirAll(orgDB.OrgsDir(), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(orgDB.OrgDir("acme"), []byte("not a directory"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := b.base("acme"); err == nil {
		t.Fatal("opening a Base over a file succeeded")
	}

	b.mu.RLock()
	_, remembered := b.open["acme"]
	b.mu.RUnlock()
	if remembered {
		t.Fatal("a Base that failed to open was remembered")
	}
}
