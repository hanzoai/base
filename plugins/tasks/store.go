package tasks

import (
	"errors"
	"fmt"
	"time"

	"github.com/hanzoai/base/core"
	"github.com/hanzoai/base/tools/types"
	"github.com/hanzoai/dbx"
)

const (
	// TasksCollection is the internal collection for tasks.
	TasksCollection = "_tasks"
	// WorkflowsCollection is the internal collection for workflows.
	WorkflowsCollection = "_workflows"
)

var (
	ErrTaskNotFound      = errors.New("task not found")
	ErrWorkflowNotFound  = errors.New("workflow not found")
	ErrInvalidTransition = errors.New("invalid state transition")
	ErrAlreadyClaimed    = errors.New("task already claimed")
)

// Store provides SQLite-backed task persistence using Base collections.
type Store struct {
	app core.App
}

// NewStore creates a new task store.
func NewStore(app core.App) *Store {
	return &Store{app: app}
}

// --- Task CRUD ---

// CreateTask persists a new task. ID is auto-generated if empty.
func (s *Store) CreateTask(task *Task) error {
	if task.Title == "" {
		return errors.New("title is required")
	}

	col, err := s.app.FindCollectionByNameOrId(TasksCollection)
	if err != nil {
		return fmt.Errorf("tasks collection not found: %w", err)
	}

	record := core.NewRecord(col)
	s.setTaskFields(record, task)

	if task.State == "" {
		record.Set("state", string(TaskPending))
	}

	if err := s.app.Save(record); err != nil {
		return err
	}

	// Read back auto-generated fields.
	task.ID = record.Id
	task.CreatedAt = record.GetDateTime("created").Time()
	task.UpdatedAt = record.GetDateTime("updated").Time()
	if task.State == "" {
		task.State = TaskPending
	}
	return nil
}

// GetTask retrieves a task by ID.
// A non-empty org additionally requires the task to belong to it.
func (s *Store) GetTask(id string, org string) (*Task, error) {
	record, err := s.app.FindRecordById(TasksCollection, id)
	if err != nil {
		return nil, ErrTaskNotFound
	}
	task := s.recordToTask(record)
	if org != "" && task.OrgID != org {
		return nil, ErrTaskNotFound
	}
	return task, nil
}

// UpdateTask patches mutable fields on an existing task.
func (s *Store) UpdateTask(task *Task) error {
	record, err := s.app.FindRecordById(TasksCollection, task.ID)
	if err != nil {
		return ErrTaskNotFound
	}

	s.setTaskFields(record, task)

	if err := s.app.Save(record); err != nil {
		return err
	}
	task.UpdatedAt = record.GetDateTime("updated").Time()
	return nil
}

// ListTasks returns filtered tasks, sorted by priority DESC then created ASC.
func (s *Store) ListTasks(filters TaskFilters) ([]*Task, error) {
	query := s.app.RecordQuery(TasksCollection).
		OrderBy("priority DESC", "created ASC")

	if filters.OrgID != "" {
		query = query.AndWhere(dbx.HashExp{"orgId": filters.OrgID})
	}
	if filters.SpaceID != "" {
		query = query.AndWhere(dbx.HashExp{"spaceId": filters.SpaceID})
	}
	if filters.State != nil {
		query = query.AndWhere(dbx.HashExp{"state": string(*filters.State)})
	}
	if filters.AssignedTo != nil {
		query = query.AndWhere(dbx.HashExp{"assignedTo": *filters.AssignedTo})
	}
	if filters.Priority != nil {
		query = query.AndWhere(dbx.HashExp{"priority": int(*filters.Priority)})
	}
	if filters.WorkflowID != nil {
		query = query.AndWhere(dbx.HashExp{"workflowId": *filters.WorkflowID})
	}
	if filters.Offset > 0 {
		query = query.Offset(int64(filters.Offset))
	}
	limit := filters.Limit
	if limit <= 0 {
		limit = 100
	}
	query = query.Limit(int64(limit))

	var records []*core.Record
	if err := query.All(&records); err != nil {
		return nil, err
	}

	result := make([]*Task, 0, len(records))
	for _, r := range records {
		result = append(result, s.recordToTask(r))
	}
	return result, nil
}

// allOrgs is the unscoped org: every org, no narrowing.
//
// Named, because "" at a call site says nothing about whether the author meant
// every org or forgot to pass one — which is exactly what the variadic form
// could not distinguish either.
const allOrgs = ""

// --- Atomic state transitions ---

