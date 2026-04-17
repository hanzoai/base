// Package taskqueue provides a durable task client for Hanzo Tasks.
//
// Drop-in replacement for Base's Cron():
//
//	// Before (cron):
//	e.App.Cron().Add("settlement", "*/30 * * * * *", func() { ... })
//
//	// After (tasks):
//	tasks.Add("settlement", "30s", func() { ... })
//
// If TASKS_URL is set, schedules run as durable Temporal workflows
// (retries, dead letter, audit trail). If not, runs locally via goroutine
// timer (dev mode, same behavior as cron but no persistence).
//
// If zapAddr is set, ZAP binary transport is preferred over HTTP for
// submitting tasks (lower latency, same semantics). HTTP is fallback.
package tasks

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/luxfi/zap"
)

// ZAP opcodes for task submission.
const (
	OpcodeTaskSubmit   uint16 = 0x0050 // one-shot task
	OpcodeTaskSchedule uint16 = 0x0051 // recurring schedule
)

// ZAP object field offsets.
const (
	fieldTaskType = 0  // Text: task/workflow type
	fieldPayload  = 8  // Bytes: JSON payload
	fieldInterval = 16 // Text: duration string (schedules only)
	fieldName     = 0  // Text: schedule name (schedules)
	respStatus    = 0  // Uint32: status code
	respBody      = 4  // Bytes: response JSON
)

// Handler processes a one-shot task (webhook delivery, settlement, etc.)
type Handler func(taskType string, payload map[string]any)

// Client manages both one-shot tasks and recurring schedules.
type Client struct {
	tasksURL   string
	zapAddr    string
	httpClient *http.Client
	handler    Handler
	logger     *slog.Logger
	mu         sync.RWMutex
	schedules  map[string]context.CancelFunc // local schedule cancellers

	zapOnce sync.Once
	zapNode *zap.Node
	zapPeer string
	zapErr  error
}

// New creates a Client.
// If both tasksURL and zapAddr are empty, everything runs locally.
// If zapAddr is set, ZAP transport is preferred. HTTP is fallback.
func New(tasksURL, zapAddr string, handler Handler) *Client {
	return &Client{
		tasksURL: tasksURL,
		zapAddr:  zapAddr,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
		handler:   handler,
		logger:    slog.Default(),
		schedules: make(map[string]context.CancelFunc),
	}
}

// Add registers a recurring task. interval is a duration string ("30s", "5m", "1h").
// The fn runs every interval. If TASKS_URL is set, creates a durable Temporal schedule.
// Otherwise runs a local ticker (same as cron but cleaner).
//
//	tasks.Add("settlement.process", "30s", func() { processSettlements() })
//	tasks.Add("oracle.update", "5m", func() { updateOracle() })
func (c *Client) Add(name, interval string, fn func()) error {
	dur, err := time.ParseDuration(interval)
	if err != nil {
		return fmt.Errorf("taskqueue: invalid interval %q: %w", interval, err)
	}

	if c.zapAddr != "" || c.tasksURL != "" {
		// Try ZAP first, then HTTP, then local fallback.
		scheduled := false
		if c.zapAddr != "" {
			if err := c.scheduleZAP(name, dur); err == nil {
				scheduled = true
			} else {
				c.logger.Warn("taskqueue: ZAP schedule failed", "name", name, "error", err)
			}
		}
		if !scheduled && c.tasksURL != "" {
			if err := c.createSchedule(name, dur); err != nil {
				c.logger.Warn("taskqueue: HTTP schedule failed, falling back to local",
					"name", name, "error", err)
				c.addLocal(name, dur, fn)
			}
		} else if !scheduled {
			c.addLocal(name, dur, fn)
		}
		return nil
	}

	c.addLocal(name, dur, fn)
	return nil
}

// Enqueue submits a one-shot task for durable execution.
// Prefers ZAP transport when zapAddr is configured, falls back to HTTP.
func (c *Client) Now(taskType string, payload map[string]any) error {
	if c == nil {
		return nil
	}
	if c.tasksURL == "" && c.zapAddr == "" {
		return c.execDirect(taskType, payload)
	}
	if c.zapAddr != "" {
		if err := c.submitZAP(taskType, payload); err == nil {
			return nil
		}
		c.logger.Warn("taskqueue: ZAP submit failed, falling back to HTTP",
			"type", taskType, "zapAddr", c.zapAddr)
	}
	if c.tasksURL != "" {
		return c.submitHTTP(taskType, payload)
	}
	return c.execDirect(taskType, payload)
}

// Stop cancels all local schedules and closes ZAP connection. Call on shutdown.
func (c *Client) Stop() {
	c.mu.Lock()
	defer c.mu.Unlock()
	for name, cancel := range c.schedules {
		cancel()
		delete(c.schedules, name)
	}
	if c.zapNode != nil {
		c.zapNode.Stop()
	}
}

// connectZAP lazily establishes the ZAP connection to the Tasks server.
func (c *Client) connectZAP() error {
	c.zapOnce.Do(func() {
		c.zapNode = zap.NewNode(zap.NodeConfig{
			NodeID:      "tasks-sdk",
			ServiceType: "_tasks-sdk._tcp",
			Port:        0, // ephemeral
			Logger:      c.logger,
			NoDiscovery: true,
		})
		if err := c.zapNode.Start(); err != nil {
			c.zapErr = fmt.Errorf("taskqueue: zap start: %w", err)
			return
		}
		if err := c.zapNode.ConnectDirect(c.zapAddr); err != nil {
			c.zapErr = fmt.Errorf("taskqueue: zap connect %s: %w", c.zapAddr, err)
			return
		}
		peers := c.zapNode.Peers()
		if len(peers) > 0 {
			c.zapPeer = peers[0]
		} else {
			c.zapPeer = c.zapAddr
		}
		c.logger.Info("taskqueue: ZAP connected", "addr", c.zapAddr, "peer", c.zapPeer)
	})
	return c.zapErr
}

