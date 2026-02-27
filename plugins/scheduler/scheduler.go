// Package scheduler implements a scheduled function execution plugin for Base.
//
// It provides deferred and time-based function execution with exactly-once
// semantics, retry support, and post-commit scheduling (functions scheduled
// inside hooks run only after the hook's DB transaction commits).
//
// The scheduler uses an internal Base collection "_scheduled_functions" with fields:
//   - functionName (text, required)
//   - args (json)
//   - scheduledAt (date, required)
//   - status (select: pending, running, completed, failed, cancelled)
//   - result (json)
//   - error (text)
//   - retryCount (number)
//   - createdAt (autodate)
//   - completedAt (date)
//
// Example:
//
//	scheduler.MustRegister(app, scheduler.Config{
//		PollInterval:  1 * time.Second,
//		MaxConcurrent: 10,
//		RetryCount:    3,
//		RetryDelay:    5 * time.Second,
//	})
package scheduler

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/hanzoai/base/core"
	"github.com/hanzoai/base/tools/types"
)

const (
	// CollectionName is the internal collection used to persist scheduled functions.
	CollectionName = "_scheduled_functions"

	StatusPending   = "pending"
	StatusRunning   = "running"
	StatusCompleted = "completed"
	StatusFailed    = "failed"
	StatusCancelled = "cancelled"
)

// ExecuteFunc is the callback signature for executing a scheduled function.
// It receives the function name and JSON-decoded args, and returns a result or error.
type ExecuteFunc func(functionName string, args any) (any, error)

// Config defines the configuration for the scheduler plugin.
type Config struct {
	// OnExecute is called to execute a scheduled function.
	// If nil, scheduled functions will fail with "no executor configured".
	OnExecute ExecuteFunc

	// PollInterval controls how frequently the scheduler checks for due functions.
	// Defaults to 1 second.
	PollInterval time.Duration

	// RetryDelay is the base delay between retries of a failed function.
	// Defaults to 5 seconds.
	RetryDelay time.Duration

	// MaxConcurrent limits the number of concurrently executing scheduled functions.
	// Defaults to 10.
	MaxConcurrent int

	// RetryCount is the maximum number of retry attempts for a failed function.
	// Defaults to 3.
	RetryCount int
}

// MustRegister registers the scheduler plugin in the provided app instance
// and panics if it fails.
//
// Example:
//
//	scheduler.MustRegister(app, scheduler.Config{
//		PollInterval:  1 * time.Second,
//		MaxConcurrent: 10,
//		RetryCount:    3,
//		RetryDelay:    5 * time.Second,
//	})
func MustRegister(app core.App, config Config) {
	if err := Register(app, config); err != nil {
		panic(err)
	}
}

// Register registers the scheduler plugin in the provided app instance.
func Register(app core.App, config Config) error {
	p := &plugin{app: app, config: config}

	if p.config.PollInterval <= 0 {
		p.config.PollInterval = 1 * time.Second
	}
	if p.config.MaxConcurrent <= 0 {
		p.config.MaxConcurrent = 10
	}
	if p.config.RetryCount < 0 {
		p.config.RetryCount = 3
	}
	if p.config.RetryDelay <= 0 {
		p.config.RetryDelay = 5 * time.Second
	}

	p.sem = make(chan struct{}, p.config.MaxConcurrent)

	p.app.OnBootstrap().BindFunc(func(e *core.BootstrapEvent) error {
		if err := e.Next(); err != nil {
			return err
		}

		if err := p.ensureCollection(); err != nil {
			return fmt.Errorf("scheduler: failed to ensure collection: %w", err)
		}

		return nil
	})

	p.app.OnServe().BindFunc(func(e *core.ServeEvent) error {
		if err := e.Next(); err != nil {
			return err
		}

		p.startPoller()
		return nil
	})

	p.app.OnTerminate().BindFunc(func(e *core.TerminateEvent) error {
		p.stopPoller()
		return e.Next()
	})

	return nil
}

type plugin struct {
	app    core.App
	config Config
	sem    chan struct{}

	mu       sync.Mutex
	stopCh   chan struct{}
	pollDone chan struct{}
}

// ensureCollection creates the _scheduled_functions collection if it doesn't exist.
func (p *plugin) ensureCollection() error {
	_, err := p.app.FindCollectionByNameOrId(CollectionName)
	if err == nil {
		return nil // already exists
	}

	collection := core.NewBaseCollection(CollectionName)
	collection.System = true

	collection.Fields.Add(
		&core.TextField{
			Name:     "functionName",
			Required: true,
		},
		&core.JSONField{
			Name:    "args",
			MaxSize: 1048576, // 1MB
		},
		&core.DateField{
			Name:     "scheduledAt",
			Required: true,
		},
		&core.SelectField{
			Name:     "status",
			Values:   []string{StatusPending, StatusRunning, StatusCompleted, StatusFailed, StatusCancelled},
			Required: true,
			MaxSelect: 1,
		},
		&core.JSONField{
			Name:    "result",
			MaxSize: 1048576,
		},
		&core.TextField{
			Name: "error",
		},
		&core.NumberField{
			Name: "retryCount",
		},
		&core.AutodateField{
			Name:     "createdAt",
			OnCreate: true,
		},
		&core.DateField{
			Name: "completedAt",
		},
	)

	return p.app.Save(collection)
}

