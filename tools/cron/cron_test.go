package cron

import (
	"encoding/json"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hanzoai/base/tools/tasks"
)

func TestCronNew(t *testing.T) {
	t.Parallel()

	c := New()
	defer c.Stop()

	if c.Total() != 0 {
		t.Fatalf("expected no jobs on fresh cron, got %d", c.Total())
	}
	if c.HasStarted() {
		t.Fatal("expected HasStarted=false before any Add")
	}
}

// TestCronSetInterval verifies SetInterval is a safe no-op (superseded by
// per-schedule durations on tasks.Client).
func TestCronSetInterval(t *testing.T) {
	t.Parallel()

	c := New()
	defer c.Stop()
	c.SetInterval(2 * time.Minute) // must not panic
}

// TestCronSetTimezone verifies SetTimezone accepts a zone without erroring.
func TestCronSetTimezone(t *testing.T) {
	t.Parallel()

	c := New()
	defer c.Stop()
	tz, err := time.LoadLocation("Asia/Tokyo")
	if err != nil {
		t.Fatal(err)
	}
	c.SetTimezone(tz)   // must not panic
	c.SetTimezone(nil)  // must not panic
}

func TestCronAddAndRemove(t *testing.T) {
	t.Parallel()

	c := New()
	defer c.Stop()

	if err := c.Add("test0", "* * * * *", nil); err == nil {
		t.Fatal("expected nil-fn error")
	}
	if err := c.Add("test1", "invalid", func() {}); err == nil {
		t.Fatal("expected invalid cron expression error")
	}

	for _, id := range []string{"test2", "test3", "test4"} {
		if err := c.Add(id, "* * * * *", func() {}); err != nil {
			t.Fatal(err)
		}
	}

	// Overwrite test2 — should not duplicate.
	if err := c.Add("test2", "1 2 3 4 5", func() {}); err != nil {
		t.Fatal(err)
	}
	if err := c.Add("test5", "1 2 3 4 5", func() {}); err != nil {
		t.Fatal(err)
	}

	c.Remove("test4")
	c.Remove("missing") // no-op

	expectedIds := []string{"test2", "test3", "test5"}
	if c.Total() != len(expectedIds) {
		t.Fatalf("expected %d jobs, got %d", len(expectedIds), c.Total())
	}
	for _, id := range expectedIds {
		if !c.HasJob(id) {
			t.Fatalf("expected HasJob(%q)=true", id)
		}
	}
	if c.HasJob("test4") {
		t.Fatal("expected HasJob(test4)=false after Remove")
	}
}

func TestCronMustAdd(t *testing.T) {
	t.Parallel()

	c := New()
	defer c.Stop()

	defer func() {
		if r := recover(); r == nil {
			t.Errorf("MustAdd didn't panic on nil fn")
		}
	}()

	c.MustAdd("withfn", "* * * * *", func() {})
	if !c.HasJob("withfn") {
		t.Fatal("MustAdd did not register job")
	}
	c.MustAdd("nilfn", "* * * * *", nil) // must panic
}

func TestCronRemoveAll(t *testing.T) {
	t.Parallel()

	c := New()
	defer c.Stop()

	for _, id := range []string{"t1", "t2", "t3"} {
		if err := c.Add(id, "* * * * *", func() {}); err != nil {
			t.Fatal(err)
		}
	}
	if c.Total() != 3 {
		t.Fatalf("expected 3 jobs, got %d", c.Total())
	}
	c.RemoveAll()
	if c.Total() != 0 {
		t.Fatalf("expected 0 jobs after RemoveAll, got %d", c.Total())
	}
}

func TestCronTotal(t *testing.T) {
	t.Parallel()

	c := New()
	defer c.Stop()

	if v := c.Total(); v != 0 {
		t.Fatalf("expected 0, got %d", v)
	}
	_ = c.Add("a", "* * * * *", func() {})
	_ = c.Add("b", "* * * * *", func() {})
	_ = c.Add("a", "* * * * *", func() {}) // overwrite
	if v := c.Total(); v != 2 {
		t.Fatalf("expected 2, got %d", v)
	}
}

func TestCronJobs(t *testing.T) {
	t.Parallel()

	c := New()
	defer c.Stop()

	var mu sync.Mutex
	calls := ""

	if err := c.Add("a", "1 * * * *", func() { mu.Lock(); calls += "a"; mu.Unlock() }); err != nil {
		t.Fatal(err)
	}
	if err := c.Add("b", "2 * * * *", func() { mu.Lock(); calls += "b"; mu.Unlock() }); err != nil {
		t.Fatal(err)
	}
	// Overwrite b — its callback is replaced; original callback must not fire.
	if err := c.Add("b", "3 * * * *", func() { mu.Lock(); calls += "b"; mu.Unlock() }); err != nil {
		t.Fatal(err)
	}

	jobs := c.Jobs()
	if len(jobs) != 2 {
		t.Fatalf("expected 2 jobs, got %d", len(jobs))
	}

	// MarshalJSON — admin endpoint contract.
	slices.SortFunc(jobs, func(x, y *Job) int {
		if x.Id() < y.Id() {
			return -1
		}
		if x.Id() > y.Id() {
			return 1
		}
		return 0
	})
	out, err := json.Marshal(jobs)
	if err != nil {
		t.Fatal(err)
	}
	want := `[{"id":"a","expression":"1 * * * *"},{"id":"b","expression":"3 * * * *"}]`
	if string(out) != want {
		t.Fatalf("unexpected json\n got: %s\nwant: %s", out, want)
	}

	for _, j := range jobs {
		j.Run()
	}

	mu.Lock()
	defer mu.Unlock()
	if calls != "ab" {
		t.Fatalf("expected calls=ab, got %q", calls)
	}
}