// submitZAP sends a one-shot task over ZAP (opcode 0x0050).
func (c *Client) submitZAP(taskType string, payload map[string]any) error {
	if err := c.connectZAP(); err != nil {
		return err
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("taskqueue: marshal: %w", err)
	}

	b := zap.NewBuilder(len(taskType) + len(payloadBytes) + 64)
	obj := b.StartObject(24)
	obj.SetText(fieldTaskType, taskType)
	obj.SetBytes(fieldPayload, payloadBytes)
	obj.FinishAsRoot()
	flags := uint16(OpcodeTaskSubmit) << 8
	data := b.FinishWithFlags(flags)

	msg, err := zap.Parse(data)
	if err != nil {
		return fmt.Errorf("taskqueue: build zap msg: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	resp, err := c.zapNode.Call(ctx, c.zapPeer, msg)
	if err != nil {
		return fmt.Errorf("taskqueue: zap call: %w", err)
	}

	status := resp.Root().Uint32(respStatus)
	if status != 0 && status != 200 {
		body := resp.Root().Bytes(respBody)
		return fmt.Errorf("taskqueue: zap status %d: %s", status, string(body))
	}
	return nil
}

// scheduleZAP creates a recurring schedule over ZAP (opcode 0x0051).
func (c *Client) scheduleZAP(name string, interval time.Duration) error {
	if err := c.connectZAP(); err != nil {
		return err
	}

	b := zap.NewBuilder(len(name) + 64)
	obj := b.StartObject(24)
	obj.SetText(fieldName, name)
	obj.SetText(fieldInterval, interval.String())
	obj.FinishAsRoot()
	flags := uint16(OpcodeTaskSchedule) << 8
	data := b.FinishWithFlags(flags)

	msg, err := zap.Parse(data)
	if err != nil {
		return fmt.Errorf("taskqueue: build zap msg: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	resp, err := c.zapNode.Call(ctx, c.zapPeer, msg)
	if err != nil {
		return fmt.Errorf("taskqueue: zap call: %w", err)
	}

	status := resp.Root().Uint32(respStatus)
	if status != 0 && status != 200 {
		body := resp.Root().Bytes(respBody)
		return fmt.Errorf("taskqueue: zap status %d: %s", status, string(body))
	}

	c.logger.Info("taskqueue: ZAP schedule created", "name", name, "interval", interval)
	return nil
}

// addLocal runs fn on a ticker (dev fallback, no persistence).
func (c *Client) addLocal(name string, interval time.Duration, fn func()) {
	ctx, cancel := context.WithCancel(context.Background())

	c.mu.Lock()
	if old, ok := c.schedules[name]; ok {
		old() // cancel previous
	}
	c.schedules[name] = cancel
	c.mu.Unlock()

	c.logger.Info("taskqueue: local schedule started", "name", name, "interval", interval)

	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				func() {
					defer func() {
						if r := recover(); r != nil {
							c.logger.Error("taskqueue: schedule panic", "name", name, "panic", r)
						}
					}()
					fn()
				}()
			}
		}
	}()
}

// createSchedule creates a durable schedule on Hanzo Tasks.
func (c *Client) createSchedule(name string, interval time.Duration) error {
	schedule := map[string]any{
		"schedule_id": name,
		"schedule": map[string]any{
			"spec": map[string]any{
				"interval": []map[string]any{
					{"every": interval.String()},
				},
			},
			"action": map[string]any{
				"start_workflow": map[string]any{
					"workflow_type": name,
					"task_queue":    "ats",
				},
			},
		},
	}

	body, err := json.Marshal(schedule)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}

	url := c.tasksURL + "/api/v1/namespaces/default/schedules/" + name
	req, err := http.NewRequest("POST", url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("http: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		c.logger.Info("taskqueue: durable schedule created", "name", name, "interval", interval)
		return nil
	}

	return fmt.Errorf("status %d", resp.StatusCode)
}

// execDirect runs the handler in a goroutine (dev mode).
func (c *Client) execDirect(taskType string, payload map[string]any) error {
	c.mu.RLock()
	h := c.handler
	c.mu.RUnlock()

	if h == nil {
		c.logger.Warn("taskqueue: no handler, dropping task", "type", taskType)
		return nil
	}

	go h(taskType, payload)
	return nil
}

// submitHTTP posts a one-shot task to the Hanzo Tasks HTTP API.
func (c *Client) submitHTTP(taskType string, payload map[string]any) error {
	envelope := map[string]any{
		"workflow_type": taskType,
		"task_queue":    "ats",
		"input":         payload,
	}

	body, err := json.Marshal(envelope)
	if err != nil {
		return fmt.Errorf("taskqueue: marshal: %w", err)
	}

	url := c.tasksURL + "/api/v1/namespaces/default/workflows"
	req, err := http.NewRequest("POST", url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("taskqueue: request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		c.logger.Warn("taskqueue: server unreachable, executing directly",
			"type", taskType, "error", err)
		return c.execDirect(taskType, payload)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}

	c.logger.Warn("taskqueue: server error, executing directly",
		"type", taskType, "status", resp.StatusCode)
	return c.execDirect(taskType, payload)
}
