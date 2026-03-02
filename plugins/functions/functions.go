// Package functions implements serverless function management for Hanzo Base
// via OpenFaaS. It provides per-tenant function deployment, invocation, and
// lifecycle management through the Base API surface.
//
// Example:
//
//	functions.MustRegister(app, functions.Config{
//		GatewayURL:        "http://openfaas-gateway.hanzo.svc:8080",
//		FunctionNamespace: "openfaas-fn",
//		RegistryURL:       "registry.hanzo.svc:5000",
//	})
package functions

import (
	"net/http"

	"github.com/hanzoai/base/core"
	"github.com/hanzoai/base/tools/router"
)

// Config holds the Functions plugin configuration.
type Config struct {
	// GatewayURL is the OpenFaaS gateway URL.
	GatewayURL string

	// FunctionNamespace is the K8s namespace for function deployments.
	FunctionNamespace string

	// RegistryURL is the container registry URL for function images.
	RegistryURL string
}

// MustRegister registers the Functions plugin and panics on failure.
func MustRegister(app core.App, config Config) {
	if err := Register(app, config); err != nil {
		panic(err)
	}
}

// Register registers the Functions plugin to the provided app instance.
func Register(app core.App, config Config) error {
	if config.GatewayURL == "" {
		config.GatewayURL = "http://openfaas-gateway.hanzo.svc:8080"
	}
	if config.FunctionNamespace == "" {
		config.FunctionNamespace = "openfaas-fn"
	}
	if config.RegistryURL == "" {
		config.RegistryURL = "registry.hanzo.svc:5000"
	}

	p := &plugin{
		app:    app,
		config: config,
		client: &http.Client{},
	}

	app.OnBootstrap().BindFunc(func(e *core.BootstrapEvent) error {
		if err := e.Next(); err != nil {
			return err
		}
		return p.ensureCollections()
	})

	app.OnServe().BindFunc(func(e *core.ServeEvent) error {
		p.registerRoutes(e.Router)
		return e.Next()
	})

	return nil
}

type plugin struct {
	app    core.App
	config Config
	client *http.Client
}

// --------------------------------------------------------------------------
// Routes
// --------------------------------------------------------------------------

func (p *plugin) registerRoutes(r *router.Router[*core.RequestEvent]) {
	api := r.Group("/api/functions")

	// Function CRUD
	api.POST("", p.handleDeployFunction)
	api.GET("", p.handleListFunctions)
	api.GET("/{name}", p.handleGetFunction)
	api.DELETE("/{name}", p.handleDeleteFunction)

	// Invocation
	api.POST("/{name}/invoke", p.handleInvokeFunction)

	// Logs
	api.GET("/{name}/logs", p.handleGetLogs)
}