// TestCronDelegatesToTasks verifies that cron.Add ends up invoking the
// callback the tasks.Client registered — i.e. the shim is not just
// bookkeeping, the ticker actually fires.
func TestCronDelegatesToTasks(t *testing.T) {
	t.Parallel()

	c := New()
	defer c.Stop()

	var hits atomic.Int64
	if err := c.Add("delegates", "20ms", func() { hits.Add(1) }); err != nil {
		t.Fatal(err)
	}

	// Should tick >=3 times over 100ms on a 20ms schedule.
	deadline := time.Now().Add(250 * time.Millisecond)
	for time.Now().Before(deadline) {
		if hits.Load() >= 3 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	if got := hits.Load(); got < 3 {
		t.Fatalf("expected >=3 ticks, got %d", got)
	}

	c.Remove("delegates")
	before := hits.Load()
	time.Sleep(60 * time.Millisecond)
	after := hits.Load()
	if after > before+1 {
		// Allow one in-flight tick but no further ticking.
		t.Fatalf("ticker continued after Remove: %d -> %d", before, after)
	}
}

// TestCronNewFromTasksShared verifies NewFromTasks wraps an existing client
// and that Stop() pauses without dropping registry entries — matching the
// legacy cron.Cron semantics.
func TestCronNewFromTasksShared(t *testing.T) {
	t.Parallel()

	tc := tasks.New("", "", nil)
	defer tc.Stop()

	c := NewFromTasks(tc)

	if err := c.Add("shared", "* * * * *", func() {}); err != nil {
		t.Fatal(err)
	}
	if !tc.HasJob("shared") {
		t.Fatal("expected tasks client to hold the registered schedule")
	}

	c.Stop()
	if !tc.HasJob("shared") {
		t.Fatal("Stop must pause tickers without removing registry entries")
	}
	if c.HasStarted() {
		t.Fatal("expected HasStarted=false after Stop")
	}

	c.Start()
	if !c.HasStarted() {
		t.Fatal("expected HasStarted=true after Start")
	}

	// Shared client still usable.
	if err := tc.Add("after", "* * * * *", func() {}); err != nil {
		t.Fatal(err)
	}
	if !tc.HasJob("after") {
		t.Fatal("expected shared client to remain usable")
	}
}

// TestCronStartStop is retained as a documentation test: Start/Stop are
// pause/resume in the shim. The legacy interval-based tick behaviour
// from the old ticker implementation is superseded by per-schedule durations
// at the tasks.Client layer (see TestCronDelegatesToTasks).
func TestCronStartStop(t *testing.T) {
	t.Parallel()
	t.Skip("superseded by TestCronDelegatesToTasks — legacy 1-min-tick semantics no longer apply")
}

// TestCronAppFacadeLocal exercises the app-shaped call site with TASKS_URL
// unset — the pure-local path that every dev/test environment uses today.
func TestCronAppFacadeLocal(t *testing.T) {
	t.Parallel()

	tc := tasks.New("", "", nil) // local only
	defer tc.Stop()

	c := NewFromTasks(tc)
	defer c.Stop()

	if err := c.Add("settlement", "*/5 * * * *", func() {}); err != nil {
		t.Fatal(err)
	}

	if !c.HasJob("settlement") {
		t.Fatal("expected settlement schedule to be registered")
	}
	if c.Total() != 1 {
		t.Fatalf("expected Total=1, got %d", c.Total())
	}
}

// TestCronAppFacadeRemoteFallback exercises the TASKS_URL-set path with an
// unreachable server — it must fall through to the local goroutine ticker,
// record the schedule, and still tick.
func TestCronAppFacadeRemoteFallback(t *testing.T) {
	t.Parallel()

	// Point at a definitely-closed port so HTTP scheduling fails fast.
	tc := tasks.New("http://127.0.0.1:1", "", nil)
	defer tc.Stop()

	c := NewFromTasks(tc)
	defer c.Stop()

	var hits atomic.Int64
	if err := c.Add("remote-fallback", "20ms", func() { hits.Add(1) }); err != nil {
		t.Fatal(err)
	}

	if !c.HasJob("remote-fallback") {
		t.Fatal("expected schedule to be registered after remote failure")
	}

	deadline := time.Now().Add(250 * time.Millisecond)
	for time.Now().Before(deadline) {
		if hits.Load() >= 2 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if hits.Load() < 2 {
		t.Fatalf("expected local fallback to keep ticking, got %d hits", hits.Load())
	}
}
