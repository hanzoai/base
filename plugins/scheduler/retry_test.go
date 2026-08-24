package scheduler

import (
	"testing"
	"time"

	"github.com/hanzoai/base/tests"
	"github.com/hanzoai/base/tools/types"
)

// newRetryPlugin returns a scheduler whose executor always fails, plus a counter
// of how many times it was asked to run.
func newRetryPlugin(t *testing.T, cfg Config) (*plugin, *tests.TestApp, *int) {
	t.Helper()
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("NewTestApp: %v", err)
	}
	t.Cleanup(app.Cleanup)

	calls := 0
	cfg.OnExecute = func(name string, args any) (any, error) {
		calls++
		return nil, &schedulerError{"intentional failure"}
	}
	p := &plugin{app: app, config: cfg, sem: make(chan struct{}, 10)}
	if err := p.ensureCollection(); err != nil {
		t.Fatalf("ensureCollection: %v", err)
	}
	return p, app, &calls
}

func runOnce(t *testing.T, p *plugin, app *tests.TestApp, id string) {
	t.Helper()
	rec, err := app.FindRecordById(CollectionName, id)
	if err != nil {
		t.Fatalf("FindRecordById: %v", err)
	}
	p.claimFunction(rec)
	p.executeFunction(rec)
}

// A function that keeps failing is retried RetryCount times and then stopped.
// The terminal state matters as much as the retries: a job left pending forever
// is one the poller keeps picking up, and a job marked failed without its reason
// is one nobody can diagnose.
func TestRetriesAreExhaustedThenTheFunctionIsMarkedFailed(t *testing.T) {
	p, app, calls := newRetryPlugin(t, Config{RetryCount: 2, RetryDelay: time.Millisecond})

	id, err := ScheduleAfter(app, 0, "doomed", nil)
	if err != nil {
		t.Fatalf("ScheduleAfter: %v", err)
	}

	// Attempt 1 and 2 reschedule; attempt 3 exhausts the budget.
	for attempt := 1; attempt <= 3; attempt++ {
		runOnce(t, p, app, id)
		rec, err := app.FindRecordById(CollectionName, id)
		if err != nil {
			t.Fatalf("FindRecordById: %v", err)
		}
		if attempt <= 2 {
			if got := rec.GetString("status"); got != StatusPending {
				t.Fatalf("attempt %d: status = %q, want %q (a retry stays pending)", attempt, got, StatusPending)
			}
			if got := int(rec.GetFloat("retryCount")); got != attempt {
				t.Fatalf("attempt %d: retryCount = %d, want %d", attempt, got, attempt)
			}
			continue
		}
		if got := rec.GetString("status"); got != StatusFailed {
			t.Fatalf("after the budget is spent: status = %q, want %q", got, StatusFailed)
		}
		if rec.GetString("error") == "" {
			t.Fatal("a failed function must carry the reason it failed")
		}
		if rec.GetString("completedAt") == "" {
			t.Fatal("a failed function must be stamped, or it is indistinguishable from one still running")
		}
	}

	if *calls != 3 {
		t.Fatalf("executor ran %d times, want 3 (the first attempt plus RetryCount retries)", *calls)
	}
}

// With no executor there is nothing to retry: the function fails immediately and
// says why, rather than sitting pending against a scheduler that cannot run it.
func TestNoExecutorFailsImmediately(t *testing.T) {
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("NewTestApp: %v", err)
	}
	t.Cleanup(app.Cleanup)

	p := &plugin{app: app, config: Config{RetryCount: 5}, sem: make(chan struct{}, 10)}
	if err := p.ensureCollection(); err != nil {
		t.Fatalf("ensureCollection: %v", err)
	}
	id, err := ScheduleAfter(app, 0, "orphan", nil)
	if err != nil {
		t.Fatalf("ScheduleAfter: %v", err)
	}
	runOnce(t, p, app, id)

	rec, err := app.FindRecordById(CollectionName, id)
	if err != nil {
		t.Fatalf("FindRecordById: %v", err)
	}
	if got := rec.GetString("status"); got != StatusFailed {
		t.Fatalf("status = %q, want %q — RetryCount must not retry what cannot run", got, StatusFailed)
	}
	if got := rec.GetString("error"); got != "no executor configured" {
		t.Fatalf("error = %q, want %q", got, "no executor configured")
	}
}

// The backoff is ARITHMETIC: RetryDelay × the retry number, so a 10s base gives
// 10s, 20s, 30s. Pinned because the code says one thing and is easy to read as
// another — RetryDelay is documented as a "base delay", which is equally true of
// a doubling schedule, and nothing else in the tree says which this is.
func TestBackoffGrowsByAFixedStepPerRetry(t *testing.T) {
	const base = 10 * time.Second
	p, app, _ := newRetryPlugin(t, Config{RetryCount: 3, RetryDelay: base})

	id, err := ScheduleAfter(app, 0, "slow", nil)
	if err != nil {
		t.Fatalf("ScheduleAfter: %v", err)
	}

	for attempt := 1; attempt <= 3; attempt++ {
		before := time.Now().UTC()
		runOnce(t, p, app, id)

		rec, err := app.FindRecordById(CollectionName, id)
		if err != nil {
			t.Fatalf("FindRecordById: %v", err)
		}
		at, err := types.ParseDateTime(rec.GetString("scheduledAt"))
		if err != nil {
			t.Fatalf("ParseDateTime(%q): %v", rec.GetString("scheduledAt"), err)
		}
		got := at.Time().Sub(before)
		want := base * time.Duration(attempt)
		// A second of slack for the clock read either side of the write.
		if got < want-time.Second || got > want+time.Second {
			t.Fatalf("retry %d scheduled %v out, want ~%v (arithmetic: base × n)", attempt, got, want)
		}
	}
}
