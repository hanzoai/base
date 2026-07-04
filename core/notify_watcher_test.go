package core_test

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	"github.com/hanzoai/dbx"
	"github.com/hanzoai/base/core"
	"github.com/hanzoai/base/tools/store"
	"golang.org/x/sync/semaphore"
)

func TestNotifyWatcher_SettingsUpdate(t *testing.T) {
	t.Parallel()

	testEvents := store.New[core.App, int](nil)

	tmpDir, err := os.MkdirTemp("", "hz_notify_test*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	app1 := core.NewBaseApp(core.BaseAppConfig{
		DataDir: tmpDir,
	})
	if err := app1.Bootstrap(); err != nil {
		t.Fatal(err)
	}

	app2 := core.NewBaseApp(core.BaseAppConfig{
		DataDir: tmpDir,
	})
	if err := app2.Bootstrap(); err != nil {
		t.Fatal(err)
	}

	ctx, cancelCtx := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancelCtx()

	sem := semaphore.NewWeighted(1)
	sem.Acquire(ctx, 1)

	app1.OnSettingsReload().BindFunc(func(e *core.SettingsReloadEvent) error {
		testEvents.SetFunc(app1, func(old int) int {
			return old + 1
		})
		return e.Next()
	})

	app2.OnSettingsReload().BindFunc(func(e *core.SettingsReloadEvent) error {
		// Apply the reload (e.Next swaps in the new settings) BEFORE releasing the
		// barrier, so the assertions below cannot observe app2 mid-reload with the
		// old AppName still in place.
		err := e.Next()
		testEvents.SetFunc(app2, func(old int) int {
			sem.Release(1)
			return old + 1
		})
		return err
	})

	// updating app1 settings should trigger a reload in app2
	app1.Settings().Meta.AppName = "notify-probe"
	if err := app1.Save(app1.Settings()); err != nil {
		t.Fatal(err)
	}

	// block until released or timeouted
	sem.Acquire(ctx, 1)

	if app1Total := testEvents.Get(app1); app1Total != 1 {
		t.Fatalf("Expected 1 app1 event, got %d", app1Total)
	}

	if app2Total := testEvents.Get(app2); app2Total != 1 {
		t.Fatalf("Expected 1 app2 event, got %d", app2Total)
	}

	if name := app2.Settings().Meta.AppName; name != "notify-probe" {
		t.Fatalf("Expected app2 settings event to carry AppName %q, got %q", "notify-probe", name)
	}
}

func TestNotifyWatcher_CollectionsUpdate(t *testing.T) {
	t.Parallel()

	tmpDir, err := os.MkdirTemp("", "hz_notify_test*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	app1 := core.NewBaseApp(core.BaseAppConfig{
		DataDir: tmpDir,
	})
	if err := app1.Bootstrap(); err != nil {
		t.Fatal(err)
	}

	app2 := core.NewBaseApp(core.BaseAppConfig{
		DataDir: tmpDir,
	})
	if err := app2.Bootstrap(); err != nil {
		t.Fatal(err)
	}

	testQueries := store.New[string, []string](nil)
	app2.ConcurrentDB().(*dbx.DB).QueryLogFunc = func(ctx context.Context, t time.Duration, sql string, rows *sql.Rows, err error) {
		testQueries.SetFunc("concurrent", func(old []string) []string {
			return append(old, sql)
		})
	}
	app2.ConcurrentDB().(*dbx.DB).ExecLogFunc = func(ctx context.Context, t time.Duration, sql string, result sql.Result, err error) {
		testQueries.SetFunc("concurrent", func(old []string) []string {
			return append(old, sql)
		})
	}
	app2.NonconcurrentDB().(*dbx.DB).QueryLogFunc = func(ctx context.Context, t time.Duration, sql string, rows *sql.Rows, err error) {
		testQueries.SetFunc("nonconcurrent", func(old []string) []string {
			return append(old, sql)
		})
	}
	app2.NonconcurrentDB().(*dbx.DB).ExecLogFunc = func(ctx context.Context, t time.Duration, sql string, result sql.Result, err error) {
		testQueries.SetFunc("nonconcurrent", func(old []string) []string {
			return append(old, sql)
		})
	}

	// create/update/delete app1 collections should trigger a reload in app2
	dummyCollection := core.NewBaseCollection("test")
	if err := app1.Save(dummyCollection); err != nil {
		t.Fatal(err)
	}
	dummyCollection.Fields.Add(&core.TextField{Name: "test"})
	if err := app1.Save(dummyCollection); err != nil {
		t.Fatal(err)
	}
	if err := app1.Delete(dummyCollection); err != nil {
		t.Fatal(err)
	}

	// There is no reload hook, so poll instead: wait until at least one reload has
	// been observed and the count has then stayed stable for a quiescence window,
	// or until the deadline. This replaces a brittle "release the instant the
	// count hits 1, then assert it is exactly 1" handshake that flaked to 0 (the
	// reload had not landed yet on a loaded runner) or 2 (a late coalesced reload
	// raced in between the release and the assertion).
	deadline := time.Now().Add(10 * time.Second)
	const settle = 400 * time.Millisecond
	stableSince := time.Now()
	last := -1
	for time.Now().Before(deadline) {
		if n := len(testQueries.Get("concurrent")); n != last {
			last, stableSince = n, time.Now()
		} else if n >= 1 && time.Since(stableSince) >= settle {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	nonconcurrentQueries := testQueries.Get("nonconcurrent")
	concurrentQueries := testQueries.Get("concurrent")

	if len(nonconcurrentQueries) != 0 {
		t.Fatalf("reload must use the concurrent (read) pool, not the write pool; got %d write queries (%v)", len(nonconcurrentQueries), nonconcurrentQueries)
	}
	// Coalescing invariant: three rapid changes must reload app2 FEWER than three
	// times — ideally once; a slow runner may split the burst into two. Zero means
	// app2 never observed the change; three would mean no coalescing at all.
	if n := len(concurrentQueries); n < 1 || n > 2 {
		t.Fatalf("expected the change burst to coalesce into 1-2 reloads, got %d (%v)", n, concurrentQueries)
	}

	expectedQuery := "SELECT {{_collections}}.* FROM `_collections` ORDER BY `rowid` ASC"
	if concurrentQueries[0] != expectedQuery {
		t.Fatalf("Expected query\n%s\ngot\n%s", expectedQuery, concurrentQueries[0])
	}
}
