// Copyright (C) 2020-2026, Hanzo AI Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package kms

import (
	"log/slog"

	"github.com/hanzoai/base/core"
	"github.com/hanzoai/base/tools/hook"
	sdk "github.com/hanzoai/kms-sdk-go"
)

// MustRegister registers the KMS plugin with a Base app and panics on error.
//
// Usage:
//
//	kms.MustRegister(app, kms.Config{
//	    Nodes:     []string{"https://kms-mpc-0:9651", "https://kms-mpc-1:9651", "https://kms-mpc-2:9651"},
//	    OrgSlug:   "my-org",
//	    Threshold: 2,
//	    EncryptedCollections: map[string][]string{
//	        "credentials": {"api_key", "api_secret"},
//	    },
//	    FHESearchable: map[string][]string{
//	        "credentials": {"name"},
//	    },
//	})
func MustRegister(app core.App, config Config) {
	if err := Register(app, config); err != nil {
		panic(err)
	}
}

// Register registers the KMS plugin with a Base app. The plugin provides:
//   - Zero-knowledge encrypted secret management via REST API
//   - Transparent field-level encryption on configured collections
//   - FHE-encrypted indexes for querying encrypted fields without decryption
func Register(app core.App, config Config) error {
	if !config.Enabled {
		return nil
	}

	if err := config.validate(); err != nil {
		return err
	}

	client, err := sdk.NewClient(sdk.Config{
		Nodes:     config.Nodes,
		OrgSlug:   config.OrgSlug,
		Threshold: config.Threshold,
	})
	if err != nil {
		return err
	}

	p := &plugin{
		app:    app,
		config: config,
		client: client,
		logger: slog.Default().With("component", "kms"),
	}

	// Build lookup maps for fast hook checks.
	p.encFieldSet = make(map[string]map[string]struct{}, len(config.EncryptedCollections))
	for col, fields := range config.EncryptedCollections {
		m := make(map[string]struct{}, len(fields))
		for _, f := range fields {
			m[f] = struct{}{}
		}
		p.encFieldSet[col] = m
	}

	p.fheFieldSet = make(map[string]map[string]struct{}, len(config.FHESearchable))
	for col, fields := range config.FHESearchable {
		m := make(map[string]struct{}, len(fields))
		for _, f := range fields {
			m[f] = struct{}{}
		}
		p.fheFieldSet[col] = m
	}

	// Bootstrap: auto-unlock if configured.
	app.OnBootstrap().Bind(&hook.Handler[*core.BootstrapEvent]{
		Id: "__kmsBootstrap__",
		Func: func(e *core.BootstrapEvent) error {
			if err := e.Next(); err != nil {
				return err
			}

			if config.AutoUnlock {
				passphrase := config.resolvePassphrase()
				if passphrase != "" {
					if unlockErr := client.Unlock(passphrase); unlockErr != nil {
						p.logger.Warn("kms: auto-unlock failed", "error", unlockErr)
					} else {
						p.logger.Info("kms: auto-unlock succeeded")
					}
				}
			}

			return nil
		},
	})

	// Serve: register routes and record hooks.
	app.OnServe().Bind(&hook.Handler[*core.ServeEvent]{
		Id: "__kmsServe__",
		Func: func(e *core.ServeEvent) error {
			// Store reference for programmatic access.
			app.Store().Set("kms", p)

			p.registerRoutes(e.Router)
			p.registerHooks()

			return e.Next()
		},
	})

	// Terminate: lock CEK on shutdown.
	app.OnTerminate().Bind(&hook.Handler[*core.TerminateEvent]{
		Id: "__kmsTerminate__",
		Func: func(e *core.TerminateEvent) error {
			p.logger.Info("kms: locking CEK on shutdown")
			client.Lock()
			return e.Next()
		},
	})

	return nil
}

type plugin struct {
	app    core.App
	config Config
	client *sdk.Client
	logger *slog.Logger

	// Pre-computed field sets for O(1) lookup in hooks.
	encFieldSet map[string]map[string]struct{} // collection -> field -> struct{}
	fheFieldSet map[string]map[string]struct{} // collection -> field -> struct{}
}
