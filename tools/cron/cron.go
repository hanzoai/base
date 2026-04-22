// Package cron is a thin alias over Hanzo Tasks (tools/tasks) kept for
// backward compatibility. New code should call app.Tasks() directly.
//
// Scheduled jobs registered through this package are delegated to a
// *tasks.Client — durable when TASKS_URL is set, local goroutine tickers
// otherwise. The Schedule / NewSchedule helpers remain for cron-expression
// validation (used by settings validators).
package cron

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/hanzoai/base/tools/tasks"
)

// Cron is a crontab-like scheduler. Since the v2 refactor it is a shim
// that delegates to a *tasks.Client.
type Cron struct {
	mu       sync.RWMutex
	client   *tasks.Client
	timezone *time.Location
}

// New creates a Cron backed by a new local-only tasks.Client.
// Use NewFromTasks to share an existing client with the rest of the app.
func New() *Cron {
	return &Cron{
		client:   tasks.New("", "", nil),
		timezone: time.UTC,
	}
}

// NewFromTasks wraps an existing tasks.Client.
func NewFromTasks(c *tasks.Client) *Cron {
	return &Cron{
		client:   c,
		timezone: time.UTC,
	}
}

// SetInterval is retained for API compatibility. No-op: the tick cadence is
// dictated by each schedule's expression, not a global interval.
func (c *Cron) SetInterval(time.Duration) {}

// SetTimezone updates the timezone used for cron-expression evaluation.
// Kept for API compatibility; the underlying tasks.Client does not currently
// take a per-client timezone — server-side schedules run in UTC.
func (c *Cron) SetTimezone(l *time.Location) {
	if l == nil {
		return
	}
	c.mu.Lock()
	c.timezone = l
	c.mu.Unlock()
}

// MustAdd is Add that panics on error.
func (c *Cron) MustAdd(jobId string, cronExpr string, fn func()) {
	if err := c.Add(jobId, cronExpr, fn); err != nil {
		panic(err)
	}
}

// Add registers a recurring job. cronExpr is any expression accepted by
// tasks.Client.Add (standard 5-field cron, macros like @daily, or a Go
// duration string like "30s"). Re-adding with the same id replaces the
// previous registration.
func (c *Cron) Add(jobId string, cronExpr string, fn func()) error {
	if fn == nil {
		return errors.New("failed to add new cron job: fn must be non-nil function")
	}
	// Validate cron-expression syntax up front so callers get the same
	// error signal they got pre-refactor. Duration strings bypass this.
	if _, durErr := time.ParseDuration(cronExpr); durErr != nil {
		if _, err := NewSchedule(cronExpr); err != nil {
			return fmt.Errorf("failed to add new cron job: %w", err)
		}
	}
	return c.client.Add(jobId, cronExpr, fn)
}

// Remove deletes a registered job. No-op if the id is unknown.
func (c *Cron) Remove(jobId string) { c.client.Remove(jobId) }

// RemoveAll deletes every registered job.
func (c *Cron) RemoveAll() { c.client.RemoveAll() }

// Total returns the number of registered jobs.
func (c *Cron) Total() int { return c.client.Total() }

// HasJob reports whether a job with the given id is registered.
func (c *Cron) HasJob(jobId string) bool { return c.client.HasJob(jobId) }

// Jobs returns a snapshot of all registered jobs ordered by id.
func (c *Cron) Jobs() []*Job {
	schedules := c.client.Schedules()
	out := make([]*Job, 0, len(schedules))
	for _, s := range schedules {
		out = append(out, &Job{
			id:         s.Name,
			expression: s.Expression,
			client:     c.client,
		})
	}
	return out
}

// Start resumes all paused tickers. Schedules are auto-started at Add()
// time, so this is only meaningful after a prior Stop().
func (c *Cron) Start() { c.client.ResumeAll() }

// Stop pauses every local ticker without removing jobs. Registered
// schedules remain discoverable via Jobs() / HasJob() and can be resumed
// with Start(). To tear down the scheduler fully, call RemoveAll() or
// tasks.Client.Stop() on the underlying client.
func (c *Cron) Stop() { c.client.PauseAll() }

// HasStarted reports whether at least one job is present and actively
// ticking.
func (c *Cron) HasStarted() bool { return c.client.ActiveTickerCount() > 0 }
