package tasks

import (
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/hanzoai/base/core"
)

const (
	// StoreKey is the app.Store() key where the task store is registered.
	StoreKey = "tasks.store"
)

// ExecuteFunc is called by the scheduler to execute a claimed task.
// Return output map on success, or error on failure.
type ExecuteFunc func(task *Task) (map[string]any, error)

// Config defines the tasks plugin configuration.
type Config struct {
	// OnExecute is called to run a task. If nil, tasks must be completed
	// externally via the API (work-stealing pattern).
	OnExecute ExecuteFunc

	// PollInterval controls scheduler tick frequency. Default 2s.
	PollInterval time.Duration

	// MaxConcurrent limits concurrent auto-executions. Default 10.
	MaxConcurrent int
}

// MustRegister registers the tasks plugin and panics on failure.
//
// Example:
//
//	tasks.MustRegister(app, tasks.Config{
//		PollInterval:  2 * time.Second,
//		MaxConcurrent: 10,
//	})
func MustRegister(app core.App, config Config) {
	if err := Register(app, config); err != nil {
		panic(err)
	}
}

// Register registers the tasks plugin in the provided app instance.
func Register(app core.App, config Config) error {
	p := &plugin{app: app, config: config}

	if p.config.PollInterval <= 0 {
		p.config.PollInterval = 2 * time.Second
	}
	if p.config.MaxConcurrent <= 0 {
		p.config.MaxConcurrent = 10
	}

	p.sem = make(chan struct{}, p.config.MaxConcurrent)

	app.OnBootstrap().BindFunc(func(e *core.BootstrapEvent) error {
		if err := e.Next(); err != nil {
			return err
		}

		if err := p.ensureTasksCollection(); err != nil {
			return fmt.Errorf("tasks: failed to create _tasks collection: %w", err)
		}
		if err := p.ensureWorkflowsCollection(); err != nil {
			return fmt.Errorf("tasks: failed to create _workflows collection: %w", err)
		}

		p.store = NewStore(app)
		app.Store().Set(StoreKey, p.store)

		return nil
	})

	app.OnServe().BindFunc(func(e *core.ServeEvent) error {
		if err := e.Next(); err != nil {
			return err
		}

		p.startScheduler()
		return nil
	})

	app.OnTerminate().BindFunc(func(e *core.TerminateEvent) error {
		p.stopScheduler()
		return e.Next()
	})

	return nil
}

// GetStore retrieves the registered task store from the app, or nil.
func GetStore(app core.App) *Store {
	raw := app.Store().Get(StoreKey)
	if raw == nil {
		return nil
	}
	store, _ := raw.(*Store)
	return store
}

type plugin struct {
	app    core.App
	config Config
	store  *Store
	sem    chan struct{}

	mu       sync.Mutex
	stopCh   chan struct{}
	pollDone chan struct{}
}

// --- Collection creation ---

func (p *plugin) ensureTasksCollection() error {
	_, err := p.app.FindCollectionByNameOrId(TasksCollection)
	if err == nil {
		return nil
	}

	col := core.NewBaseCollection(TasksCollection)
	col.System = true

	col.Fields.Add(
		&core.TextField{Name: "spaceId", Required: true},
		&core.TextField{Name: "title", Required: true},
		&core.TextField{Name: "description"},
		&core.SelectField{
			Name:      "state",
			Required:  true,
			MaxSelect: 1,
			Values: []string{
				string(TaskPending), string(TaskClaimed), string(TaskRunning),
				string(TaskCompleted), string(TaskFailed), string(TaskCancelled),
				string(TaskRetrying),
			},
		},
		&core.NumberField{Name: "priority"},
		&core.TextField{Name: "assignedTo"},
		&core.TextField{Name: "createdBy"},
		&core.TextField{Name: "workflowId"},
		&core.TextField{Name: "parentTaskId"},
		&core.JSONField{Name: "dependsOn", MaxSize: 65536},
		&core.JSONField{Name: "labels", MaxSize: 65536},
		&core.JSONField{Name: "input", MaxSize: 1048576},
		&core.JSONField{Name: "output", MaxSize: 1048576},
		&core.TextField{Name: "error"},
		&core.NumberField{Name: "progress"},
		&core.NumberField{Name: "maxRetries"},
		&core.NumberField{Name: "retryCount"},
		&core.NumberField{Name: "timeout"}, // seconds
		&core.JSONField{Name: "metadata", MaxSize: 65536},
		&core.DateField{Name: "startedAt"},
		&core.DateField{Name: "completedAt"},
		&core.AutodateField{Name: "created", OnCreate: true},
		&core.AutodateField{Name: "updated", OnCreate: true, OnUpdate: true},
	)

	return p.app.Save(col)
}