// scoped narrows where to one org, or leaves it alone.
//
// The empty string is every org, not the org named "": an absent X-Org-Id
// header must widen, never match a row whose orgId happens to be blank. This
// guard was written at seven call sites, each free to drift; it is written
// here.
func scoped(where []dbx.Expression, org string) []dbx.Expression {
	if org == "" {
		return where
	}
	return append(where, dbx.HashExp{"orgId": org})
}

// transition applies one state change as a single guarded UPDATE, and reports
// the two ways it can decline.
//
// Every transition below is the same shape: set some columns, but only on a row
// in an acceptable starting state, and only within the requested org. Written
// out six times, that was six copies of the scoping clause and six copies of
// the nothing-matched branch.
//
// The org is a FILTER, not a permission. Every route reaching these methods is
// behind RequireSuperuserAuth and the value comes from a caller-supplied
// X-Org-Id header, so it narrows what an already-privileged caller sees rather
// than restricting what they may reach. Saying otherwise in a comment would
// invite someone to lean on it.
//
// from is the states this transition will accept; nil accepts any.
func (s *Store) transition(taskID string, set dbx.Params, from dbx.Expression, conflict error, org string) error {
	where := []dbx.Expression{dbx.HashExp{"id": taskID}}
	if from != nil {
		where = append(where, from)
	}
	where = scoped(where, org)

	result, err := s.app.NonconcurrentDB().
		Update(TasksCollection, set, dbx.And(where...)).
		Execute()
	if err != nil {
		return err
	}

	rows, _ := result.RowsAffected()
	if rows == 0 {
		// Zero rows is two different answers wearing the same face: the task is
		// not there (or not the caller's), or it is there in a state this
		// transition does not accept. Only a second read can tell them apart,
		// and the caller needs to — one is a 404, the other a conflict.
		if _, err := s.GetTask(taskID, org); err != nil {
			return ErrTaskNotFound
		}
		return conflict
	}
	return nil
}

// ClaimTask atomically transitions a task from pending to claimed.
// org narrows the mutation to one org; empty is every org.
func (s *Store) ClaimTask(taskID, agentID string, org string) error {
	now := types.NowDateTime().String()
	return s.transition(taskID,
		dbx.Params{"state": string(TaskClaimed), "assignedTo": agentID, "updated": now},
		dbx.HashExp{"state": string(TaskPending)},
		ErrAlreadyClaimed, org)
}

// StartTask transitions a claimed (or pending) task to running.
// org narrows the mutation to one org; empty is every org.
func (s *Store) StartTask(taskID string, org string) error {
	now := types.NowDateTime().String()
	return s.transition(taskID,
		dbx.Params{"state": string(TaskRunning), "startedAt": now, "updated": now},
		dbx.In("state", string(TaskClaimed), string(TaskPending)),
		ErrInvalidTransition, org)
}

// CompleteTask transitions a running task to completed with output.
// org narrows the mutation to one org; empty is every org.
func (s *Store) CompleteTask(taskID string, output map[string]any, org string) error {
	now := types.NowDateTime().String()
	return s.transition(taskID,
		dbx.Params{
			"state":       string(TaskCompleted),
			"output":      string(marshalJSON(output)),
			"progress":    100,
			"completedAt": now,
			"updated":     now,
		},
		dbx.HashExp{"state": string(TaskRunning)},
		ErrInvalidTransition, org)
}

// FailTask transitions a running task to failed. If retries remain, re-queues as pending.
// Uses a single atomic SQL with CASE to avoid TOCTOU races.
// org narrows the mutation to one org; empty is every org.
func (s *Store) FailTask(taskID string, errMsg string, org string) error {
	now := types.NowDateTime().String()

	// Whether this failure retries or ends the task depends on a column, so the
	// decision has to be made by the database inside the same statement that
	// acts on it. Reading retryCount first and then writing would let two
	// concurrent failures both see "retries remain" and both retry.
	//
	// retry is that condition, written once and reused, so every column cannot
	// disagree about which branch it is in.
	const retry = "[[retryCount]] < [[maxRetries]]"
	keep := func(thenValue, elseColumn string) dbx.Expression {
		return dbx.NewExp("CASE WHEN "+retry+" THEN "+thenValue+" ELSE "+elseColumn+" END",
			dbx.Params{"pending": string(TaskPending), "failed": string(TaskFailed), "now": now})
	}

	return s.transition(taskID,
		dbx.Params{
			"state":      keep("{:pending}", "{:failed}"),
			"retryCount": keep("[[retryCount]] + 1", "[[retryCount]]"),
			"assignedTo": keep("''", "[[assignedTo]]"),
			"startedAt":  keep("''", "[[startedAt]]"),
			"progress":   keep("0", "[[progress]]"),
			// Inverted: a task that still has retries left has not completed.
			"completedAt": dbx.NewExp("CASE WHEN "+retry+" THEN [[completedAt]] ELSE {:now} END",
				dbx.Params{"now": now}),
			"error":   errMsg,
			"updated": now,
		},
		dbx.HashExp{"state": string(TaskRunning)},
		ErrInvalidTransition, org)
}

