// Package bootnode is the Go port of the Python bootnode backend
// (bootnode/api/), built natively on Hanzo Base as a plugin.
//
// It consolidates the blockchain developer platform — multi-network OAuth,
// project API keys, team management, and 1-click network/node/key
// provisioning — onto Base's IAM + tenant infrastructure. Identity is owned by
// Hanzo IAM (reused via github.com/hanzoai/base/iam); per-org and per-user data
// isolation come from the platform plugin's PrincipalIsolation="sqlite" mode;
// billing is wired through the commerce plugin's Client interface.
//
// Network/NodeFleet/KMSSecret provisioning targets bootno.de/v1 custom
// resources applied via a dependency-free Kubernetes REST client
// (plugins/bootnode/kube) — no client-go, no CGO.
//
// This is a foundation: 5 of 20 API modules are ported end-to-end (auth, team,
// networks, nodes, keys). The remaining modules are tracked in the PR body.
package bootnode

import (
	"net/http"
	"strings"

	"github.com/hanzoai/base/core"
	"github.com/hanzoai/base/iam"
	"github.com/hanzoai/base/plugins/bootnode/auth"
	"github.com/hanzoai/base/plugins/bootnode/kube"
	"github.com/hanzoai/base/plugins/bootnode/models"
	"github.com/hanzoai/base/plugins/commerce"
	"github.com/hanzoai/base/tools/hook"
	"github.com/hanzoai/base/tools/router"
	luxlog "github.com/luxfi/log"
)

// MustRegister installs the bootnode plugin and panics on error.
func MustRegister(app core.App, cfg Config) {
	if err := Register(app, cfg); err != nil {
		panic(err)
	}
}

// Register installs the bootnode plugin.
//
// On OnBootstrap it creates the bootnode collections (idempotent). On OnServe
// it mounts the API under /v1. A zero-value Config disables the plugin; callers
// opt in with Enabled:true.
func Register(app core.App, cfg Config) error {
	if !cfg.Enabled {
		return nil
	}
	cfg.resolve()

	// Fail fast: never run with the insecure default salt against a real IAM.
	if cfg.isProductionIAM() && cfg.APIKeySalt == insecureSaltDefault {
		return errInsecureSalt
	}

	p := &plugin{
		app:      app,
		config:   cfg,
		logger:   luxlog.New("component", "bootnode"),
		iam:      iam.NewClient(cfg.IAMEndpoint),
		kube:     kube.New(cfg.KubeNamespace),
		commerce: commerce.New(commerce.Config{BaseURL: cfg.CommerceURL, APIKey: cfg.CommerceAPIKey}),
	}

	app.OnBootstrap().Bind(&hook.Handler[*core.BootstrapEvent]{
		Id: "__bootnodeBootstrap__",
		Func: func(e *core.BootstrapEvent) error {
			if err := e.Next(); err != nil {
				return err
			}
			return models.EnsureAll(app)
		},
	})

	app.OnServe().Bind(&hook.Handler[*core.ServeEvent]{
		Id: "__bootnodeServe__",
		Func: func(e *core.ServeEvent) error {
			app.Store().Set("bootnode", p)
			p.registerRoutes(e.Router)
			return e.Next()
		},
	})

	return nil
}

// errInsecureSalt is returned when production-mode IAM is paired with the
// placeholder API key salt.
var errInsecureSalt = errString("bootnode: BOOTNODE_API_KEY_SALT must be set to a non-default value when IAM_URL is non-local")

type errString string

func (e errString) Error() string { return string(e) }

// plugin holds the shared dependencies for all bootnode handlers.
type plugin struct {
	app      core.App
	config   Config
	logger   luxlog.Logger
	iam      *iam.Client
	kube     *kube.Client
	commerce commerce.Client
}

