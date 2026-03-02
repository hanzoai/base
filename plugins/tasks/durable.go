package tasks

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/temporal"
)

// DurableConfig holds connection settings for durable task execution.
// When enabled, tasks are submitted as Temporal workflows for crash-safe execution.
// Supports both local Temporal (localhost:7233) and cloud (tasks.hanzo.ai).
type DurableConfig struct {
	// Enabled activates durable execution. Default false (SQLite-only mode).
	Enabled bool `json:"enabled" yaml:"enabled"`

	// Address is the Temporal frontend address. Default "tasks.hanzo.ai:7233".
	Address string `json:"address" yaml:"address"`

	// Namespace is the Temporal namespace. For multi-tenant, use org ID.
	// Default "default".
	Namespace string `json:"namespace" yaml:"namespace"`

	// DefaultQueue is the task queue name. Default "default".
	DefaultQueue string `json:"default_queue" yaml:"default_queue"`

	// RunWorker starts an embedded Temporal worker in this process.
	// If false, tasks are submitted but executed by external workers.
	// Default true when enabled.
	RunWorker bool `json:"run_worker" yaml:"run_worker"`
}

// DefaultDurableConfig returns production defaults with env var overrides.
//
// Env vars:
//   - TASKS_ENABLED (or HANZO_TASKS_ENABLED): "true" to enable
//   - TASKS_ADDRESS (or HANZO_TASKS_ADDRESS): Temporal frontend address
//   - TASKS_NAMESPACE (or HANZO_TASKS_NAMESPACE): Temporal namespace (org ID for multi-tenant)
//   - TASKS_QUEUE: default task queue name
//   - TASKS_WORKER: "false" to disable embedded worker
func DefaultDurableConfig() DurableConfig {
	cfg := DurableConfig{
		Enabled:      false,
		Address:      "tasks.hanzo.ai:7233",
		Namespace:    "default",
		DefaultQueue: "default",
		RunWorker:    true,
	}

	for _, key := range []string{"TASKS_ENABLED", "HANZO_TASKS_ENABLED"} {
		if v := os.Getenv(key); v == "true" || v == "1" {
			cfg.Enabled = true
			break
		}
	}
	for _, key := range []string{"TASKS_ADDRESS", "HANZO_TASKS_ADDRESS"} {
		if v := os.Getenv(key); v != "" {
			cfg.Address = v
			break
		}
	}
	for _, key := range []string{"TASKS_NAMESPACE", "HANZO_TASKS_NAMESPACE"} {
		if v := os.Getenv(key); v != "" {
			cfg.Namespace = v
			break
		}
	}
	if v := os.Getenv("TASKS_QUEUE"); v != "" {
		cfg.DefaultQueue = v
	}
	if v := os.Getenv("TASKS_WORKER"); v == "false" || v == "0" {
		cfg.RunWorker = false
	}

	return cfg
}

// DurableStore implements durable task execution via Temporal.
// Provides submit/cancel/signal/status for workflows that survive process restarts.
type DurableStore struct {
	Client    client.Client
	namespace string
	connected bool
}

// NewDurableStore connects to the Temporal service.
func NewDurableStore(addr, namespace string) (*DurableStore, error) {
	c, err := client.Dial(client.Options{
		HostPort:  addr,
		Namespace: namespace,
	})
	if err != nil {
		return nil, fmt.Errorf("tasks: failed to connect to %s: %w", addr, err)
	}
	return &DurableStore{Client: c, namespace: namespace, connected: true}, nil
}

// Close shuts down the client connection.
func (ds *DurableStore) Close() {
	if ds.Client != nil {
		ds.Client.Close()
		ds.connected = false
	}
}

// IsConnected reports whether the durable store has an active connection.
func (ds *DurableStore) IsConnected() bool {
	return ds != nil && ds.connected
}

// SubmitTask starts a durable workflow execution for a task.
// The task queue defaults to task.SpaceID (org-as-namespace for multi-tenant).
func (ds *DurableStore) SubmitTask(ctx context.Context, task *Task) error {
	queue := task.SpaceID
	if queue == "" {
		queue = "default"
	}

	opts := client.StartWorkflowOptions{
		ID:        task.ID,
		TaskQueue: queue,
	}
	if task.Timeout > 0 {
		opts.WorkflowExecutionTimeout = task.Timeout
	}
	if task.MaxRetries > 0 {
		opts.RetryPolicy = &temporal.RetryPolicy{
			MaximumAttempts:    int32(task.MaxRetries),
			InitialInterval:    2 * time.Second,
			MaximumInterval:    time.Minute,
			BackoffCoefficient: 2.0,
		}
	}

	_, err := ds.Client.ExecuteWorkflow(ctx, opts, AgentTaskWorkflow, task)
	if err != nil {
		return fmt.Errorf("tasks: failed to submit workflow: %w", err)
	}
	task.State = TaskRunning
	return nil
}

// SubmitWorkflow starts a pipeline or fan-out workflow.
func (ds *DurableStore) SubmitWorkflow(ctx context.Context, wf *Workflow, tasks []*Task, parallel bool) error {
	queue := wf.SpaceID
	if queue == "" {
		queue = "default"
	}

	opts := client.StartWorkflowOptions{
		ID:        wf.ID,
		TaskQueue: queue,
	}

	var wfFunc interface{}
	if parallel {
		wfFunc = FanOutWorkflow
	} else {
		wfFunc = PipelineWorkflow
	}

	_, err := ds.Client.ExecuteWorkflow(ctx, opts, wfFunc, wf, tasks)
	if err != nil {
		return fmt.Errorf("tasks: failed to submit workflow: %w", err)
	}
	wf.State = TaskRunning
	return nil
}

// GetTaskStatus queries a running workflow for its current state.
func (ds *DurableStore) GetTaskStatus(ctx context.Context, taskID string) (TaskState, string, error) {
	desc, err := ds.Client.DescribeWorkflowExecution(ctx, taskID, "")
	if err != nil {
		return TaskPending, "", err
	}

	info := desc.WorkflowExecutionInfo
	switch info.GetStatus().String() {
	case "Running":
		return TaskRunning, "", nil
	case "Completed":
		return TaskCompleted, "", nil
	case "Failed":
		return TaskFailed, "workflow failed", nil
	case "Canceled", "Cancelled":
		return TaskCancelled, "", nil
	case "TimedOut":
		return TaskFailed, "timed out", nil
	default:
		return TaskPending, "", nil
	}
}

// CancelTask cancels a running workflow.
func (ds *DurableStore) CancelTask(ctx context.Context, taskID string) error {
	return ds.Client.CancelWorkflow(ctx, taskID, "")
}

// SignalTask sends a signal to a running workflow.
func (ds *DurableStore) SignalTask(ctx context.Context, taskID, signalName string, data interface{}) error {
	return ds.Client.SignalWorkflow(ctx, taskID, "", signalName, data)
}

// logDurableError logs a durable store error without failing the operation.
func logDurableError(logger *slog.Logger, op string, taskID string, err error) {
	if logger != nil {
		logger.Warn("tasks: durable "+op+" failed, SQLite state is authoritative",
			slog.String("task_id", taskID),
			slog.String("error", err.Error()),
		)
	}
}