// CancelTask transitions any non-terminal task to cancelled.
// org narrows the mutation to one org; empty is every org.
func (s *Store) CancelTask(taskID string, org string) error {
	now := types.NowDateTime().String()
	return s.transition(taskID,
		dbx.Params{"state": string(TaskCancelled), "completedAt": now, "updated": now},
		dbx.NotIn("state", string(TaskCompleted), string(TaskCancelled)),
		ErrInvalidTransition, org)
}

// UpdateProgress sets progress (0-100) on a running task.
// org narrows the mutation to one org; empty is every org.
func (s *Store) UpdateProgress(taskID string, progress int, org string) error {
	if progress < 0 {
		progress = 0
	}
	if progress > 100 {
		progress = 100
	}

	return s.transition(taskID,
		dbx.Params{"progress": progress, "updated": types.NowDateTime().String()},
		dbx.HashExp{"state": string(TaskRunning)},
		ErrInvalidTransition, org)
}

// GetNextPendingTask finds and atomically claims the highest-priority pending task
// in the given space whose dependencies are all completed.
// org narrows the query to one org; empty is every org.
func (s *Store) GetNextPendingTask(spaceID, agentID string, org string) (*Task, error) {
	pending := TaskPending
	filters := TaskFilters{
		SpaceID: spaceID,
		State:   &pending,
		Limit:   50,
	}
	filters.OrgID = org
	candidates, err := s.ListTasks(filters)
	if err != nil {
		return nil, err
	}

	for _, task := range candidates {
		if !s.dependenciesMet(task) {
			continue
		}

		// Attempt atomic claim scoped to org.
		if err := s.ClaimTask(task.ID, agentID, org); err != nil {
			continue // lost race or invalid transition
		}

		// Re-read the claimed task scoped to org.
		claimed, err := s.GetTask(task.ID, org)
		if err != nil {
			return nil, err
		}
		return claimed, nil
	}

	return nil, nil // no eligible tasks
}

// dependenciesMet checks if all tasks in DependsOn are completed.
func (s *Store) dependenciesMet(task *Task) bool {
	if len(task.DependsOn) == 0 {
		return true
	}
	for _, depID := range task.DependsOn {
		// allOrgs deliberately: a dependency is a property of the task graph,
		// not of tenancy, so a blocker in another org still blocks. This read
		// was already unscoped — the variadic just made it impossible to see
		// that it differed from the scoped call that leads here.
		dep, err := s.GetTask(depID, allOrgs)
		if err != nil || dep.State != TaskCompleted {
			return false
		}
	}
	return true
}

