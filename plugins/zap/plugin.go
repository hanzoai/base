package zap

import (
	"context"
	"log/slog"

	"github.com/hanzoai/base/core"
	"github.com/hanzoai/base/tools/hook"
	zaplib "github.com/luxfi/zap"
)

// MustRegister registers the ZAP transport plugin with a Base app.
// Call this before app.Start().
func MustRegister(app core.App) {
	MustRegisterWithConfig(app, DefaultConfig())
}

// MustRegisterWithConfig registers the ZAP transport plugin with custom config.
func MustRegisterWithConfig(app core.App, config Config) {
	if !config.Enabled {
		return
	}

	p := &plugin{
		app:    app,
		config: config,
	}

	app.OnServe().Bind(&hook.Handler[*core.ServeEvent]{
		Id: "__zapTransport__",
		Func: func(e *core.ServeEvent) error {
			if err := e.Next(); err != nil {
				return err
			}
			p.start()
			return nil
		},
	})

	app.OnTerminate().Bind(&hook.Handler[*core.TerminateEvent]{
		Id: "__zapTransportCleanup__",
		Func: func(e *core.TerminateEvent) error {
			p.stop()
			return e.Next()
		},
	})
}

type plugin struct {
	app     core.App
	config  Config
	node    *zaplib.Node
	logger  *slog.Logger
	handler *handler
}

func (p *plugin) start() {
	p.logger = slog.Default().With("component", "zap")
	p.logger.Info("starting ZAP transport", "port", p.config.Port, "nodeID", p.config.NodeID)

	p.node = zaplib.NewNode(zaplib.NodeConfig{
		NodeID:      p.config.NodeID,
		Port:        p.config.Port,
		ServiceType: p.config.ServiceType,
		Logger:      p.logger,
	})

	p.handler = newHandler(p.app, p.logger)

	// Register message handlers
	p.node.Handle(MsgTypeCollections, func(ctx context.Context, from string, msg *zaplib.Message) (*zaplib.Message, error) {
		return p.handler.handleCollections(ctx, from, msg)
	})

	p.node.Handle(MsgTypeRecords, func(ctx context.Context, from string, msg *zaplib.Message) (*zaplib.Message, error) {
		return p.handler.handleRecords(ctx, from, msg)
	})

	p.node.Handle(MsgTypeAuth, func(ctx context.Context, from string, msg *zaplib.Message) (*zaplib.Message, error) {
		return p.handler.handleAuth(ctx, from, msg)
	})

	p.node.Handle(MsgTypeRealtime, func(ctx context.Context, from string, msg *zaplib.Message) (*zaplib.Message, error) {
		return p.handler.handleRealtime(ctx, from, msg)
	})

	if err := p.node.Start(); err != nil {
		p.logger.Error("failed to start ZAP node", "error", err)
		return
	}

	// Give the handler a reference to the node for push notifications
	p.handler.setNode(p.node)

	// Hook into app record events for realtime broadcasting
	p.app.OnRecordAfterCreateSuccess().Bind(&hook.Handler[*core.RecordEvent]{
		Id: "__zapRealtimeCreate__",
		Func: func(e *core.RecordEvent) error {
			p.handler.broadcastEvent(e.Record.Collection().Name, "create", e.Record)
			return e.Next()
		},
	})

	p.app.OnRecordAfterUpdateSuccess().Bind(&hook.Handler[*core.RecordEvent]{
		Id: "__zapRealtimeUpdate__",
		Func: func(e *core.RecordEvent) error {
			p.handler.broadcastEvent(e.Record.Collection().Name, "update", e.Record)
			return e.Next()
		},
	})

	p.app.OnRecordAfterDeleteSuccess().Bind(&hook.Handler[*core.RecordEvent]{
		Id: "__zapRealtimeDelete__",
		Func: func(e *core.RecordEvent) error {
			p.handler.broadcastEvent(e.Record.Collection().Name, "delete", e.Record)
			return e.Next()
		},
	})

	p.logger.Info("ZAP transport listening", "port", p.config.Port, "discovery", p.config.ServiceType)
}

func (p *plugin) stop() {
	if p.node != nil {
		p.logger.Info("stopping ZAP transport")
		p.node.Stop()
	}
}
