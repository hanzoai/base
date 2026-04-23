package tasks

import (
	"context"
	"fmt"

	"github.com/hanzoai/tasks/pkg/sdk/activity"
)

// ExecuteTaskActivity is the Temporal activity that executes a task.
// Delegates to the Config.OnExecute callback registered during plugin setup.
//
// If no executor is configured, the task completes with an acknowledgment message.
var taskExecutor func(ctx context.Context, task *Task) (*Task, error)

// SetTaskExecutor registers the function called by Temporal activities.
// This is typically set during plugin registration via Config.OnDurableExecute.
func SetTaskExecutor(fn func(ctx context.Context, task *Task) (*Task, error)) {
	taskExecutor = fn
}

// ExecuteTaskActivity is the activity implementation called by Temporal workflows.
func ExecuteTaskActivity(ctx context.Context, task *Task) (*Task, error) {
	logger := activity.GetLogger(ctx)
	logger.Info("Executing task",
		"task_id", task.ID,
		"space_id", task.SpaceID,
		"title", task.Title,
		"assigned_to", task.AssignedTo,
	)

	if taskExecutor == nil {
		task.State = TaskCompleted
		task.Output = map[string]any{"message": "No executor registered. Task acknowledged."}
		return task, nil
	}

	// Heartbeat so Temporal knows we're alive.
	activity.RecordHeartbeat(ctx, "executing")

	result, err := taskExecutor(ctx, task)
	if err != nil {
		return nil, fmt.Errorf("task execution failed: %w", err)
	}

	return result, nil
}