func (p *plugin) ensureWorkflowsCollection() error {
	_, err := p.app.FindCollectionByNameOrId(WorkflowsCollection)
	if err == nil {
		return nil
	}

	col := core.NewBaseCollection(WorkflowsCollection)
	col.System = true

	col.Fields.Add(
		&core.TextField{Name: "spaceId", Required: true},
		&core.TextField{Name: "name", Required: true},
		&core.TextField{Name: "description"},
		&core.SelectField{
			Name:      "state",
			Required:  true,
			MaxSelect: 1,
			Values: []string{
				string(TaskPending), string(TaskRunning),
				string(TaskCompleted), string(TaskFailed), string(TaskCancelled),
			},
		},
		&core.JSONField{Name: "tasks", MaxSize: 65536},
		&core.TextField{Name: "createdBy"},
		&core.JSONField{Name: "metadata", MaxSize: 65536},
		&core.DateField{Name: "completedAt"},
		&core.AutodateField{Name: "created", OnCreate: true},
		&core.AutodateField{Name: "updated", OnCreate: true, OnUpdate: true},
	)

	return p.app.Save(col)
}

// --- Background scheduler ---

func (p *plugin) startScheduler() {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.stopCh != nil {
		return
	}

	p.stopCh = make(chan struct{})
	p.pollDone = make(chan struct{})

	go p.pollLoop()
}

func (p *plugin) stopScheduler() {
	p.mu.Lock()
	stopCh := p.stopCh
	pollDone := p.pollDone
	p.stopCh = nil
	p.pollDone = nil
	p.mu.Unlock()

	if stopCh != nil {
		close(stopCh)
		<-pollDone
	}
}

func (p *plugin) pollLoop() {
	defer close(p.pollDone)

	ticker := time.NewTicker(p.config.PollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-p.stopCh:
			return
		case <-ticker.C:
			p.tick()
		}
	}
}

func (p *plugin) tick() {
	if p.store == nil {
		return
	}

	// 1. Check timeouts.
	if err := p.store.CheckTimeouts(); err != nil {
		p.app.Logger().Error("tasks: timeout check failed", slog.String("error", err.Error()))
	}

	// 2. Advance workflows.
	if err := p.store.AdvanceWorkflows(); err != nil {
		p.app.Logger().Error("tasks: workflow advance failed", slog.String("error", err.Error()))
	}

	// 3. Auto-execute pending tasks if OnExecute is configured.
	if p.config.OnExecute != nil {
		p.autoExecute()
	}
}

// autoExecute finds pending tasks and runs them via the configured executor.
func (p *plugin) autoExecute() {
	pending := TaskPending
	tasks, err := p.store.ListTasks(TaskFilters{
		State: &pending,
		Limit: p.config.MaxConcurrent,
	})
	if err != nil {
		return
	}

	for _, task := range tasks {
		if !p.store.dependenciesMet(task) {
			continue
		}

		// Claim it.
		if err := p.store.ClaimTask(task.ID, "_scheduler"); err != nil {
			continue
		}

		// Start it.
		if err := p.store.StartTask(task.ID); err != nil {
			continue
		}

		// Acquire semaphore and execute.
		p.sem <- struct{}{}
		go func(t *Task) {
			defer func() { <-p.sem }()
			p.executeTask(t)
		}(task)
	}
}

func (p *plugin) executeTask(task *Task) {
	output, err := p.config.OnExecute(task)
	if err != nil {
		if failErr := p.store.FailTask(task.ID, err.Error()); failErr != nil {
			p.app.Logger().Error("tasks: failed to record task failure",
				slog.String("task_id", task.ID),
				slog.String("error", failErr.Error()),
			)
		}
		return
	}

	if err := p.store.CompleteTask(task.ID, output); err != nil {
		p.app.Logger().Error("tasks: failed to complete task",
			slog.String("task_id", task.ID),
			slog.String("error", err.Error()),
		)
	}
}
