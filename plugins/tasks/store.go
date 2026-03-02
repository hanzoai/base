package tasks

import (
	"errors"
	"fmt"
	"time"

	"github.com/hanzoai/dbx"
	"github.com/hanzoai/base/core"
	"github.com/hanzoai/base/tools/types"
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

// --- Atomic state transitions (raw SQL for exactly-once semantics) ---

// ClaimTask atomically transitions a task from pending to claimed.
// orgID scopes the mutation to a specific org when provided.
func (s *Store) ClaimTask(taskID, agentID string, orgID ...string) error {
	now := types.NowDateTime()
	where := "WHERE [[id]] = {:id} AND [[state]] = {:pending}"
	params := dbx.Params{
		"state":   string(TaskClaimed),
		"agent":   agentID,
		"now":     now.String(),
		"id":      taskID,
		"pending": string(TaskPending),
	}
	if len(orgID) > 0 && orgID[0] != "" {
		where += " AND [[orgId]] = {:orgId}"
		params["orgId"] = orgID[0]
	}

	result, err := s.app.NonconcurrentDB().NewQuery(
		"UPDATE {{" + TasksCollection + "}} SET " +
			"[[state]] = {:state}, [[assignedTo]] = {:agent}, [[updated]] = {:now} " +
			where,
	).Bind(params).Execute()
	if err != nil {
		return err
	}

	rows, _ := result.RowsAffected()
	if rows == 0 {
		// Check if task exists and belongs to the caller's org.
		if _, err := s.GetTask(taskID, orgID...); err != nil {
			return ErrTaskNotFound
		}
		return ErrAlreadyClaimed
	}
	return nil
}

// StartTask transitions a claimed (or pending) task to running.
// orgID scopes the mutation to a specific org when provided.
func (s *Store) StartTask(taskID string, orgID ...string) error {
	now := types.NowDateTime()
	where := "WHERE [[id]] = {:id} AND ([[state]] = {:claimed} OR [[state]] = {:pending})"
	params := dbx.Params{
		"running": string(TaskRunning),
		"now":     now.String(),
		"id":      taskID,
		"claimed": string(TaskClaimed),
		"pending": string(TaskPending),
	}
	if len(orgID) > 0 && orgID[0] != "" {
		where += " AND [[orgId]] = {:orgId}"
		params["orgId"] = orgID[0]
	}

	result, err := s.app.NonconcurrentDB().NewQuery(
		"UPDATE {{" + TasksCollection + "}} SET " +
			"[[state]] = {:running}, [[startedAt]] = {:now}, [[updated]] = {:now} " +
			where,
	).Bind(params).Execute()
	if err != nil {
		return err
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		if _, err := s.GetTask(taskID, orgID...); err != nil {
			return ErrTaskNotFound
		}
		return ErrInvalidTransition
	}
	return nil
}

// CompleteTask transitions a running task to completed with output.
// orgID scopes the mutation to a specific org when provided.
func (s *Store) CompleteTask(taskID string, output map[string]any, orgID ...string) error {
	now := types.NowDateTime()
	outputJSON := marshalJSON(output)

	where := "WHERE [[id]] = {:id} AND [[state]] = {:running}"
	params := dbx.Params{
		"state":   string(TaskCompleted),
		"output":  string(outputJSON),
		"now":     now.String(),
		"id":      taskID,
		"running": string(TaskRunning),
	}
	if len(orgID) > 0 && orgID[0] != "" {
		where += " AND [[orgId]] = {:orgId}"
		params["orgId"] = orgID[0]
	}

	result, err := s.app.NonconcurrentDB().NewQuery(
		"UPDATE {{" + TasksCollection + "}} SET " +
			"[[state]] = {:state}, [[output]] = {:output}, [[progress]] = 100, " +
			"[[completedAt]] = {:now}, [[updated]] = {:now} " +
			where,
	).Bind(params).Execute()
	if err != nil {
		return err
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		if _, err := s.GetTask(taskID, orgID...); err != nil {
			return ErrTaskNotFound
		}
		return ErrInvalidTransition
	}
	return nil
}

// FailTask transitions a running task to failed. If retries remain, re-queues as pending.
// Uses a single atomic SQL with CASE to avoid TOCTOU races.
// orgID scopes the mutation to a specific org when provided.
func (s *Store) FailTask(taskID string, errMsg string, orgID ...string) error {
	now := types.NowDateTime()
	where := "WHERE [[id]] = {:id} AND [[state]] = {:running}"
	params := dbx.Params{
		"pending": string(TaskPending),
		"failed":  string(TaskFailed),
		"error":   errMsg,
		"now":     now.String(),
		"id":      taskID,
		"running": string(TaskRunning),
	}
	if len(orgID) > 0 && orgID[0] != "" {
		where += " AND [[orgId]] = {:orgId}"
		params["orgId"] = orgID[0]
	}

	result, err := s.app.NonconcurrentDB().NewQuery(
		"UPDATE {{" + TasksCollection + "}} SET " +
			"[[state]] = CASE WHEN [[retryCount]] < [[maxRetries]] THEN {:pending} ELSE {:failed} END, " +
			"[[retryCount]] = CASE WHEN [[retryCount]] < [[maxRetries]] THEN [[retryCount]] + 1 ELSE [[retryCount]] END, " +
			"[[assignedTo]] = CASE WHEN [[retryCount]] < [[maxRetries]] THEN '' ELSE [[assignedTo]] END, " +
			"[[startedAt]] = CASE WHEN [[retryCount]] < [[maxRetries]] THEN '' ELSE [[startedAt]] END, " +
			"[[progress]] = CASE WHEN [[retryCount]] < [[maxRetries]] THEN 0 ELSE [[progress]] END, " +
			"[[completedAt]] = CASE WHEN [[retryCount]] >= [[maxRetries]] THEN {:now} ELSE [[completedAt]] END, " +
			"[[error]] = {:error}, [[updated]] = {:now} " +
			where,
	).Bind(params).Execute()
	if err != nil {
		return err
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		if _, err := s.GetTask(taskID, orgID...); err != nil {
			return ErrTaskNotFound
		}
		return ErrInvalidTransition
	}
	return nil
}

// CancelTask transitions any non-terminal task to cancelled.
// orgID scopes the mutation to a specific org when provided.
func (s *Store) CancelTask(taskID string, orgID ...string) error {
	now := types.NowDateTime()
	where := "WHERE [[id]] = {:id} AND [[state]] NOT IN ({:completed}, {:cancelled})"
	params := dbx.Params{
		"cancelled": string(TaskCancelled),
		"completed": string(TaskCompleted),
		"now":       now.String(),
		"id":        taskID,
	}
	if len(orgID) > 0 && orgID[0] != "" {
		where += " AND [[orgId]] = {:orgId}"
		params["orgId"] = orgID[0]
	}

	result, err := s.app.NonconcurrentDB().NewQuery(
		"UPDATE {{" + TasksCollection + "}} SET " +
			"[[state]] = {:cancelled}, [[completedAt]] = {:now}, [[updated]] = {:now} " +
			where,
	).Bind(params).Execute()
	if err != nil {
		return err
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		if _, err := s.GetTask(taskID, orgID...); err != nil {
			return ErrTaskNotFound
		}
		return ErrInvalidTransition
	}
	return nil
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

	now := types.NowDateTime()
	where := "WHERE [[id]] = {:id} AND [[state]] = {:running}"
	params := dbx.Params{
		"progress": progress,
		"now":      now.String(),
		"id":       taskID,
		"running":  string(TaskRunning),
	}
	if len(orgID) > 0 && orgID[0] != "" {
		where += " AND [[orgId]] = {:orgId}"
		params["orgId"] = orgID[0]
	}

	result, err := s.app.NonconcurrentDB().NewQuery(
		"UPDATE {{" + TasksCollection + "}} SET " +
			"[[progress]] = {:progress}, [[updated]] = {:now} " +
			where,
	).Bind(params).Execute()
	if err != nil {
		return err
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		if _, err := s.GetTask(taskID, orgID...); err != nil {
			return ErrTaskNotFound
		}
		return ErrInvalidTransition
	}
	return nil
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
	where := "WHERE [[assignedTo]] = {:agent} AND [[state]] IN ({:claimed}, {:running})"
	params := dbx.Params{
		"agent":   agentID,
		"claimed": string(TaskClaimed),
		"running": string(TaskRunning),
	}
	if len(orgID) > 0 && orgID[0] != "" {
		where += " AND [[orgId]] = {:orgId}"
		params["orgId"] = orgID[0]
	}

	var count int
	err := s.app.ConcurrentDB().NewQuery(
		"SELECT COUNT(*) FROM {{" + TasksCollection + "}} " + where,
	).Bind(params).Row(&count)
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
	now := types.NowDateTime()
	where := "WHERE [[id]] = {:id}"
	params := dbx.Params{
		"tasks": string(marshalJSON(wf.Tasks)),
		"now":   now.String(),
		"id":    wf.ID,
	}
	if wf.OrgID != "" {
		where += " AND [[orgId]] = {:orgId}"
		params["orgId"] = wf.OrgID
	}

	_, err := s.app.NonconcurrentDB().NewQuery(
		"UPDATE {{" + WorkflowsCollection + "}} SET [[tasks]] = {:tasks}, [[updated]] = {:now} " + where,
	).Bind(params).Execute()
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

	now := types.NowDateTime()
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
			params := dbx.Params{
				"state": string(newState),
				"now":   now.String(),
				"id":    record.Id,
			}
			query := "UPDATE {{" + WorkflowsCollection + "}} SET [[state]] = {:state}, [[updated]] = {:now}"
			if newState == TaskCompleted || newState == TaskFailed {
				query += ", [[completedAt]] = {:now}"
			}
			query += " WHERE [[id]] = {:id}"

			_, _ = s.app.NonconcurrentDB().NewQuery(query).Bind(params).Execute()
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
