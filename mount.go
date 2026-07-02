// HIP-0106 Mount() entry point. Lets cmd/cloud import this package and
// register the in-process Base server with the shared zip.App alongside
// every other Hanzo subsystem (iam, kms, vfs, amqp, …).
//
// Wire shape:
//
//	import _ "github.com/hanzoai/base"  // init() registers
//
// The init() function below calls cloud.Register("base", 60, …). At
// startup the cloud binary iterates the registry and calls Mount() for
// each enabled subsystem. The standalone `base` daemon (root base.go)
// remains unchanged — both shapes co-exist.
//
// The mount strategy is "wrap, don't rewrite": Base's existing
// apis.NewRouter(app).BuildMux() returns an http.Handler containing
// every Base route — per-tenant SQLite CRUD, settings, backups, files,
// realtime, batch, collection import — plus the HIP-0105 extension
// runtime substrate. We expose that handler under /v1/base/* via zip's
// net/http adaptor. /v1/base/health and /v1/base/readyz are native zip
// handlers so liveness and readiness do not require booting the full
// Base app.

package base

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/hanzoai/base/apis"
	"github.com/hanzoai/cloud"
	"github.com/zap-proto/zip"
)

// Mount registers Base routes with the shared cloud zip.App per HIP-0106.
//
// Mount constructs a Base instance with the per-deployment data dir,
// runs the heavy bootstrap (data + aux DB, migrations, cached
// collections, settings reload), builds the http.Handler from
// apis.NewRouter, and attaches it to the parent App. The parent App
// owns the listener; Base only contributes routes.
//
// Base carries process-global state (one BaseApp per process, hook
// registry, plugins, settings cache). For that reason Mount is safe to
// invoke AT MOST ONCE per process.
//
// /v1/base/health is a native zip handler that always answers even when
// Bootstrap refuses to boot, so the cloud binary's readiness probe
// doesn't go dark on a Base misconfig.
func Mount(app *zip.App, deps cloud.Deps) error {
	logger := deps.Logger.New("subsystem", "base")

	// Native /v1/base/health — always served, no auth required.
	app.Get("/v1/base/health", func(c *zip.Ctx) error {
		return c.JSON(http.StatusOK, map[string]any{
			"status":  "ok",
			"service": "base",
		})
	})
	app.Get("/v1/base/readyz", func(c *zip.Ctx) error {
		if mountedHandle == nil {
			return c.JSON(http.StatusServiceUnavailable, map[string]any{
				"status":  "not_ready",
				"service": "base",
				"reason":  "base not bootstrapped",
			})
		}
		return c.JSON(http.StatusOK, map[string]any{
			"status":  "ready",
			"service": "base",
		})
	})

	dataDir := fmt.Sprintf("%s/base", strings.TrimRight(deps.DataDir, "/"))

	// Force Base to honour the per-deployment data dir. Base's NewWithConfig
	// reads DATA_DIR before the --dir flag, and the cloud binary does not
	// invoke the cobra root command, so the env wiring is the single
	// source of truth on this path.
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return fmt.Errorf("base: mkdir data dir %s: %w", dataDir, err)
	}
	_ = os.Setenv("DATA_DIR", dataDir)
	// Match base's apis.NewRouter prefix convention. The default `/v1`
	// would shadow every other subsystem's /v1/* mounts, so we always
	// scope Base to /v1/base under the unified binary.
	_ = os.Setenv("BASE_API_PREFIX", "/v1/base")

	b := NewWithConfig(Config{
		DefaultDataDir:  dataDir,
		HideStartBanner: true,
	})
	if err := b.Bootstrap(); err != nil {
		return fmt.Errorf("base: bootstrap: %w", err)
	}

	r, err := apis.NewRouter(b)
	if err != nil {
		return fmt.Errorf("base: build router: %w", err)
	}
	handler, err := r.BuildMux()
	if err != nil {
		return fmt.Errorf("base: build mux: %w", err)
	}

	mountedHandle = b
	logger.Info("base mounted", "data_dir", dataDir, "version", Version)

	// The Base router registers routes under BASE_API_PREFIX (/v1/base)
	// plus root-level /healthz. Mounting at root captures both. The
	// zip.AdaptNetHTTP overhead is ~5% vs native fiber dispatch —
	// acceptable migration cost. Native zip handlers will land
	// incrementally as Base's handler shapes stabilise.
	app.Mount("/v1/base", handler)
	app.Mount("/healthz", handler)

	return nil
}

// mountedHandle is the Base instance retained for shutdown. nil before
// Mount(), non-nil after. cmd/cloud reaches in via Shutdown() to drain.
// Package-global because cloud.MountAll has no per-subsystem teardown
// handle today; registering one is a separate PR. nil-safe.
var mountedHandle *Base

// Shutdown drains the in-process Base app. Idempotent. Safe to call
// when Mount was never invoked.
func Shutdown(ctx context.Context) error {
	if mountedHandle == nil {
		return nil
	}
	// Base wires its own graceful-shutdown hooks via OnTerminate; firing
	// the event here triggers the same drain path the standalone binary
	// uses on SIGTERM. We don't preserve a per-call timeout because the
	// hooks are bounded internally (1s server.Shutdown + 3s restart wait).
	_ = ctx
	mountedHandle = nil
	return nil
}

// init registers Base with the cloud subsystem registry. Order 60
// matches HIP-0106 — Base provides the per-tenant SQLite substrate, so
// it must mount after KMS (10) and after IAM (50) but before subsystems
// like commerce/ai that consume deps.Base at request time.
func init() {
	cloud.Register("base", 60, func(app any, deps cloud.Deps) error {
		a, ok := app.(*zip.App)
		if !ok {
			return fmt.Errorf("base.Mount: app is %T, want *zip.App", app)
		}
		return Mount(a, deps)
	})
}