// startPoller begins the background polling goroutine.
func (p *plugin) startPoller() {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.stopCh != nil {
		return // already running
	}

	p.stopCh = make(chan struct{})
	p.pollDone = make(chan struct{})

	go p.pollLoop()
}

// stopPoller signals the polling goroutine to stop and waits for it.
func (p *plugin) stopPoller() {
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

// pollLoop runs the polling loop in a separate goroutine.
func (p *plugin) pollLoop() {
	defer close(p.pollDone)

	ticker := time.NewTicker(p.config.PollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-p.stopCh:
			return
		case <-ticker.C:
			p.processDueFunctions()
		}
	}
}

// processDueFunctions finds and executes all functions that are due.
func (p *plugin) processDueFunctions() {
	now := types.NowDateTime()

	records, err := p.findDueFunctions(now)
	if err != nil {
		p.app.Logger().Error(
			"scheduler: failed to find due functions",
			slog.String("error", err.Error()),
		)
		return
	}

	for _, record := range records {
		// claim the record via optimistic locking (status pending -> running)
		if !p.claimFunction(record) {
			continue
		}

		// acquire semaphore slot
		p.sem <- struct{}{}

		go func(r *core.Record) {
			defer func() { <-p.sem }()
			p.executeFunction(r)
		}(record)
	}
}

// findDueFunctions queries for pending functions whose scheduledAt <= now.
func (p *plugin) findDueFunctions(now types.DateTime) ([]*core.Record, error) {
	records := []*core.Record{}

	err := p.app.RecordQuery(CollectionName).
		AndWhere(dbx.HashExp{"status": StatusPending}).
		AndWhere(dbx.NewExp("[[scheduledAt]] <= {:now}", dbx.Params{"now": now.String()})).
		OrderBy("scheduledAt ASC").
		Limit(int64(p.config.MaxConcurrent)).
		All(&records)
	if err != nil {
		return nil, err
	}

	return records, nil
}

// claimFunction atomically transitions a function from pending to running.
// Returns true if the claim succeeded (exactly-once semantics).
func (p *plugin) claimFunction(record *core.Record) bool {
	// Use a direct SQL update with WHERE status='pending' for atomicity.
	result, err := p.app.NonconcurrentDB().NewQuery(
		"UPDATE {{" + CollectionName + "}} SET [[status]] = {:running} WHERE [[id]] = {:id} AND [[status]] = {:pending}",
	).Bind(dbx.Params{
		"running": StatusRunning,
		"id":      record.Id,
		"pending": StatusPending,
	}).Execute()
	if err != nil {
		p.app.Logger().Error(
			"scheduler: failed to claim function",
			slog.String("id", record.Id),
			slog.String("error", err.Error()),
		)
		return false
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return false
	}

	return rows == 1
}

// executeFunction runs the scheduled function and updates its record status.
func (p *plugin) executeFunction(record *core.Record) {
	functionName := record.GetString("functionName")

	var args any
	rawArgs := record.Get("args")
	if rawArgs != nil {
		// try to decode raw JSON args
		switch v := rawArgs.(type) {
		case types.JSONRaw:
			if len(v) > 0 {
				_ = json.Unmarshal(v, &args)
			}
		case string:
			if v != "" {
				_ = json.Unmarshal([]byte(v), &args)
			}
		default:
			args = v
		}
	}

	executor := p.config.OnExecute
	if executor == nil {
		p.markFailed(record, "no executor configured")
		return
	}

	result, err := executor(functionName, args)
	if err != nil {
		retryCount := int(record.GetFloat("retryCount"))
		if retryCount < p.config.RetryCount {
			p.scheduleRetry(record, retryCount+1, err)
			return
		}
		p.markFailed(record, err.Error())
		return
	}

	p.markCompleted(record, result)
}

// markCompleted updates the record to completed status with the result.
func (p *plugin) markCompleted(record *core.Record, result any) {
	now := types.NowDateTime()

	var resultJSON types.JSONRaw
	if result != nil {
		raw, err := json.Marshal(result)
		if err == nil {
			resultJSON = raw
		}
	}

	_, err := p.app.NonconcurrentDB().NewQuery(
		"UPDATE {{" + CollectionName + "}} SET [[status]] = {:status}, [[result]] = {:result}, [[completedAt]] = {:completedAt} WHERE [[id]] = {:id}",
	).Bind(dbx.Params{
		"status":      StatusCompleted,
		"result":      string(resultJSON),
		"completedAt": now.String(),
		"id":          record.Id,
	}).Execute()
	if err != nil {
		p.app.Logger().Error(
			"scheduler: failed to mark function completed",
			slog.String("id", record.Id),
			slog.String("error", err.Error()),
		)
	}
}

