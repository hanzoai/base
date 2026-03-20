package tasks

import (
	"fmt"
	"strings"
	"time"

	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

// AgentTaskWorkflow is the primary durable workflow for executing a single task.
// Survives process crashes and restarts via Temporal.
func AgentTaskWorkflow(ctx workflow.Context, task *Task) (*Task, error) {
	actOpts := workflow.ActivityOptions{
		StartToCloseTimeout: task.Timeout,
		HeartbeatTimeout:    30 * time.Second,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts:    int32(task.MaxRetries),
			InitialInterval:    2 * time.Second,
			MaximumInterval:    time.Minute,
			BackoffCoefficient: 2.0,
		},
	}
	if actOpts.StartToCloseTimeout == 0 {
		actOpts.StartToCloseTimeout = time.Hour
	}
	ctx = workflow.WithActivityOptions(ctx, actOpts)

	var result Task
	err := workflow.ExecuteActivity(ctx, ExecuteTaskActivity, task).Get(ctx, &result)
	if err != nil {
		task.State = TaskFailed
		task.Error = err.Error()
		return task, err
	}

	task.State = TaskCompleted
	task.Output = result.Output
	now := time.Now().UTC()
	task.CompletedAt = &now
	return task, nil
}

// PipelineWorkflow runs tasks sequentially — each waits for the previous to complete.
func PipelineWorkflow(ctx workflow.Context, wf *Workflow, tasks []*Task) (*Workflow, error) {
	for i, task := range tasks {
		actOpts := workflow.ActivityOptions{
			StartToCloseTimeout: task.Timeout,
			HeartbeatTimeout:    30 * time.Second,
		}
		if actOpts.StartToCloseTimeout == 0 {
			actOpts.StartToCloseTimeout = time.Hour
		}
		actCtx := workflow.WithActivityOptions(ctx, actOpts)

		var result Task
		err := workflow.ExecuteActivity(actCtx, ExecuteTaskActivity, task).Get(ctx, &result)
		if err != nil {
			wf.State = TaskFailed
			return wf, fmt.Errorf("step %d (%s) failed: %w", i, task.Title, err)
		}
		tasks[i] = &result
	}

	wf.State = TaskCompleted
	now := time.Now().UTC()
	wf.CompletedAt = &now
	return wf, nil
}

// FanOutWorkflow runs tasks in parallel and waits for all to complete.
func FanOutWorkflow(ctx workflow.Context, wf *Workflow, tasks []*Task) (*Workflow, error) {
	var futures []workflow.Future

	for _, task := range tasks {
		actOpts := workflow.ActivityOptions{
			StartToCloseTimeout: task.Timeout,
			HeartbeatTimeout:    30 * time.Second,
		}
		if actOpts.StartToCloseTimeout == 0 {
			actOpts.StartToCloseTimeout = time.Hour
		}
		actCtx := workflow.WithActivityOptions(ctx, actOpts)

		future := workflow.ExecuteActivity(actCtx, ExecuteTaskActivity, task)
		futures = append(futures, future)
	}

	var errs []string
	for i, future := range futures {
		var result Task
		if err := future.Get(ctx, &result); err != nil {
			errs = append(errs, fmt.Sprintf("task %d: %v", i, err))
		}
	}

	if len(errs) > 0 {
		wf.State = TaskFailed
		return wf, fmt.Errorf("fan-out failures: %s", strings.Join(errs, "; "))
	}

	wf.State = TaskCompleted
	now := time.Now().UTC()
	wf.CompletedAt = &now
	return wf, nil
}
