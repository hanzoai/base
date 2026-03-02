// Copyright (C) 2026, Hanzo AI Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package core

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"time"

	nats "github.com/hanzoai/pubsub-go"
)

// NATSPublisher publishes record lifecycle events to NATS JetStream.
// Opt-in: only active when NATS_URL env var is set.
type NATSPublisher struct {
	url  string
	mu   sync.Mutex
	conn *nats.Conn
}

// RecordEvent is the JSON payload published to NATS.
type RecordLifecycleEvent struct {
	Collection string `json:"collection"`
	Action     string `json:"action"`
	RecordID   string `json:"record_id"`
	Timestamp  int64  `json:"timestamp"`
}

// newNATSPublisher returns a publisher if NATS_URL is set, nil otherwise.
func newNATSPublisher() *NATSPublisher {
	url := os.Getenv("NATS_URL")
	if url == "" {
		return nil
	}
	return &NATSPublisher{url: url}
}

// connect lazily establishes the NATS connection on first publish.
func (p *NATSPublisher) connect() (*nats.Conn, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.conn != nil && p.conn.IsConnected() {
		return p.conn, nil
	}

	conn, err := nats.Connect(p.url,
		nats.Name("base"),
		nats.ReconnectWait(2*time.Second),
		nats.MaxReconnects(60),
	)
	if err != nil {
		return nil, fmt.Errorf("nats connect: %w", err)
	}
	p.conn = conn
	return conn, nil
}

// Publish sends a record lifecycle event to NATS.
// Subject format: base.{collection}.{action}
func (p *NATSPublisher) Publish(_ context.Context, collection, action, recordID string) error {
	conn, err := p.connect()
	if err != nil {
		return err
	}

	event := RecordLifecycleEvent{
		Collection: collection,
		Action:     action,
		RecordID:   recordID,
		Timestamp:  time.Now().UnixMilli(),
	}

	data, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("marshal event: %w", err)
	}

	subject := fmt.Sprintf("base.%s.%s", collection, action)
	return conn.Publish(subject, data)
}

// Close drains and closes the NATS connection.
func (p *NATSPublisher) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.conn != nil {
		p.conn.Close()
		p.conn = nil
	}
}

// registerNATSHooks wires the NATS publisher into model lifecycle hooks.
// Does nothing if NATS_URL is not set.
func registerNATSHooks(app *BaseApp) {
	pub := newNATSPublisher()
	if pub == nil {
		return
	}

	app.Logger().Info("NATS event publisher enabled", slog.String("url", pub.url))

	publishEvent := func(e *ModelEvent, action string) {
		table := e.Model.TableName()
		id := fmt.Sprintf("%v", e.Model.PK())
		if err := pub.Publish(e.Context, table, action, id); err != nil {
			app.Logger().Warn("NATS publish failed",
				slog.String("subject", fmt.Sprintf("base.%s.%s", table, action)),
				slog.String("error", err.Error()),
			)
		}
	}

	app.OnModelAfterCreateSuccess().BindFunc(func(e *ModelEvent) error {
		publishEvent(e, "create")
		return e.Next()
	})

	app.OnModelAfterUpdateSuccess().BindFunc(func(e *ModelEvent) error {
		publishEvent(e, "update")
		return e.Next()
	})

	app.OnModelAfterDeleteSuccess().BindFunc(func(e *ModelEvent) error {
		publishEvent(e, "delete")
		return e.Next()
	})

	app.OnTerminate().BindFunc(func(e *TerminateEvent) error {
		pub.Close()
		return e.Next()
	})
}
