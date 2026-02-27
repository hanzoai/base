package scheduler

import (
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/hanzoai/base/tests"
	"github.com/hanzoai/base/tools/types"
)

func TestEnsureCollection(t *testing.T) {
	app, _ := tests.NewTestApp()
	defer app.Cleanup()

	p := &plugin{app: app, config: Config{}}

	// collection should not exist yet
	_, err := app.FindCollectionByNameOrId(CollectionName)
	if err == nil {
		t.Fatal("expected collection to not exist initially")
	}

	// ensure creates it
	if err := p.ensureCollection(); err != nil {
		t.Fatalf("ensureCollection failed: %v", err)
	}

	col, err := app.FindCollectionByNameOrId(CollectionName)
	if err != nil {
		t.Fatalf("expected collection to exist after ensureCollection: %v", err)
	}

	// verify key fields exist
	expectedFields := []string{"functionName", "args", "scheduledAt", "status", "result", "error", "retryCount", "createdAt", "completedAt"}
	for _, name := range expectedFields {
		if col.Fields.GetByName(name) == nil {
			t.Errorf("expected field %q to exist", name)
		}
	}

	// idempotent: calling again should not error
	if err := p.ensureCollection(); err != nil {
		t.Fatalf("second ensureCollection call failed: %v", err)
	}
}

func TestScheduleAfterAndList(t *testing.T) {
	app, _ := tests.NewTestApp()
	defer app.Cleanup()

	p := &plugin{app: app, config: Config{}}
	if err := p.ensureCollection(); err != nil {
		t.Fatalf("ensureCollection failed: %v", err)
	}

	args := map[string]any{"key": "value"}
	id, err := ScheduleAfter(app, 0, "testFunc", args)
	if err != nil {
		t.Fatalf("ScheduleAfter failed: %v", err)
	}
	if id == "" {
		t.Fatal("expected non-empty record ID")
	}

	// verify it exists via ListScheduled
	records, err := ListScheduled(app, StatusPending)
	if err != nil {
		t.Fatalf("ListScheduled failed: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 pending record, got %d", len(records))
	}
	if records[0].Id != id {
		t.Fatalf("expected record ID %q, got %q", id, records[0].Id)
	}
	if records[0].GetString("functionName") != "testFunc" {
		t.Fatalf("expected functionName 'testFunc', got %q", records[0].GetString("functionName"))
	}
}

func TestScheduleAt(t *testing.T) {
	app, _ := tests.NewTestApp()
	defer app.Cleanup()

	p := &plugin{app: app, config: Config{}}
	if err := p.ensureCollection(); err != nil {
		t.Fatalf("ensureCollection failed: %v", err)
	}

	future := time.Now().UTC().Add(1 * time.Hour)
	id, err := ScheduleAt(app, future, "futureFunc", nil)
	if err != nil {
		t.Fatalf("ScheduleAt failed: %v", err)
	}
	if id == "" {
		t.Fatal("expected non-empty record ID")
	}

	record, err := app.FindRecordById(CollectionName, id)
	if err != nil {
		t.Fatalf("FindRecordById failed: %v", err)
	}
	if record.GetString("status") != StatusPending {
		t.Fatalf("expected status 'pending', got %q", record.GetString("status"))
	}
}

func TestCancelScheduled(t *testing.T) {
	app, _ := tests.NewTestApp()
	defer app.Cleanup()

	p := &plugin{app: app, config: Config{}}
	if err := p.ensureCollection(); err != nil {
		t.Fatalf("ensureCollection failed: %v", err)
	}

	id, err := ScheduleAfter(app, 60000, "toCancel", nil)
	if err != nil {
		t.Fatalf("ScheduleAfter failed: %v", err)
	}

	if err := CancelScheduled(app, id); err != nil {
		t.Fatalf("CancelScheduled failed: %v", err)
	}

	// verify it's cancelled - should not appear in pending list
	pending, err := ListScheduled(app, StatusPending)
	if err != nil {
		t.Fatalf("ListScheduled failed: %v", err)
	}
	if len(pending) != 0 {
		t.Fatalf("expected 0 pending records after cancel, got %d", len(pending))
	}
}

func TestClaimFunction(t *testing.T) {
	app, _ := tests.NewTestApp()
	defer app.Cleanup()

	p := &plugin{app: app, config: Config{}}
	if err := p.ensureCollection(); err != nil {
		t.Fatalf("ensureCollection failed: %v", err)
	}

	id, err := ScheduleAfter(app, 0, "claimTest", nil)
	if err != nil {
		t.Fatalf("ScheduleAfter failed: %v", err)
	}

	record, err := app.FindRecordById(CollectionName, id)
	if err != nil {
		t.Fatalf("FindRecordById failed: %v", err)
	}

	// first claim should succeed
	if !p.claimFunction(record) {
		t.Fatal("expected first claim to succeed")
	}

	// second claim should fail (already running)
	if p.claimFunction(record) {
		t.Fatal("expected second claim to fail")
	}
}