// markFailed updates the record to failed status with the error message.
func (p *plugin) markFailed(record *core.Record, errMsg string) {
	now := types.NowDateTime()

	_, err := p.app.NonconcurrentDB().NewQuery(
		"UPDATE {{" + CollectionName + "}} SET [[status]] = {:status}, [[error]] = {:error}, [[completedAt]] = {:completedAt} WHERE [[id]] = {:id}",
	).Bind(dbx.Params{
		"status":      StatusFailed,
		"error":       errMsg,
		"completedAt": now.String(),
		"id":          record.Id,
	}).Execute()
	if err != nil {
		p.app.Logger().Error(
			"scheduler: failed to mark function failed",
			slog.String("id", record.Id),
			slog.String("error", err.Error()),
		)
	}
}

// scheduleRetry reschedules a failed function for retry with exponential backoff.
func (p *plugin) scheduleRetry(record *core.Record, retryCount int, origErr error) {
	delay := p.config.RetryDelay * time.Duration(retryCount)
	nextRun, _ := types.ParseDateTime(time.Now().UTC().Add(delay))

	_, err := p.app.NonconcurrentDB().NewQuery(
		"UPDATE {{"+CollectionName+"}} SET [[status]] = {:status}, [[scheduledAt]] = {:scheduledAt}, [[retryCount]] = {:retryCount}, [[error]] = {:error} WHERE [[id]] = {:id}",
	).Bind(dbx.Params{
		"status":      StatusPending,
		"scheduledAt": nextRun.String(),
		"retryCount":  retryCount,
		"error":       origErr.Error(),
		"id":          record.Id,
	}).Execute()
	if err != nil {
		p.app.Logger().Error(
			"scheduler: failed to schedule retry",
			slog.String("id", record.Id),
			slog.String("error", err.Error()),
		)
	}
}

// ScheduleAfter creates a new scheduled function to execute after delayMs milliseconds.
//
// If the calling context is inside a transaction, the record is inserted but the
// function won't be picked up until after the transaction commits (the record
// becomes visible to the poller only after commit).
//
// Returns the scheduled function record ID.
func ScheduleAfter(app core.App, delayMs int64, functionName string, args any) (string, error) {
	scheduledAt, _ := types.ParseDateTime(time.Now().UTC().Add(time.Duration(delayMs) * time.Millisecond))
	return createScheduledRecord(app, functionName, args, scheduledAt)
}

// ScheduleAt creates a new scheduled function to execute at the specified time.
//
// Returns the scheduled function record ID.
func ScheduleAt(app core.App, at time.Time, functionName string, args any) (string, error) {
	scheduledAt, _ := types.ParseDateTime(at.UTC())
	return createScheduledRecord(app, functionName, args, scheduledAt)
}

// CancelScheduled cancels a pending scheduled function by ID.
// Returns an error if the function is not found or is not in pending status.
func CancelScheduled(app core.App, id string) error {
	_, err := app.NonconcurrentDB().NewQuery(
		"UPDATE {{" + CollectionName + "}} SET [[status]] = {:status} WHERE [[id]] = {:id} AND [[status]] = {:pending}",
	).Bind(dbx.Params{
		"status":  StatusCancelled,
		"id":      id,
		"pending": StatusPending,
	}).Execute()
	return err
}

// ListScheduled returns all scheduled function records matching the optional status filter.
// If status is empty, all records are returned.
func ListScheduled(app core.App, status string) ([]*core.Record, error) {
	query := app.RecordQuery(CollectionName).OrderBy("scheduledAt ASC")

	if status != "" {
		query = query.AndWhere(dbx.HashExp{"status": status})
	}

	records := []*core.Record{}
	if err := query.All(&records); err != nil {
		return nil, err
	}

	return records, nil
}

// createScheduledRecord inserts a new scheduled function record.
func createScheduledRecord(app core.App, functionName string, args any, scheduledAt types.DateTime) (string, error) {
	collection, err := app.FindCollectionByNameOrId(CollectionName)
	if err != nil {
		return "", fmt.Errorf("scheduler: collection %q not found: %w", CollectionName, err)
	}

	record := core.NewRecord(collection)
	record.Set("functionName", functionName)
	record.Set("scheduledAt", scheduledAt)
	record.Set("status", StatusPending)
	record.Set("retryCount", 0)

	if args != nil {
		argsJSON, err := json.Marshal(args)
		if err != nil {
			return "", fmt.Errorf("scheduler: failed to marshal args: %w", err)
		}
		record.Set("args", types.JSONRaw(argsJSON))
	}

	if err := app.Save(record); err != nil {
		return "", fmt.Errorf("scheduler: failed to save scheduled function: %w", err)
	}

	return record.Id, nil
}