// AgentHasActiveTask reports whether the agent has a claimed or running task.
// org narrows the query to one org; empty is every org.
func (s *Store) AgentHasActiveTask(agentID string, org string) (bool, error) {
	where := []dbx.Expression{
		dbx.HashExp{"assignedTo": agentID},
		dbx.In("state", string(TaskClaimed), string(TaskRunning)),
	}
	where = scoped(where, org)

	// COUNT rather than a LIMIT 1 probe, because an aggregate always returns a
	// row. A probe returns NO row when nothing matches, and Row then reports
	// sql.ErrNoRows — turning "this agent is free", the answer the scheduler
	// acts on most, into an error.
	var count int
	err := s.app.ConcurrentDB().
		Select("COUNT(*)").
		From(TasksCollection).
		Where(dbx.And(where...)).
		Row(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// --- Workflow CRUD ---

// CreateWorkflow persists a new workflow.
func (s *Store) CreateWorkflow(wf *Workflow) error {
	if wf.Name == "" {
		return errors.New("workflow name is required")
	}

	col, err := s.app.FindCollectionByNameOrId(WorkflowsCollection)
	if err != nil {
		return fmt.Errorf("workflows collection not found: %w", err)
	}

	record := core.NewRecord(col)
	record.Set("orgId", wf.OrgID)
	record.Set("spaceId", wf.SpaceID)
	record.Set("name", wf.Name)
	record.Set("description", wf.Description)
	record.Set("state", string(TaskPending))
	record.Set("createdBy", wf.CreatedBy)
	record.Set("tasks", marshalJSON(wf.Tasks))
	record.Set("metadata", marshalJSON(wf.Metadata))

	if err := s.app.Save(record); err != nil {
		return err
	}

	wf.ID = record.Id
	wf.State = TaskPending
	wf.CreatedAt = record.GetDateTime("created").Time()
	wf.UpdatedAt = record.GetDateTime("updated").Time()
	return nil
}

// GetWorkflow retrieves a workflow by ID.
// A non-empty org additionally requires the workflow to belong to it.
func (s *Store) GetWorkflow(id string, org string) (*Workflow, error) {
	record, err := s.app.FindRecordById(WorkflowsCollection, id)
	if err != nil {
		return nil, ErrWorkflowNotFound
	}
	wf := s.recordToWorkflow(record)
	if org != "" && wf.OrgID != org {
		return nil, ErrWorkflowNotFound
	}
	return wf, nil
}

// ListWorkflows returns workflows for a space, optionally scoped to an org.
func (s *Store) ListWorkflows(spaceID string, org string) ([]*Workflow, error) {
	query := s.app.RecordQuery(WorkflowsCollection).
		OrderBy("created ASC")

	if org != "" {
		query = query.AndWhere(dbx.HashExp{"orgId": org})
	}
	if spaceID != "" {
		query = query.AndWhere(dbx.HashExp{"spaceId": spaceID})
	}

	var records []*core.Record
	if err := query.All(&records); err != nil {
		return nil, err
	}

	result := make([]*Workflow, 0, len(records))
	for _, r := range records {
		result = append(result, s.recordToWorkflow(r))
	}
	return result, nil
}

// UpdateWorkflowTasks updates the task ID list on an existing workflow.
// When the workflow has an OrgID, the update is scoped to that org.
func (s *Store) UpdateWorkflowTasks(wf *Workflow) error {
	now := types.NowDateTime().String()
	where := []dbx.Expression{dbx.HashExp{"id": wf.ID}}
	if wf.OrgID != "" {
		where = append(where, dbx.HashExp{"orgId": wf.OrgID})
	}

	_, err := s.app.NonconcurrentDB().Update(WorkflowsCollection,
		dbx.Params{"tasks": string(marshalJSON(wf.Tasks)), "updated": now},
		dbx.And(where...),
	).Execute()
	return err
}

// AdvanceWorkflows checks non-terminal workflows and updates their state
// based on constituent task states.
func (s *Store) AdvanceWorkflows() error {
	notDone := []string{string(TaskCompleted), string(TaskFailed), string(TaskCancelled)}

	var records []*core.Record
	err := s.app.RecordQuery(WorkflowsCollection).
		AndWhere(dbx.NewExp(
			"[[state]] NOT IN ({:s1}, {:s2}, {:s3})",
			dbx.Params{"s1": notDone[0], "s2": notDone[1], "s3": notDone[2]},
		)).
		All(&records)
	if err != nil {
		return err
	}

	now := types.NowDateTime().String()
	for _, record := range records {
		wf := s.recordToWorkflow(record)

		allCompleted := true
		anyFailed := false

		for _, taskID := range wf.Tasks {
			// allOrgs: AdvanceWorkflows is a background sweep over every
			// workflow, so its member-task reads are unscoped by design.
			t, err := s.GetTask(taskID, allOrgs)
			if err != nil {
				continue
			}
			switch t.State {
			case TaskCompleted:
				// ok
			case TaskFailed, TaskCancelled:
				anyFailed = true
				allCompleted = false
			default:
				allCompleted = false
			}
		}

		var newState TaskState
		if allCompleted && len(wf.Tasks) > 0 {
			newState = TaskCompleted
		} else if anyFailed {
			newState = TaskFailed
		} else if wf.State == TaskPending {
			newState = TaskRunning
		}

		if newState != "" && newState != wf.State {
			set := dbx.Params{"state": string(newState), "updated": now}
			if newState == TaskCompleted || newState == TaskFailed {
				set["completedAt"] = now
			}
			_, _ = s.app.NonconcurrentDB().Update(WorkflowsCollection,
				set, dbx.HashExp{"id": record.Id},
			).Execute()
		}
	}
	return nil
}

// CheckTimeouts fails or retries tasks that have exceeded their timeout.
func (s *Store) CheckTimeouts() error {
	var records []*core.Record
	err := s.app.RecordQuery(TasksCollection).
		AndWhere(dbx.HashExp{"state": string(TaskRunning)}).
		AndWhere(dbx.NewExp("[[timeout]] > 0")).
		AndWhere(dbx.NewExp("[[startedAt]] != ''")).
		All(&records)
	if err != nil {
		return err
	}

	now := time.Now()
	for _, r := range records {
		task := s.recordToTask(r)
		if task.StartedAt == nil || task.Timeout <= 0 {
			continue
		}
		if now.Sub(*task.StartedAt) <= task.Timeout {
			continue
		}

		// allOrgs: the timeout sweep runs across every org.
		_ = s.FailTask(task.ID, "task timed out", allOrgs)
	}
	return nil
}

// --- Record ↔ Task conversion ---

func (s *Store) setTaskFields(record *core.Record, task *Task) {
	record.Set("orgId", task.OrgID)
	record.Set("spaceId", task.SpaceID)
	record.Set("title", task.Title)
	record.Set("description", task.Description)
	record.Set("state", string(task.State))
	record.Set("priority", int(task.Priority))
	record.Set("assignedTo", task.AssignedTo)
	record.Set("createdBy", task.CreatedBy)
	record.Set("workflowId", task.WorkflowID)
	record.Set("parentTaskId", task.ParentTaskID)
	record.Set("error", task.Error)
	record.Set("progress", task.Progress)
	record.Set("maxRetries", task.MaxRetries)
	record.Set("retryCount", task.RetryCount)
	record.Set("timeout", int(task.Timeout.Seconds()))

	record.Set("dependsOn", marshalJSON(task.DependsOn))
	record.Set("labels", marshalJSON(task.Labels))
	record.Set("input", marshalJSON(task.Input))
	record.Set("output", marshalJSON(task.Output))
	record.Set("metadata", marshalJSON(task.Metadata))

	if task.StartedAt != nil {
		dt, _ := types.ParseDateTime(*task.StartedAt)
		record.Set("startedAt", dt)
	}
	if task.CompletedAt != nil {
		dt, _ := types.ParseDateTime(*task.CompletedAt)
		record.Set("completedAt", dt)
	}
}

func (s *Store) recordToTask(record *core.Record) *Task {
	task := &Task{
		ID:           record.Id,
		OrgID:        record.GetString("orgId"),
		SpaceID:      record.GetString("spaceId"),
		Title:        record.GetString("title"),
		Description:  record.GetString("description"),
		State:        TaskState(record.GetString("state")),
		Priority:     TaskPriority(int(record.GetFloat("priority"))),
		AssignedTo:   record.GetString("assignedTo"),
		CreatedBy:    record.GetString("createdBy"),
		WorkflowID:   record.GetString("workflowId"),
		ParentTaskID: record.GetString("parentTaskId"),
		Error:        record.GetString("error"),
		Progress:     int(record.GetFloat("progress")),
		MaxRetries:   int(record.GetFloat("maxRetries")),
		RetryCount:   int(record.GetFloat("retryCount")),
		Timeout:      time.Duration(record.GetFloat("timeout")) * time.Second,
		CreatedAt:    record.GetDateTime("created").Time(),
		UpdatedAt:    record.GetDateTime("updated").Time(),
	}

	unmarshalJSONField(record.Get("dependsOn"), &task.DependsOn)
	unmarshalJSONField(record.Get("labels"), &task.Labels)
	unmarshalJSONField(record.Get("input"), &task.Input)
	unmarshalJSONField(record.Get("output"), &task.Output)
	unmarshalJSONField(record.Get("metadata"), &task.Metadata)

	if dt := record.GetDateTime("startedAt"); !dt.IsZero() {
		t := dt.Time()
		task.StartedAt = &t
	}
	if dt := record.GetDateTime("completedAt"); !dt.IsZero() {
		t := dt.Time()
		task.CompletedAt = &t
	}

	return task
}

func (s *Store) recordToWorkflow(record *core.Record) *Workflow {
	wf := &Workflow{
		ID:          record.Id,
		OrgID:       record.GetString("orgId"),
		SpaceID:     record.GetString("spaceId"),
		Name:        record.GetString("name"),
		Description: record.GetString("description"),
		State:       TaskState(record.GetString("state")),
		CreatedBy:   record.GetString("createdBy"),
		CreatedAt:   record.GetDateTime("created").Time(),
		UpdatedAt:   record.GetDateTime("updated").Time(),
	}

	unmarshalJSONField(record.Get("tasks"), &wf.Tasks)
	unmarshalJSONField(record.Get("metadata"), &wf.Metadata)

	if dt := record.GetDateTime("completedAt"); !dt.IsZero() {
		t := dt.Time()
		wf.CompletedAt = &t
	}

	return wf
}
