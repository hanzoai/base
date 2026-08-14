package org

import (
	"testing"

	"github.com/hanzoai/base/tests"
)

// A Base looks after itself. The periodic checkpoint and the log cleanup are
// registered where a Base is constructed and where its logger is built, and a
// schedule starts ticking when it is added — so an org's Base keeps its own
// write-ahead log checkpointed and its own logs inside its own retention,
// without anything serving on it.
//
// The alternative reading is easy to arrive at and wrong: __hzCronStart__ is
// bound on OnServe, and a tenant's Base is bootstrapped and never served, so it
// looks as though its schedules never start. Start resumes what Stop paused;
// nothing here is ever paused. The two schedules below are what would silently
// stop if that ever became true, and neither failure announces itself — a Base
// that is not checkpointed grows a write-ahead log, and one whose logs are not
// cleaned keeps them past the days its settings allow.
//
// The autobackup schedule is the third of the set and is absent until a Base's
// settings name a cron for it, which is the same on every Base.
func TestATenantsBaseMaintainsItself(t *testing.T) {
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatal(err)
	}
	defer app.Cleanup()

	b := newBases(&plugin{app: app, orgDB: NewOrgDB(app, "")})

	tenant, err := b.base("acme")
	if err != nil {
		t.Fatal(err)
	}

	for _, id := range []string{"__hzDBOptimize__", "__hzLogsCleanup__"} {
		if !tenant.Cron().HasJob(id) {
			t.Fatalf("an org's Base does not run %s, so nothing maintains it", id)
		}
	}

	if tenant.Cron().HasJob("__hzAutoBackup__") {
		t.Fatal("an org's Base scheduled a backup its settings did not ask for")
	}

	// Registered is not running. This is the half that the OnServe reading
	// would take away.
	if !tenant.Cron().HasStarted() {
		t.Fatal("an org's Base registered its maintenance and never started it")
	}

	// And they belong to the Base: closing it stops them, so a process that
	// opens many orgs is not left ticking for the ones it has released.
	b.close()

	if tenant.Cron().HasStarted() {
		t.Fatal("an org's Base kept ticking after it was closed")
	}
}
