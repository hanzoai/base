package main

import (
	"log"
	"net/http"
	"os"
	"path/filepath"

	"github.com/hanzoai/base"
	"github.com/hanzoai/base/apis"
	"github.com/hanzoai/base/core"
	"github.com/hanzoai/base/plugins/ghupdate"
	"github.com/hanzoai/base/plugins/jsvm"
	"github.com/hanzoai/base/plugins/migratecmd"
	"github.com/hanzoai/base/plugins/cloudsql"
	"github.com/hanzoai/base/plugins/functions"
	"github.com/hanzoai/base/plugins/platform"
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

	// GitHub selfupdate
	ghupdate.MustRegister(app, app.RootCmd, ghupdate.Config{})

	// Multi-tenant platform (IAM + KMS integration)
	platform.MustRegister(app, platform.PlatformConfig{
		IAMEndpoint:     os.Getenv("IAM_ENDPOINT"),
		KMSEndpoint:     os.Getenv("KMS_ENDPOINT"),
		IAMClientID:     os.Getenv("IAM_CLIENT_ID"),
		IAMClientSecret: os.Getenv("IAM_CLIENT_SECRET"),
	})

	// Hanzo Cloud SQL — serverless PostgreSQL (per-tenant database provisioning)
	cloudsql.MustRegister(app, cloudsql.Config{
		MetaURL:       os.Getenv("CLOUD_SQL_META_URL"),
		ComputeHost:   os.Getenv("CLOUD_SQL_COMPUTE_HOST"),
		DefaultPGUser: os.Getenv("CLOUD_SQL_PG_USER"),
		DefaultPGPass: os.Getenv("CLOUD_SQL_PG_PASS"),
	})

	// OpenFaaS serverless functions
	functions.MustRegister(app, functions.Config{
		GatewayURL:        os.Getenv("OPENFAAS_GATEWAY_URL"),
		FunctionNamespace: os.Getenv("OPENFAAS_FN_NAMESPACE"),
		RegistryURL:       os.Getenv("OPENFAAS_REGISTRY_URL"),
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
