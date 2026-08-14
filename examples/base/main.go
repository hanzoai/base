package main

import (
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/hanzoai/base"
	"github.com/hanzoai/base/apis"
	"github.com/hanzoai/base/core"
	"github.com/hanzoai/base/plugins/bootnode"
	"github.com/hanzoai/base/plugins/calendar"
	"github.com/hanzoai/base/plugins/cloudsql"
	"github.com/hanzoai/base/plugins/ghupdate"
	"github.com/hanzoai/base/plugins/jsvm"
	"github.com/hanzoai/base/plugins/migratecmd"
	"github.com/hanzoai/base/plugins/org"
	"github.com/hanzoai/base/plugins/waitlist"
	"github.com/hanzoai/base/plugins/zap"
	"github.com/hanzoai/base/tools/hook"
	"github.com/hanzoai/base/tools/osutils"
)

func main() {
	app := base.New()

	// ---------------------------------------------------------------
	// Optional plugin flags:
	// ---------------------------------------------------------------

	var hooksDir string
	app.RootCmd.PersistentFlags().StringVar(
		&hooksDir,
		"hooksDir",
		"",
		"the directory with the JS app hooks",
	)

	var hooksWatch bool
	app.RootCmd.PersistentFlags().BoolVar(
		&hooksWatch,
		"hooksWatch",
		true,
		"auto restart the app on hooks file change; it has no effect on Windows",
	)

	var hooksPool int
	app.RootCmd.PersistentFlags().IntVar(
		&hooksPool,
		"hooksPool",
		15,
		"the total prewarm goja.Runtime instances for the JS app hooks execution",
	)

	var migrationsDir string
	app.RootCmd.PersistentFlags().StringVar(
		&migrationsDir,
		"migrationsDir",
		"",
		"the directory with the user defined migrations",
	)

	var automigrate bool
	app.RootCmd.PersistentFlags().BoolVar(
		&automigrate,
		"automigrate",
		true,
		"enable/disable auto migrations",
	)

	var publicDir string
	app.RootCmd.PersistentFlags().StringVar(
		&publicDir,
		"publicDir",
		defaultPublicDir(),
		"the directory to serve static files",
	)

	var indexFallback bool
	app.RootCmd.PersistentFlags().BoolVar(
		&indexFallback,
		"indexFallback",
		true,
		"fallback the request to index.html on missing static path, e.g. when pretty urls are used with SPA",
	)

	app.RootCmd.ParseFlags(os.Args[1:])

	// ---------------------------------------------------------------
	// Plugins and hooks:
	// ---------------------------------------------------------------

	// load jsvm (hooks and migrations)
	jsvm.MustRegister(app, jsvm.Config{
		MigrationsDir: migrationsDir,
		HooksDir:      hooksDir,
		HooksWatch:    hooksWatch,
		HooksPoolSize: hooksPool,
	})

	// migrate command (with js templates)
	migratecmd.MustRegister(app, app.RootCmd, migratecmd.Config{
		TemplateLang: migratecmd.TemplateLangJS,
		Automigrate:  automigrate,
		Dir:          migrationsDir,
	})

	// ZAP binary protocol transport (port 9999)
	zap.MustRegister(app)

	// Waitlist — POST /v1/waitlist/join etc. (the "Join waitlist" CTA on
	// coming-soon products; console2 proxies to it via WAITLIST_URL). Turnstile
	// (TURNSTILE_SECRET_KEY) + admin export (WAITLIST_ADMIN_SECRET) resolve from
	// env; join is IP rate-limited. Without this the route 404s and the UI shows
	// "the waitlist is not open on this deployment yet".
	waitlist.MustRegister(app, waitlist.Config{Enabled: true})

	// GitHub selfupdate
	ghupdate.MustRegister(app, app.RootCmd, ghupdate.Config{})

	// Per-org platform (IAM + KMS integration). Registering it is what gives
	// every org a Base of its own under {DataDir}/orgs/{org} — there is no
	// second setting to turn that on, because a deployment with orgs and one
	// shared database is not a shape anyone wants.
	//
	// It registers when an IAM endpoint is configured, and that endpoint is the
	// SAME value the plugin already requires rather than a switch beside it. An
	// org is resolved from identity, so with no identity provider there is
	// nothing for a Base to be scoped to: this binary then serves ONE Base and
	// needs no upstream at all, which is what `make dev` runs.
	//
	// Accept the canonical *_ENDPOINT name or the *_URL alias so a deployment
	// can set either without config drift (one way to configure Base's
	// upstreams, two accepted spellings). Mirrors the resolution in
	// plugins/org/kms_helpers.go.
	if iamEndpoint := envAny("IAM_ENDPOINT", "IAM_URL"); iamEndpoint != "" {
		org.MustRegister(app, org.Config{
			IAMEndpoint:            iamEndpoint,
			IAMAddress:             envAny("IAM_ADDRESS"),
			KMSEndpoint:            envAny("KMS_ENDPOINT", "KMS_URL"),
			IAMClientID:            os.Getenv("IAM_CLIENT_ID"),
			IAMClientSecret:        os.Getenv("IAM_CLIENT_SECRET"),
			PrincipalEncryptionKey: os.Getenv("PRINCIPAL_ENCRYPTION_KEY"),
		})
	}

	// Bootnode blockchain developer platform (multi-network OAuth, project keys,
	// teams, network/node/key provisioning via bootno.de CRDs). Opt-in via
	// BOOTNODE_ENABLED=true. Reuses the platform's IAM + per-org isolation.
	bootnode.MustRegister(app, bootnode.ConfigFromEnv())

	// Calendar — the native booking API that speaks Cal.com's API-v2 shapes over the
	// scheduling collections, IAM-owned, so cal.hanzo.ai's <Booker> talks to Base.
	// Mounts under /v1/calendar. Opt-in via CALENDAR_ENABLED=true.
	calendar.MustRegister(app, calendar.ConfigFromEnv())

	// Hanzo Cloud SQL — serverless PostgreSQL (per-base database provisioning)
	cloudsql.MustRegister(app, cloudsql.Config{
		MetaURL:       os.Getenv("CLOUD_SQL_META_URL"),
		ComputeHost:   os.Getenv("CLOUD_SQL_COMPUTE_HOST"),
		DefaultPGUser: os.Getenv("CLOUD_SQL_PG_USER"),
		DefaultPGPass: os.Getenv("CLOUD_SQL_PG_PASS"),
	})

	// static route to serves files from the provided public dir
	// (if publicDir exists and the route path is not already defined)
	app.OnServe().Bind(&hook.Handler[*core.ServeEvent]{
		Func: func(e *core.ServeEvent) error {
			if !e.Router.HasRoute(http.MethodGet, "/{path...}") {
				e.Router.GET("/{path...}", apis.Static(os.DirFS(publicDir), indexFallback))
			}

			return e.Next()
		},
		Priority: 999, // execute as latest as possible to allow users to provide their own route
	})

	if err := app.Start(); err != nil {
		log.Fatal(err)
	}
}

// the default public dir location is relative to the executable
func defaultPublicDir() string {
	if osutils.IsProbablyGoRun() {
		return "./public"
	}

	return filepath.Join(os.Args[0], "../public")
}

// envAny returns the first non-empty, trimmed value among the given
// environment variable names. It lets a deployment set either the canonical
// name or a legacy alias (e.g. IAM_ENDPOINT or IAM_URL) without per-deploy
// config drift.
func envAny(keys ...string) string {
	for _, k := range keys {
		if v := strings.TrimSpace(os.Getenv(k)); v != "" {
			return v
		}
	}
	return ""
}
