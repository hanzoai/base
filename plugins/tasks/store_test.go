package tasks

import (
	"os"
	"testing"

	"github.com/hanzoai/base/core"
	_ "github.com/hanzoai/base/migrations"
)

// newTestApp creates a minimal base app for testing (no import of tests package to avoid cycles).
func newTestApp(t *testing.T) core.App {
	t.Helper()
	dir, err := os.MkdirTemp("", "tasks_test_*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })

	app := core.NewBaseApp(core.BaseAppConfig{
		DataDir:       dir,
		EncryptionEnv: "test_enc_key",
	})

	if err := Register(app, Config{}); err != nil {
		t.Fatal(err)
	}
	if err := app.Bootstrap(); err != nil {
		t.Fatal(err)
	}

	return app
}

func TestCreateAndGetTask(t *testing.T) {
	app := newTestApp(t)
	s := GetStore(app)
	if s == nil {
		t.Fatal("store not registered")
	}

	task := &Task{
		SpaceID: "space-1",
		Title:   "Build the thing",
	}
	if err := s.CreateTask(task); err != nil {
		t.Fatal(err)
	}
	if task.ID == "" {
		t.Fatal("expected auto-generated ID")
	}
	if task.State != TaskPending {
		t.Fatalf("expected pending, got %s", task.State)
	}

	got, err := s.GetTask(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "Build the thing" {
		t.Fatalf("expected 'Build the thing', got %q", got.Title)
	}
	if got.SpaceID != "space-1" {
		t.Fatalf("expected space-1, got %q", got.SpaceID)
	}
}

func TestClaimTask(t *testing.T) {
	app := newTestApp(t)
	s := GetStore(app)

	task := &Task{SpaceID: "s1", Title: "Claimable"}
	if err := s.CreateTask(task); err != nil {
		t.Fatal(err)
	}

	if err := s.ClaimTask(task.ID, "agent-1"); err != nil {
		t.Fatal(err)
	}

	// Second claim should fail.
	if err := s.ClaimTask(task.ID, "agent-2"); err != ErrAlreadyClaimed {
		t.Fatalf("expected ErrAlreadyClaimed, got %v", err)
	}

	got, _ := s.GetTask(task.ID)
	if got.State != TaskClaimed {
		t.Fatalf("expected claimed, got %s", got.State)
	}
	if got.AssignedTo != "agent-1" {
		t.Fatalf("expected agent-1, got %s", got.AssignedTo)
	}
}

func TestCompleteTask(t *testing.T) {
	app := newTestApp(t)
	s := GetStore(app)

	task := &Task{SpaceID: "s1", Title: "Completable"}
	_ = s.CreateTask(task)
	_ = s.ClaimTask(task.ID, "agent-1")
	_ = s.StartTask(task.ID)

	output := map[string]any{"result": "success"}
	if err := s.CompleteTask(task.ID, output); err != nil {
		t.Fatal(err)
	}

	got, _ := s.GetTask(task.ID)
	if got.State != TaskCompleted {
		t.Fatalf("expected completed, got %s", got.State)
	}
	if got.Progress != 100 {
		t.Fatalf("expected progress 100, got %d", got.Progress)
	}
	if got.CompletedAt == nil {
		t.Fatal("expected completedAt to be set")
	}
}

func TestFailTaskWithRetry(t *testing.T) {
	app := newTestApp(t)
	s := GetStore(app)

	task := &Task{SpaceID: "s1", Title: "Retriable", MaxRetries: 2}
	_ = s.CreateTask(task)
	_ = s.ClaimTask(task.ID, "agent-1")
	_ = s.StartTask(task.ID)

	// First failure should re-queue.
	if err := s.FailTask(task.ID, "oops"); err != nil {
		t.Fatal(err)
	}
	got, _ := s.GetTask(task.ID)
	if got.State != TaskPending {
		t.Fatalf("expected re-queued to pending, got %s", got.State)
	}
	if got.RetryCount != 1 {
		t.Fatalf("expected retryCount 1, got %d", got.RetryCount)
	}

	// Second cycle.
	_ = s.ClaimTask(task.ID, "agent-2")
	_ = s.StartTask(task.ID)
	_ = s.FailTask(task.ID, "oops again")

	got, _ = s.GetTask(task.ID)
	if got.State != TaskPending {
		t.Fatalf("expected re-queued, got %s", got.State)
	}
	if got.RetryCount != 2 {
		t.Fatalf("expected retryCount 2, got %d", got.RetryCount)
	}

	// Third failure should be terminal.
	_ = s.ClaimTask(task.ID, "agent-3")
	_ = s.StartTask(task.ID)
	_ = s.FailTask(task.ID, "final fail")

	got, _ = s.GetTask(task.ID)
	if got.State != TaskFailed {
		t.Fatalf("expected terminal failed, got %s", got.State)
	}
}

func TestCancelTask(t *testing.T) {
	app := newTestApp(t)
	s := GetStore(app)

	task := &Task{SpaceID: "s1", Title: "Cancellable"}
	_ = s.CreateTask(task)

	if err := s.CancelTask(task.ID); err != nil {
		t.Fatal(err)
	}

	got, _ := s.GetTask(task.ID)
	if got.State != TaskCancelled {
		t.Fatalf("expected cancelled, got %s", got.State)
	}

	// Can't cancel again.
	if err := s.CancelTask(task.ID); err != ErrInvalidTransition {
		t.Fatalf("expected ErrInvalidTransition, got %v", err)
	}
}

func TestGetNextPendingTask(t *testing.T) {
	app := newTestApp(t)
	s := GetStore(app)

	low := &Task{SpaceID: "s1", Title: "Low", Priority: PriorityLow}
	high := &Task{SpaceID: "s1", Title: "High", Priority: PriorityHigh}
	_ = s.CreateTask(low)
	_ = s.CreateTask(high)

	// Should get high-priority first.
	next, err := s.GetNextPendingTask("s1", "agent-1")
	if err != nil {
		t.Fatal(err)
	}
	if next == nil {
		t.Fatal("expected a task")
	}
	if next.Title != "High" {
		t.Fatalf("expected High, got %q", next.Title)
	}
	if next.State != TaskClaimed {
		t.Fatalf("expected claimed, got %s", next.State)
	}

	// Next should get low.
	next, _ = s.GetNextPendingTask("s1", "agent-2")
	if next == nil || next.Title != "Low" {
		t.Fatalf("expected Low, got %v", next)
	}

	// No more.
	next, _ = s.GetNextPendingTask("s1", "agent-3")
	if next != nil {
		t.Fatalf("expected nil, got %+v", next)
	}
}

func TestDependencyBlocking(t *testing.T) {
	app := newTestApp(t)
	s := GetStore(app)

	a := &Task{SpaceID: "s1", Title: "Task A"}
	_ = s.CreateTask(a)

	b := &Task{SpaceID: "s1", Title: "Task B", DependsOn: []string{a.ID}}
	_ = s.CreateTask(b)

	// Next should return A (B blocked).
	next, _ := s.GetNextPendingTask("s1", "agent-1")
	if next == nil || next.Title != "Task A" {
		t.Fatal("expected Task A")
	}

	// B still blocked (A claimed, not completed).
	next, _ = s.GetNextPendingTask("s1", "agent-2")
	if next != nil {
		t.Fatalf("expected nil (B blocked), got %q", next.Title)
	}

	// Complete A.
	_ = s.StartTask(a.ID)
	_ = s.CompleteTask(a.ID, nil)

	// Now B available.
	next, _ = s.GetNextPendingTask("s1", "agent-2")
	if next == nil || next.Title != "Task B" {
		t.Fatal("expected Task B after A completed")
	}
}

func TestListTasksFiltered(t *testing.T) {
	app := newTestApp(t)
	s := GetStore(app)

	_ = s.CreateTask(&Task{SpaceID: "s1", Title: "T1", Priority: PriorityLow})
	_ = s.CreateTask(&Task{SpaceID: "s1", Title: "T2", Priority: PriorityHigh})
	_ = s.CreateTask(&Task{SpaceID: "s2", Title: "T3", Priority: PriorityNormal})

	items, _ := s.ListTasks(TaskFilters{SpaceID: "s1"})
	if len(items) != 2 {
		t.Fatalf("expected 2, got %d", len(items))
	}
	if items[0].Title != "T2" {
		t.Fatalf("expected T2 first (higher priority), got %q", items[0].Title)
	}

	pending := TaskPending
	items, _ = s.ListTasks(TaskFilters{SpaceID: "s2", State: &pending})
	if len(items) != 1 {
		t.Fatalf("expected 1 pending in s2, got %d", len(items))
	}
}

func TestWorkflowLifecycle(t *testing.T) {
	app := newTestApp(t)
	s := GetStore(app)

	wf := &Workflow{SpaceID: "s1", Name: "Pipeline"}
	if err := s.CreateWorkflow(wf); err != nil {
		t.Fatal(err)
	}
	if wf.ID == "" {
		t.Fatal("expected auto-generated ID")
	}

	got, err := s.GetWorkflow(wf.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "Pipeline" {
		t.Fatalf("expected Pipeline, got %q", got.Name)
	}
	if got.State != TaskPending {
		t.Fatalf("expected pending, got %s", got.State)
	}

	items, _ := s.ListWorkflows("s1")
	if len(items) != 1 {
		t.Fatalf("expected 1, got %d", len(items))
	}
}

func TestWorkflowAdvancement(t *testing.T) {
	app := newTestApp(t)
	s := GetStore(app)

	// Create workflow with 2 tasks.
	t1 := &Task{SpaceID: "s1", Title: "Step 1"}
	_ = s.CreateTask(t1)
	t2 := &Task{SpaceID: "s1", Title: "Step 2", DependsOn: []string{t1.ID}}
	_ = s.CreateTask(t2)

	wf := &Workflow{SpaceID: "s1", Name: "Pipeline", Tasks: []string{t1.ID, t2.ID}}
	_ = s.CreateWorkflow(wf)
	_ = s.UpdateWorkflowTasks(wf)

	// Advance: should go from pending → running.
	_ = s.AdvanceWorkflows()
	got, _ := s.GetWorkflow(wf.ID)
	if got.State != TaskRunning {
		t.Fatalf("expected running, got %s", got.State)
	}

	// Complete both tasks.
	_ = s.ClaimTask(t1.ID, "a")
	_ = s.StartTask(t1.ID)
	_ = s.CompleteTask(t1.ID, nil)
	_ = s.ClaimTask(t2.ID, "a")
	_ = s.StartTask(t2.ID)
	_ = s.CompleteTask(t2.ID, nil)

	// Advance: should complete workflow.
	_ = s.AdvanceWorkflows()
	got, _ = s.GetWorkflow(wf.ID)
	if got.State != TaskCompleted {
		t.Fatalf("expected completed, got %s", got.State)
	}
	if got.CompletedAt == nil {
		t.Fatal("expected completedAt set")
	}
}

func TestOrgIsolation(t *testing.T) {
	app := newTestApp(t)
	s := GetStore(app)

	// Create tasks in two different orgs.
	taskA := &Task{OrgID: "org-alpha", SpaceID: "s1", Title: "Alpha task"}
	taskB := &Task{OrgID: "org-beta", SpaceID: "s1", Title: "Beta task"}
	if err := s.CreateTask(taskA); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateTask(taskB); err != nil {
		t.Fatal(err)
	}

	// ListTasks scoped to org-alpha should only return alpha task.
	items, err := s.ListTasks(TaskFilters{OrgID: "org-alpha"})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Title != "Alpha task" {
		t.Fatalf("expected 1 alpha task, got %d", len(items))
	}

	// GetTask with wrong org should fail.
	_, err = s.GetTask(taskA.ID, "org-beta")
	if err != ErrTaskNotFound {
		t.Fatalf("expected ErrTaskNotFound for cross-org GetTask, got %v", err)
	}

	// GetTask with correct org should succeed.
	got, err := s.GetTask(taskA.ID, "org-alpha")
	if err != nil {
		t.Fatal(err)
	}
	if got.OrgID != "org-alpha" {
		t.Fatalf("expected org-alpha, got %q", got.OrgID)
	}

	// ClaimTask with wrong org should fail silently (0 rows affected).
	err = s.ClaimTask(taskA.ID, "agent-1", "org-beta")
	if err != ErrTaskNotFound {
		t.Fatalf("expected ErrTaskNotFound for cross-org ClaimTask, got %v", err)
	}

	// ClaimTask with correct org should succeed.
	if err := s.ClaimTask(taskA.ID, "agent-1", "org-alpha"); err != nil {
		t.Fatalf("expected claim to succeed for correct org, got %v", err)
	}

	// StartTask with wrong org should fail.
	err = s.StartTask(taskA.ID, "org-beta")
	if err != ErrTaskNotFound {
		t.Fatalf("expected ErrTaskNotFound for cross-org StartTask, got %v", err)
	}

	// StartTask with correct org should succeed.
	if err := s.StartTask(taskA.ID, "org-alpha"); err != nil {
		t.Fatal(err)
	}

	// CompleteTask with wrong org should fail.
	err = s.CompleteTask(taskA.ID, nil, "org-beta")
	if err != ErrTaskNotFound {
		t.Fatalf("expected ErrTaskNotFound for cross-org CompleteTask, got %v", err)
	}

	// CompleteTask with correct org should succeed.
	if err := s.CompleteTask(taskA.ID, nil, "org-alpha"); err != nil {
		t.Fatal(err)
	}

	// CancelTask with wrong org should fail.
	err = s.CancelTask(taskB.ID, "org-alpha")
	if err != ErrTaskNotFound {
		t.Fatalf("expected ErrTaskNotFound for cross-org CancelTask, got %v", err)
	}

	// CancelTask with correct org should succeed.
	if err := s.CancelTask(taskB.ID, "org-beta"); err != nil {
		t.Fatal(err)
	}

	// GetNextPendingTask scoped to org returns only that org's tasks.
	taskC := &Task{OrgID: "org-gamma", SpaceID: "s1", Title: "Gamma task"}
	taskD := &Task{OrgID: "org-delta", SpaceID: "s1", Title: "Delta task"}
	_ = s.CreateTask(taskC)
	_ = s.CreateTask(taskD)

	next, err := s.GetNextPendingTask("s1", "agent-2", "org-gamma")
	if err != nil {
		t.Fatal(err)
	}
	if next == nil || next.OrgID != "org-gamma" {
		t.Fatalf("expected org-gamma task, got %v", next)
	}
}

func TestWorkflowOrgIsolation(t *testing.T) {
	app := newTestApp(t)
	s := GetStore(app)

	wfA := &Workflow{OrgID: "org-alpha", SpaceID: "s1", Name: "Alpha WF"}
	wfB := &Workflow{OrgID: "org-beta", SpaceID: "s1", Name: "Beta WF"}
	if err := s.CreateWorkflow(wfA); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateWorkflow(wfB); err != nil {
		t.Fatal(err)
	}

	// GetWorkflow with wrong org should fail.
	_, err := s.GetWorkflow(wfA.ID, "org-beta")
	if err != ErrWorkflowNotFound {
		t.Fatalf("expected ErrWorkflowNotFound for cross-org GetWorkflow, got %v", err)
	}

	// GetWorkflow with correct org should succeed.
	got, err := s.GetWorkflow(wfA.ID, "org-alpha")
	if err != nil {
		t.Fatal(err)
	}
	if got.OrgID != "org-alpha" {
		t.Fatalf("expected org-alpha, got %q", got.OrgID)
	}

	// ListWorkflows scoped to org-alpha should only return alpha workflow.
	items, err := s.ListWorkflows("s1", "org-alpha")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Name != "Alpha WF" {
		t.Fatalf("expected 1 alpha workflow, got %d", len(items))
	}

	// ListWorkflows without org should return both.
	all, err := s.ListWorkflows("s1")
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 {
		t.Fatalf("expected 2 workflows, got %d", len(all))
	}
}
