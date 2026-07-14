// Copyright (C) 2020-2026, Hanzo AI Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package waitlist

import (
	"github.com/hanzoai/base/core"
	"github.com/hanzoai/base/tools/hook"
	"github.com/hanzoai/base/tools/router"
	luxlog "github.com/luxfi/log"
)

// MustRegister installs the waitlist plugin on the given app and panics on
// error. Suitable for a Base process's startup wiring.
func MustRegister(app core.App, cfg Config) {
	if err := Register(app, cfg); err != nil {
		panic(err)
	}
}

// Register installs the points-based waitlist plugin.
//
// On OnBootstrap it auto-creates/upgrades three collections (`waitlists`,
// `waitlist_entries`, `waitlist_events`) and seeds any configured default
// waitlists. On OnServe it mounts the REST surface under /v1/waitlist. The
// plugin owns its collections (no public CRUD); the dashboard remains
// available to superusers.
func Register(app core.App, cfg Config) error {
	if !cfg.Enabled {
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
		app:        app,
		config:     cfg,
		logger:     luxlog.New("component", "waitlist"),
		limiter:    newSlidingLimiter(cfg.JoinRateLimit, cfg.JoinRateWindow),
		turnstile:  newTurnstileVerifier(cfg.TurnstileSecret),
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
	// public
	g.POST("/join", p.handleJoin)
	g.GET("/status", p.handleStatus)
	g.GET("/neighborhood", p.handleNeighborhood)
	g.GET("/list", p.handleList)
	g.GET("/activity", p.handleActivity)
	g.POST("/track-share", p.handleTrackShare)
	g.POST("/invite", p.handleInvite)
	// service-authed (superuser / AdminSecret) — the caller-amount boost seam
	g.POST("/boost", p.handleBoost)
	// server-to-server (AwardSecret) — the verified-event award seam
	g.POST("/award", p.handleAward)
	// admin
	g.GET("/export", p.handleExport)
}
