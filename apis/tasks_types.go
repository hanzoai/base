package apis

// TaskCreateRequest is the body for POST /api/tasks.
type TaskCreateRequest struct {
	SpaceID      string            `json:"space_id"`
	Title        string            `json:"title"`
	Name         string            `json:"name"`                    // alias for title
	Queue        string            `json:"queue"`                   // alias for space_id
	Description  string            `json:"description"`
	Priority     int               `json:"priority"`                // 0=low, 1=normal, 2=high, 3=critical
	AssignedTo   string            `json:"assigned_to"`
	WorkflowID   string            `json:"workflow_id"`
	ParentTaskID string            `json:"parent_task_id"`
	DependsOn    []string          `json:"depends_on"`
	Labels       []string          `json:"labels"`
	Input        map[string]any    `json:"input,omitempty"`
	MaxRetries   int               `json:"max_retries"`
	TimeoutSecs  int               `json:"timeout_secs"`
	Timeout      string            `json:"timeout,omitempty"`       // e.g. "1h", "30m" (parsed if timeout_secs=0)
	Metadata     map[string]string `json:"metadata,omitempty"`
	Retry        *RetryConfig      `json:"retry,omitempty"`         // alternative retry config
}

// RetryConfig controls retry behavior for a task.
type RetryConfig struct {
	MaxAttempts int     `json:"max_attempts"`
	Initial     string  `json:"initial_interval"` // e.g. "1s"
	Max         string  `json:"max_interval"`     // e.g. "1m"
	Backoff     float64 `json:"backoff_coefficient"`
}

// TaskUpdateRequest is the body for PUT /api/tasks/{id}.
type TaskUpdateRequest struct {
	Title       *string           `json:"title"`
	Description *string           `json:"description"`
	Priority    *int              `json:"priority"`
	Labels      []string          `json:"labels"`
	Metadata    map[string]string `json:"metadata"`
}

// TaskClaimRequest is the body for POST /api/tasks/{id}/claim.
type TaskClaimRequest struct {
	AgentID string `json:"agent_id"`
}

// TaskCompleteRequest is the body for POST /api/tasks/{id}/complete.
type TaskCompleteRequest struct {
	Output map[string]any `json:"output"`
}

// TaskFailRequest is the body for POST /api/tasks/{id}/fail.
type TaskFailRequest struct {
	Error string `json:"error"`
}

// TaskProgressRequest is the body for POST /api/tasks/{id}/progress.
type TaskProgressRequest struct {
	Progress int `json:"progress"`
}

// TaskNextRequest is the body for POST /api/tasks/next.
type TaskNextRequest struct {
	SpaceID string `json:"space_id"`
	Queue   string `json:"queue"`    // alias for space_id
	AgentID string `json:"agent_id"`
}

// TaskSignalRequest is the body for POST /api/tasks/{id}/signal.
type TaskSignalRequest struct {
	Name string `json:"name"`
	Data any    `json:"data,omitempty"`
}

// WorkflowCreateRequest is the body for POST /api/tasks/workflows.
type WorkflowCreateRequest struct {
	SpaceID     string              `json:"space_id"`
	Queue       string              `json:"queue"`       // alias for space_id
	Name        string              `json:"name"`
	Description string              `json:"description"`
	Tasks       []TaskCreateRequest `json:"tasks"`
	Steps       []TaskCreateRequest `json:"steps"`       // alias for tasks
	Parallel    bool                `json:"parallel"`    // fan-out mode
	Metadata    map[string]string   `json:"metadata"`
}