func TestExecuteFunction(t *testing.T) {
	app, _ := tests.NewTestApp()
	defer app.Cleanup()

	var executedName string
	var executedArgs any

	p := &plugin{
		app: app,
		config: Config{
			RetryCount: 0,
			OnExecute: func(name string, args any) (any, error) {
				executedName = name
				executedArgs = args
				return map[string]any{"ok": true}, nil
			},
		},
		sem: make(chan struct{}, 10),
	}

	if err := p.ensureCollection(); err != nil {
		t.Fatalf("ensureCollection failed: %v", err)
	}

	id, err := ScheduleAfter(app, 0, "executeTest", map[string]any{"hello": "world"})
	if err != nil {
		t.Fatalf("ScheduleAfter failed: %v", err)
	}

	record, err := app.FindRecordById(CollectionName, id)
	if err != nil {
		t.Fatalf("FindRecordById failed: %v", err)
	}

	// claim and execute
	p.claimFunction(record)
	p.executeFunction(record)

	if executedName != "executeTest" {
		t.Fatalf("expected executedName 'executeTest', got %q", executedName)
	}
	if executedArgs == nil {
		t.Fatal("expected executedArgs to be non-nil")
	}

	// verify record is completed
	updated, err := app.FindRecordById(CollectionName, id)
	if err != nil {
		t.Fatalf("FindRecordById after execute failed: %v", err)
	}
	if updated.GetString("status") != StatusCompleted {
		t.Fatalf("expected status 'completed', got %q", updated.GetString("status"))
	}
}

func TestProcessDueFunctions(t *testing.T) {
	app, _ := tests.NewTestApp()
	defer app.Cleanup()

	var mu sync.Mutex
	executed := map[string]bool{}

	p := &plugin{
		app: app,
		config: Config{
			MaxConcurrent: 5,
			RetryCount:    0,
			OnExecute: func(name string, args any) (any, error) {
				mu.Lock()
				executed[name] = true
				mu.Unlock()
				return nil, nil
			},
		},
		sem: make(chan struct{}, 5),
	}

	if err := p.ensureCollection(); err != nil {
		t.Fatalf("ensureCollection failed: %v", err)
	}

	// schedule 3 functions that are already due
	for _, name := range []string{"func1", "func2", "func3"} {
		if _, err := ScheduleAfter(app, 0, name, nil); err != nil {
			t.Fatalf("ScheduleAfter(%s) failed: %v", name, err)
		}
	}

	// schedule one in the future (should not be picked up)
	future := time.Now().UTC().Add(1 * time.Hour)
	futureAt, _ := types.ParseDateTime(future)
	_ = futureAt
	if _, err := ScheduleAt(app, future, "futureFunc", nil); err != nil {
		t.Fatalf("ScheduleAt(futureFunc) failed: %v", err)
	}

	p.processDueFunctions()

	// wait for goroutines
	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()

	for _, name := range []string{"func1", "func2", "func3"} {
		if !executed[name] {
			t.Errorf("expected %q to be executed", name)
		}
	}
	if executed["futureFunc"] {
		t.Error("futureFunc should not have been executed")
	}
}

func TestRetryOnFailure(t *testing.T) {
	app, _ := tests.NewTestApp()
	defer app.Cleanup()

	callCount := 0

	p := &plugin{
		app: app,
		config: Config{
			RetryCount: 2,
			RetryDelay: 1 * time.Millisecond,
			OnExecute: func(name string, args any) (any, error) {
				callCount++
				return nil, &schedulerError{"intentional failure"}
			},
		},
		sem: make(chan struct{}, 10),
	}

	if err := p.ensureCollection(); err != nil {
		t.Fatalf("ensureCollection failed: %v", err)
	}

	id, err := ScheduleAfter(app, 0, "retryFunc", nil)
	if err != nil {
		t.Fatalf("ScheduleAfter failed: %v", err)
	}

	record, err := app.FindRecordById(CollectionName, id)
	if err != nil {
		t.Fatalf("FindRecordById failed: %v", err)
	}

	// first attempt: should reschedule
	p.claimFunction(record)
	p.executeFunction(record)

	if callCount != 1 {
		t.Fatalf("expected 1 call, got %d", callCount)
	}

	// verify rescheduled with retryCount=1
	updated, err := app.FindRecordById(CollectionName, id)
	if err != nil {
		t.Fatalf("FindRecordById failed: %v", err)
	}
	if updated.GetString("status") != StatusPending {
		t.Fatalf("expected status 'pending' after retry, got %q", updated.GetString("status"))
	}

	retryJSON := updated.Get("retryCount")
	retryCount := 0
	if b, err := json.Marshal(retryJSON); err == nil {
		json.Unmarshal(b, &retryCount)
	}
	if retryCount != 1 {
		t.Fatalf("expected retryCount 1, got %v", retryCount)
	}
}
