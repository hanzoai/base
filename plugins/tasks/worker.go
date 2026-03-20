package tasks

import (
	"go.temporal.io/sdk/client"
	sdkworker "go.temporal.io/sdk/worker"
)

// Worker runs an embedded Temporal worker that polls a task queue
// and executes workflows and activities in-process.
type Worker struct {
	worker sdkworker.Worker
	queue  string
}

// NewWorker creates a worker for a specific task queue.
// For multi-tenant deployments, each org/space can have its own queue.
func NewWorker(c client.Client, queue string) *Worker {
	w := sdkworker.New(c, queue, sdkworker.Options{})

	// Register all workflow types.
	w.RegisterWorkflow(AgentTaskWorkflow)
	w.RegisterWorkflow(PipelineWorkflow)
	w.RegisterWorkflow(FanOutWorkflow)

	// Register activities.
	w.RegisterActivity(ExecuteTaskActivity)

	return &Worker{worker: w, queue: queue}
}

// Start begins polling the task queue.
func (tw *Worker) Start() error {
	return tw.worker.Start()
}

// Stop gracefully shuts down the worker.
func (tw *Worker) Stop() {
	tw.worker.Stop()
}
