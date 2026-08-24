package tasks

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// withExecutor installs an executor for one test and restores whatever was
// there. taskExecutor is package state, so a test that forgets to put it back
// changes the result of every test that runs after it.
func withExecutor(t *testing.T, fn func(context.Context, *Task) (*Task, error)) {
	t.Helper()
	prev := taskExecutor
	t.Cleanup(func() { taskExecutor = prev })
	taskExecutor = fn
}

// With nothing registered the activity ACKNOWLEDGES rather than failing. That is
// the deliberate choice — a task submitted to a deployment that runs no executor
// completes instead of retrying forever against a worker that will never do the
// work — and it is worth pinning, because "completed" here means "nobody ran it".
func TestNoExecutorAcknowledgesInsteadOfFailing(t *testing.T) {
	withExecutor(t, nil)

	in := &Task{ID: "t1", SpaceID: "s1", Title: "unowned"}
	out, err := ExecuteTaskActivity(context.Background(), in)
	if err != nil {
		t.Fatalf("ExecuteTaskActivity: %v", err)
	}
	if out.State != TaskCompleted {
		t.Fatalf("State = %v, want %v", out.State, TaskCompleted)
	}
	msg, _ := out.Output["message"].(string)
	if !strings.Contains(msg, "No executor") {
		t.Fatalf("Output[message] = %q, want it to say no executor ran the task", msg)
	}
}

// The activity is a pass-through: whatever the executor returns is the result,
// unchanged. It must not substitute the task it was handed.
func TestExecutorResultIsReturnedUnchanged(t *testing.T) {
	want := &Task{ID: "t2", Title: "done", State: TaskCompleted, Output: map[string]any{"rows": 3}}
	var got *Task
	withExecutor(t, func(_ context.Context, task *Task) (*Task, error) {
		got = task
		return want, nil
	})

	in := &Task{ID: "t2", SpaceID: "s1", Title: "todo", AssignedTo: "someone"}
	out, err := ExecuteTaskActivity(context.Background(), in)
	if err != nil {
		t.Fatalf("ExecuteTaskActivity: %v", err)
	}
	if got != in {
		t.Fatal("the executor was handed a different task than the activity received")
	}
	if out != want {
		t.Fatalf("out = %+v, want the executor's own result %+v", out, want)
	}
}

// A failing executor yields no task at all, and the error names the failure it
// wraps — a caller that got back the input task would record a task that never
// ran as one that did.
func TestExecutorFailureReturnsNoTaskAndWrapsTheCause(t *testing.T) {
	cause := errors.New("disk on fire")
	withExecutor(t, func(context.Context, *Task) (*Task, error) { return nil, cause })

	out, err := ExecuteTaskActivity(context.Background(), &Task{ID: "t3", Title: "doomed"})
	if err == nil {
		t.Fatal("a failing executor must not report success")
	}
	if out != nil {
		t.Fatalf("out = %+v, want nil when the executor failed", out)
	}
	if !errors.Is(err, cause) {
		t.Fatalf("err = %v, want it to wrap %v so a caller can inspect the cause", err, cause)
	}
}

// SetTaskExecutor is the only way the executor is installed, and it replaces
// rather than accumulates.
func TestSetTaskExecutorReplaces(t *testing.T) {
	prev := taskExecutor
	t.Cleanup(func() { taskExecutor = prev })

	SetTaskExecutor(func(context.Context, *Task) (*Task, error) {
		return &Task{ID: "first"}, nil
	})
	SetTaskExecutor(func(context.Context, *Task) (*Task, error) {
		return &Task{ID: "second"}, nil
	})

	out, err := ExecuteTaskActivity(context.Background(), &Task{ID: "t4"})
	if err != nil {
		t.Fatalf("ExecuteTaskActivity: %v", err)
	}
	if out.ID != "second" {
		t.Fatalf("out.ID = %q, want %q — the later registration wins", out.ID, "second")
	}
}
