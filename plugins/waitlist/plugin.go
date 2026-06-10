// Copyright (C) 2020-2026, Hanzo AI Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package waitlist

import (
	"github.com/hanzoai/base/core"
	"github.com/hanzoai/base/tools/hook"
	"github.com/hanzoai/base/tools/router"
	luxlog "github.com/luxfi/log"
)

// MustRegister installs the waitlist plugin on the given app and panics
// on error. Suitable for use in a Base process's startup wiring.
func MustRegister(app core.App, cfg Config) {
	if err := Register(app, cfg); err != nil {
		panic(err)
	}
}

// Register installs the waitlist plugin.
//
// On OnBootstrap the plugin auto-creates two collections (`waitlists`,
// `waitlist_entries`) if they don't exist. On OnServe it mounts three
// REST endpoints under /v1/waitlist:
//
//	POST /v1/waitlist/join     - create an entry (Turnstile + rate-limit gated)
//	GET  /v1/waitlist/status   - look up rank, share URL, referral count
//	GET  /v1/waitlist/export   - admin-only CSV dump
//
// The plugin owns its collections and never exposes them as direct CRUD
// — the dashboard remains available for admins.
func Register(app core.App, cfg Config) error {
	if !cfg.Enabled {
		// default-enabled to avoid surprise: a zero-value Config disables.
		// Callers must set Enabled:true explicitly to opt in.
		return nil
	}

	cfg.resolve()
	if err := cfg.validate(); err != nil {
		return err
	}

	domains := cfg.DisposableDomains
	if domains == nil {
		domains = defaultDisposableDomains
	}

	p := &plugin{
		app:       app,
		config:    cfg,
		logger:    luxlog.New("component", "waitlist"),
		limiter:   newSlidingLimiter(cfg.JoinRateLimit, cfg.JoinRateWindow),
		turnstile: newTurnstileVerifier(cfg.TurnstileSecret),
		disposable: newDomainSet(domains),
	}

	app.OnBootstrap().Bind(&hook.Handler[*core.BootstrapEvent]{
		Id: "__waitlistBootstrap__",
		Func: func(e *core.BootstrapEvent) error {
			if err := e.Next(); err != nil {
				return err
			}
			return p.ensureSchema()
		},
	})

	app.OnServe().Bind(&hook.Handler[*core.ServeEvent]{
		Id: "__waitlistServe__",
		Func: func(e *core.ServeEvent) error {
			app.Store().Set("waitlist", p)
			p.registerRoutes(e.Router)
			return e.Next()
		},
	})

	return nil
}

type plugin struct {
	app        core.App
	config     Config
	logger     luxlog.Logger
	limiter    *slidingLimiter
	turnstile  *turnstileVerifier
	disposable map[string]struct{}
}

func (p *plugin) registerRoutes(r *router.Router[*core.RequestEvent]) {
	g := r.Group("/v1/waitlist")
	g.POST("/join", p.handleJoin)
	g.GET("/status", p.handleStatus)
	g.GET("/export", p.handleExport)
}
