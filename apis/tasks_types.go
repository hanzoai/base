package apis

// TaskSubmitRequest is the body for POST /api/tasks.
type TaskSubmitRequest struct {
	Name     string         `json:"name"`
	Queue    string         `json:"queue"`
	Input    map[string]any `json:"input,omitempty"`
	Priority int            `json:"priority,omitempty"` // 0=low, 1=normal, 2=high, 3=critical
	Timeout  string         `json:"timeout,omitempty"`  // e.g. "1h", "30m"
	Retry    *RetryConfig   `json:"retry,omitempty"`
}

// RetryConfig controls retry behavior for a task.
type RetryConfig struct {
	MaxAttempts int     `json:"max_attempts"`
	Initial     string  `json:"initial_interval"` // e.g. "1s"
	Max         string  `json:"max_interval"`     // e.g. "1m"
	Backoff     float64 `json:"backoff_coefficient"`
}

// TaskResponse is the response shape for task endpoints.
type TaskResponse struct {
	ID        string         `json:"id"`
	Name      string         `json:"name"`
	Queue     string         `json:"queue"`
	State     string         `json:"state"`
	Input     map[string]any `json:"input,omitempty"`
	Output    map[string]any `json:"output,omitempty"`
	Error     string         `json:"error,omitempty"`
	Created   string         `json:"created"`
	Started   string         `json:"started,omitempty"`
	Completed string         `json:"completed,omitempty"`
}

// TaskSignalRequest is the body for POST /api/tasks/{id}/signal.
type TaskSignalRequest struct {
	Name string `json:"name"`
	Data any    `json:"data,omitempty"`
}

// WorkflowSubmitRequest is the body for POST /api/tasks/workflows.
type WorkflowSubmitRequest struct {
	Name     string              `json:"name"`
	Queue    string              `json:"queue"`
	Input    map[string]any      `json:"input,omitempty"`
	Steps    []TaskSubmitRequest `json:"steps,omitempty"`    // for pipeline workflows
	Parallel bool               `json:"parallel,omitempty"` // fan-out mode
}

// WorkflowResponse is the response shape for workflow endpoints.
type WorkflowResponse struct {
	ID        string         `json:"id"`
	Name      string         `json:"name"`
	Queue     string         `json:"queue"`
	State     string         `json:"state"`
	Input     map[string]any `json:"input,omitempty"`
	Output    map[string]any `json:"output,omitempty"`
	Error     string         `json:"error,omitempty"`
	Tasks     []TaskResponse `json:"tasks,omitempty"`
	Created   string         `json:"created"`
	Started   string         `json:"started,omitempty"`
	Completed string         `json:"completed,omitempty"`
}
