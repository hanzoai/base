package tasks

import (
	"fmt"
	"strings"
	"time"

	"github.com/hanzoai/tasks/pkg/sdk/temporal"
	"github.com/hanzoai/tasks/pkg/sdk/workflow"
)

// Signal names used by handlers to control task workflows.
const (
	SignalClaim    = "claim"
	SignalComplete = "complete"
	SignalFail     = "fail"
	SignalProgress = "progress"
	SignalUpdate   = "update"
	SignalCancel   = "cancel"
)

// AgentTaskWorkflow is the primary durable workflow for executing a single task.
// It supports two modes:
//
//  1. Auto-execute: if OnDurableExecute is configured, runs the activity immediately.
//  2. Signal-driven: waits for external signals (claim/complete/fail) from handlers.
//     This is the human-in-the-loop / agent-in-the-loop pattern.
func AgentTaskWorkflow(ctx workflow.Context, task *Task) (*Task, error) {
	logger := workflow.GetLogger(ctx)
	task.State = TaskPending

	// Set up signal channels.
	claimCh := workflow.GetSignalChannel(ctx, SignalClaim)
	completeCh := workflow.GetSignalChannel(ctx, SignalComplete)
	failCh := workflow.GetSignalChannel(ctx, SignalFail)
	progressCh := workflow.GetSignalChannel(ctx, SignalProgress)
	cancelCh := workflow.GetSignalChannel(ctx, SignalCancel)

	// Workflow timeout: use task timeout or default to 24h.
	timeout := task.Timeout
	if timeout == 0 {
		timeout = 24 * time.Hour
	}
	timerCtx, cancelTimer := workflow.WithCancel(ctx)
	timerFuture := workflow.NewTimer(timerCtx, timeout)

	// --- Phase 1: Wait for claim or auto-execute ---

	// Check if auto-execution is available by trying to run the activity.
	// If no executor is registered, the activity returns immediately with ack.
	actOpts := workflow.ActivityOptions{
		StartToCloseTimeout: timeout,
		HeartbeatTimeout:    30 * time.Second,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts:    int32(task.MaxRetries),
			InitialInterval:    2 * time.Second,
			MaximumInterval:    time.Minute,
			BackoffCoefficient: 2.0,
		},
	}
	actCtx := workflow.WithActivityOptions(ctx, actOpts)

	// Start activity in background — it may complete on its own or we may
	// receive signals that override the result.
	actFuture := workflow.ExecuteActivity(actCtx, ExecuteTaskActivity, task)

	// --- Phase 2: Listen for signals until completion ---

	for {
		selector := workflow.NewSelector(ctx)

		// Activity completed (auto-execute path).
		selector.AddFuture(actFuture, func(f workflow.Future) {
			var result Task
			if err := f.Get(ctx, &result); err != nil {
				task.State = TaskFailed
				task.Error = err.Error()
			} else {
				task.State = TaskCompleted
				task.Output = result.Output
				task.Progress = 100
			}
		})

		// Timeout.
		selector.AddFuture(timerFuture, func(f workflow.Future) {
			if err := f.Get(ctx, nil); err == nil {
				task.State = TaskFailed
				task.Error = "task timed out"
			}
		})

		// Claim signal.
		selector.AddReceive(claimCh, func(ch workflow.ReceiveChannel, more bool) {
			var data map[string]string
			ch.Receive(ctx, &data)
			task.State = TaskClaimed
			task.AssignedTo = data["agent_id"]
			logger.Info("Task claimed", "agent_id", task.AssignedTo)
		})

		// Complete signal.
		selector.AddReceive(completeCh, func(ch workflow.ReceiveChannel, more bool) {
			var output map[string]any
			ch.Receive(ctx, &output)
			task.State = TaskCompleted
			task.Output = output
			task.Progress = 100
			now := time.Now().UTC()
			task.CompletedAt = &now
			logger.Info("Task completed via signal")
		})

		// Fail signal.
		selector.AddReceive(failCh, func(ch workflow.ReceiveChannel, more bool) {
			var data map[string]string
			ch.Receive(ctx, &data)
			task.State = TaskFailed
			task.Error = data["error"]
			now := time.Now().UTC()
			task.CompletedAt = &now
			logger.Info("Task failed via signal", "error", task.Error)
		})

		// Progress signal.
		selector.AddReceive(progressCh, func(ch workflow.ReceiveChannel, more bool) {
			var data struct{ Progress int }
			ch.Receive(ctx, &data)
			task.Progress = data.Progress
		})

		// Cancel signal.
		selector.AddReceive(cancelCh, func(ch workflow.ReceiveChannel, more bool) {
			var data any
			ch.Receive(ctx, &data)
			task.State = TaskCancelled
			now := time.Now().UTC()
			task.CompletedAt = &now
			logger.Info("Task cancelled via signal")
		})

		selector.Select(ctx)

		// Check if we've reached a terminal state.
		switch task.State {
		case TaskCompleted, TaskFailed, TaskCancelled:
			cancelTimer()
			return task, nil
		}
	}
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
