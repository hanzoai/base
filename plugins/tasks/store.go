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
// If orgID is provided, verifies the task belongs to that org.
func (s *Store) GetTask(id string, orgID ...string) (*Task, error) {
	record, err := s.app.FindRecordById(TasksCollection, id)
	if err != nil {
		return nil, ErrTaskNotFound
	}
	task := s.recordToTask(record)
	if len(orgID) > 0 && orgID[0] != "" && task.OrgID != orgID[0] {
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

// --- Atomic state transitions ---

// transition applies one state change as a single guarded UPDATE, and reports
// the two ways it can decline.
//
// Every transition below is the same shape: set some columns, but only on a row
// in an acceptable starting state, owned by the caller's org. Writing that shape
// out six times meant six copies of the org-scoping clause and six copies of the
// nothing-matched branch — and the org clause is an authorization boundary, so
// one copy drifting is one caller mutating another org's task.
//
// from is the states this transition will accept; nil accepts any.
func (s *Store) transition(taskID string, set dbx.Params, from dbx.Expression, conflict error, orgID []string) error {
	where := []dbx.Expression{dbx.HashExp{"id": taskID}}
	if from != nil {
		where = append(where, from)
	}
	if len(orgID) > 0 && orgID[0] != "" {
		where = append(where, dbx.HashExp{"orgId": orgID[0]})
	}

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
		if _, err := s.GetTask(taskID, orgID...); err != nil {
			return ErrTaskNotFound
		}
		return conflict
	}
	return nil
}

// ClaimTask atomically transitions a task from pending to claimed.
// orgID scopes the mutation to a specific org when provided.
func (s *Store) ClaimTask(taskID, agentID string, orgID ...string) error {
	now := types.NowDateTime().String()
	return s.transition(taskID,
		dbx.Params{"state": string(TaskClaimed), "assignedTo": agentID, "updated": now},
		dbx.HashExp{"state": string(TaskPending)},
		ErrAlreadyClaimed, orgID)
}

// StartTask transitions a claimed (or pending) task to running.
// orgID scopes the mutation to a specific org when provided.
func (s *Store) StartTask(taskID string, orgID ...string) error {
	now := types.NowDateTime().String()
	return s.transition(taskID,
		dbx.Params{"state": string(TaskRunning), "startedAt": now, "updated": now},
		dbx.In("state", string(TaskClaimed), string(TaskPending)),
		ErrInvalidTransition, orgID)
}

// CompleteTask transitions a running task to completed with output.
// orgID scopes the mutation to a specific org when provided.
func (s *Store) CompleteTask(taskID string, output map[string]any, orgID ...string) error {
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
		ErrInvalidTransition, orgID)
}

// FailTask transitions a running task to failed. If retries remain, re-queues as pending.
// Uses a single atomic SQL with CASE to avoid TOCTOU races.
// orgID scopes the mutation to a specific org when provided.
func (s *Store) FailTask(taskID string, errMsg string, orgID ...string) error {
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
		ErrInvalidTransition, orgID)
}

// CancelTask transitions any non-terminal task to cancelled.
// orgID scopes the mutation to a specific org when provided.
func (s *Store) CancelTask(taskID string, orgID ...string) error {
	now := types.NowDateTime().String()
	return s.transition(taskID,
		dbx.Params{"state": string(TaskCancelled), "completedAt": now, "updated": now},
		dbx.NotIn("state", string(TaskCompleted), string(TaskCancelled)),
		ErrInvalidTransition, orgID)
}

// UpdateProgress sets progress (0-100) on a running task.
// orgID scopes the mutation to a specific org when provided.
func (s *Store) UpdateProgress(taskID string, progress int, orgID ...string) error {
	if progress < 0 {
		progress = 0
	}
	if progress > 100 {
		progress = 100
	}

	return s.transition(taskID,
		dbx.Params{"progress": progress, "updated": types.NowDateTime().String()},
		dbx.HashExp{"state": string(TaskRunning)},
		ErrInvalidTransition, orgID)
}

// GetNextPendingTask finds and atomically claims the highest-priority pending task
// in the given space whose dependencies are all completed.
// orgID scopes the query to a specific org when provided.
func (s *Store) GetNextPendingTask(spaceID, agentID string, orgID ...string) (*Task, error) {
	pending := TaskPending
	filters := TaskFilters{
		SpaceID: spaceID,
		State:   &pending,
		Limit:   50,
	}
	if len(orgID) > 0 && orgID[0] != "" {
		filters.OrgID = orgID[0]
	}
	candidates, err := s.ListTasks(filters)
	if err != nil {
		return nil, err
	}

	for _, task := range candidates {
		if !s.dependenciesMet(task) {
			continue
		}

		// Attempt atomic claim scoped to org.
		if err := s.ClaimTask(task.ID, agentID, orgID...); err != nil {
			continue // lost race or invalid transition
		}

		// Re-read the claimed task scoped to org.
		claimed, err := s.GetTask(task.ID, orgID...)
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
		dep, err := s.GetTask(depID)
		if err != nil || dep.State != TaskCompleted {
			return false
		}
	}
	return true
}

// AgentHasActiveTask reports whether the agent has a claimed or running task.
// orgID scopes the query to a specific org when provided.
func (s *Store) AgentHasActiveTask(agentID string, orgID ...string) (bool, error) {
	where := []dbx.Expression{
		dbx.HashExp{"assignedTo": agentID},
		dbx.In("state", string(TaskClaimed), string(TaskRunning)),
	}
	if len(orgID) > 0 && orgID[0] != "" {
		where = append(where, dbx.HashExp{"orgId": orgID[0]})
	}

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
// If orgID is provided, verifies the workflow belongs to that org.
func (s *Store) GetWorkflow(id string, orgID ...string) (*Workflow, error) {
	record, err := s.app.FindRecordById(WorkflowsCollection, id)
	if err != nil {
		return nil, ErrWorkflowNotFound
	}
	wf := s.recordToWorkflow(record)
	if len(orgID) > 0 && orgID[0] != "" && wf.OrgID != orgID[0] {
		return nil, ErrWorkflowNotFound
	}
	return wf, nil
}

// ListWorkflows returns workflows for a space, optionally scoped to an org.
func (s *Store) ListWorkflows(spaceID string, orgID ...string) ([]*Workflow, error) {
	query := s.app.RecordQuery(WorkflowsCollection).
		OrderBy("created ASC")

	if len(orgID) > 0 && orgID[0] != "" {
		query = query.AndWhere(dbx.HashExp{"orgId": orgID[0]})
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
			t, err := s.GetTask(taskID)
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

		_ = s.FailTask(task.ID, "task timed out")
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