// registerRoutes mounts every bootnode endpoint. One group, /v1, matching the
// Python api_prefix. No extraneous /api/ segment.
func (p *plugin) registerRoutes(r *router.Router[*core.RequestEvent]) {
	g := r.Group("/v1")

	// auth/
	g.POST("/auth/oauth/callback", p.handleOAuthCallback)
	g.GET("/auth/me", p.handleMe)
	g.POST("/auth/projects", p.handleCreateProject)
	g.GET("/auth/projects/{id}", p.handleGetProject)
	g.POST("/auth/keys", p.handleCreateAPIKey)
	g.GET("/auth/keys", p.handleListAPIKeys)
	g.DELETE("/auth/keys/{id}", p.handleDeleteAPIKey)

	// team/
	g.GET("/team", p.handleListTeam)
	g.POST("/team", p.handleInviteMember)
	g.PATCH("/team/{id}", p.handleUpdateMember)
	g.DELETE("/team/{id}", p.handleRemoveMember)

	// chains/
	g.GET("/chains", p.handleListChains)
	g.GET("/chains/{chain}", p.handleGetChain)

	// networks/  (bootno.de/v1 Network CRs)
	g.POST("/networks", p.handleCreateNetwork)
	g.GET("/networks", p.handleListNetworks)
	g.GET("/networks/{id}", p.handleGetNetwork)
	g.DELETE("/networks/{id}", p.handleDeleteNetwork)

	// nodes/  (bootno.de/v1 NodeFleet CRs)
	g.POST("/nodes", p.handleCreateNodeFleet)
	g.GET("/nodes", p.handleListNodeFleets)
	g.GET("/nodes/{id}", p.handleGetNodeFleet)
	g.DELETE("/nodes/{id}", p.handleDeleteNodeFleet)

	// keys/  (bootno.de/v1 KMSSecret CRs — no plaintext storage)
	g.POST("/keys", p.handleCreateKMSKey)
	g.GET("/keys/status", p.handleKeyStatus)
	g.GET("/keys/{name}", p.handleGetKMSKey)
}

// Identity is the resolved caller context for a request: who they are (IAM
// user id + org) and, when applicable, the bootnode project they act within.
type Identity struct {
	UserID string
	Email  string
	Org    string
	// ReadOnly is true for publishable (pk-) keys.
	ReadOnly bool
}

// requireUser resolves the caller's identity from the request. It accepts, in
// order: a Base-resolved auth record, an IAM bearer JWT, and IAM-managed
// pk-/sk-/hk- keys. bootnode bn_ project keys do NOT identify a user — they
// identify a project (see requireProject). Returns a 401 on failure.
func (p *plugin) requireUser(e *core.RequestEvent) (*Identity, error) {
	// Identity headers set by the platform plugin / gateway take precedence.
	if uid := e.Request.Header.Get("X-User-Id"); uid != "" {
		return &Identity{
			UserID: uid,
			Email:  e.Request.Header.Get("X-User-Email"),
			Org:    e.Request.Header.Get("X-Org-Id"),
		}, nil
	}

	// Base may have already authenticated the request.
	if e.Auth != nil {
		return &Identity{UserID: e.Auth.Id, Email: e.Auth.GetString("email")}, nil
	}

	cred, isBearer := bearerToken(e.Request)
	if cred == "" {
		return nil, e.UnauthorizedError("Authentication required. Use Authorization: Bearer <token> or X-API-Key.", nil)
	}

	switch auth.ClassifyCredential(cred, isBearer) {
	case auth.KeyJWT:
		u, err := p.iam.ValidateToken(cred)
		if err != nil {
			return nil, e.UnauthorizedError("invalid or expired token", err)
		}
		return p.identityFromIAM(e, u, false)
	case auth.KeyIAMPublishable:
		u, err := p.iam.ResolveAPIKey(cred)
		if err != nil {
			return nil, e.UnauthorizedError("invalid API key", err)
		}
		return p.identityFromIAM(e, u, true)
	case auth.KeyIAMSecret, auth.KeyIAMService:
		u, err := p.iam.ResolveAPIKey(cred)
		if err != nil {
			return nil, e.UnauthorizedError("invalid API key", err)
		}
		return p.identityFromIAM(e, u, false)
	default:
		return nil, e.UnauthorizedError("unsupported credential — bootnode keys identify a project, not a user", nil)
	}
}

// identityFromIAM builds an Identity from a resolved IAM user, enforcing the
// org allow-list.
func (p *plugin) identityFromIAM(e *core.RequestEvent, u *iam.User, readOnly bool) (*Identity, error) {
	org := ""
	if len(u.OrgIDs) > 0 {
		org = u.OrgIDs[0]
	}
	if org != "" && !p.config.orgAllowed(org) {
		return nil, e.ForbiddenError("organization '"+org+"' is not allowed", nil)
	}
	return &Identity{UserID: u.ID, Email: u.Email, Org: org, ReadOnly: readOnly}, nil
}

// bearerToken extracts a credential from the Authorization (Bearer) or
// X-API-Key header. The bool reports whether it came from a Bearer header.
func bearerToken(r *http.Request) (string, bool) {
	if k := r.Header.Get("X-API-Key"); k != "" {
		return k, false
	}
	authz := r.Header.Get("Authorization")
	if strings.HasPrefix(authz, "Bearer ") {
		return strings.TrimPrefix(authz, "Bearer "), true
	}
	return "", false
}

// orgLabels returns the standard CR labels scoping a resource to an org.
func orgLabels(org string) map[string]string {
	return map[string]string{
		"app.kubernetes.io/managed-by": "bootnode",
		"bootnode.dev/org":             org,
	}
}
