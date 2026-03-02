// Package tasks implements a durable task execution plugin for Base.
//
// It provides persistent task queues with DAG-based workflow scheduling,
// priority-based work-stealing, retry support, and timeout management,
// all backed by Base's embedded SQLite.
//
// The plugin uses two internal collections:
//   - _tasks: individual work items with state machine lifecycle
//   - _workflows: ordered task DAGs (pipeline or fan-out)
package tasks

import (
	"encoding/json"
	"time"

	"github.com/hanzoai/base/tools/types"
)

// TaskState represents the lifecycle state of a task.
type TaskState string

const (
	TaskPending   TaskState = "pending"
	TaskClaimed   TaskState = "claimed"
	TaskRunning   TaskState = "running"
	TaskCompleted TaskState = "completed"
	TaskFailed    TaskState = "failed"
	TaskCancelled TaskState = "cancelled"
	TaskRetrying  TaskState = "retrying"
)

// validTransitions defines which state transitions are allowed.
var validTransitions = map[TaskState]map[TaskState]bool{
	TaskPending:   {TaskClaimed: true, TaskRunning: true, TaskCancelled: true},
	TaskClaimed:   {TaskRunning: true, TaskCancelled: true},
	TaskRunning:   {TaskCompleted: true, TaskFailed: true, TaskCancelled: true},
	TaskFailed:    {TaskRetrying: true, TaskCancelled: true},
	TaskRetrying:  {TaskPending: true, TaskCancelled: true},
	TaskCompleted: {},
	TaskCancelled: {},
}

// CanTransition reports whether moving from one state to another is allowed.
func CanTransition(from, to TaskState) bool {
	targets, ok := validTransitions[from]
	if !ok {
		return false
	}
	return targets[to]
}

// TaskPriority controls scheduling order. Higher values are scheduled first.
type TaskPriority int

const (
	PriorityLow    TaskPriority = 0
	PriorityNormal TaskPriority = 1
	PriorityHigh   TaskPriority = 2
	PriorityUrgent TaskPriority = 3
)

// Task is a durable work item.
type Task struct {
	ID           string            `json:"id"`
	OrgID        string            `json:"org_id,omitempty"`  // IAM org — maps to Temporal namespace
	SpaceID      string            `json:"space_id"`
	Title        string            `json:"title"`
	Description  string            `json:"description,omitempty"`
	State        TaskState         `json:"state"`
	Priority     TaskPriority      `json:"priority"`
	AssignedTo   string            `json:"assigned_to,omitempty"`
	CreatedBy    string            `json:"created_by,omitempty"`
	WorkflowID   string            `json:"workflow_id,omitempty"`
	ParentTaskID string            `json:"parent_task_id,omitempty"`
	DependsOn    []string          `json:"depends_on,omitempty"`
	Labels       []string          `json:"labels,omitempty"`
	Input        map[string]any    `json:"input,omitempty"`
	Output       map[string]any    `json:"output,omitempty"`
	Error        string            `json:"error,omitempty"`
	Progress     int               `json:"progress"`
	MaxRetries   int               `json:"max_retries"`
	RetryCount   int               `json:"retry_count"`
	Timeout      time.Duration     `json:"timeout,omitempty"`
	CreatedAt    time.Time         `json:"created_at"`
	UpdatedAt    time.Time         `json:"updated_at"`
	StartedAt    *time.Time        `json:"started_at,omitempty"`
	CompletedAt  *time.Time        `json:"completed_at,omitempty"`
	Metadata     map[string]string `json:"metadata,omitempty"`
}

// Workflow chains tasks into a DAG.
type Workflow struct {
	ID          string            `json:"id"`
	OrgID       string            `json:"org_id,omitempty"` // IAM org — maps to Temporal namespace
	SpaceID     string            `json:"space_id"`
	Name        string            `json:"name"`
	Description string            `json:"description,omitempty"`
	State       TaskState         `json:"state"`
	Tasks       []string          `json:"tasks"`
	CreatedBy   string            `json:"created_by,omitempty"`
	CreatedAt   time.Time         `json:"created_at"`
	UpdatedAt   time.Time         `json:"updated_at"`
	CompletedAt *time.Time        `json:"completed_at,omitempty"`
	Metadata    map[string]string `json:"metadata,omitempty"`
}

// TaskFilters controls listing and searching.
type TaskFilters struct {
	OrgID      string        `json:"org_id,omitempty"`
	SpaceID    string        `json:"space_id,omitempty"`
	State      *TaskState    `json:"state,omitempty"`
	AssignedTo *string       `json:"assigned_to,omitempty"`
	Priority   *TaskPriority `json:"priority,omitempty"`
	Labels     []string      `json:"labels,omitempty"`
	WorkflowID *string       `json:"workflow_id,omitempty"`
	Limit      int           `json:"limit,omitempty"`
	Offset     int           `json:"offset,omitempty"`
}

// marshalJSON marshals v to types.JSONRaw, returning nil on error.
func marshalJSON(v any) types.JSONRaw {
	if v == nil {
		return nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	return types.JSONRaw(b)
}

// unmarshalJSONField decodes a record's raw JSON field into dest.
func unmarshalJSONField(raw any, dest any) {
	if raw == nil {
		return
	}
	switch v := raw.(type) {
	case types.JSONRaw:
		if len(v) > 0 {
			_ = json.Unmarshal(v, dest)
		}
	case string:
		if v != "" {
			_ = json.Unmarshal([]byte(v), dest)
		}
	case []byte:
		if len(v) > 0 {
			_ = json.Unmarshal(v, dest)
		}
	}
}
