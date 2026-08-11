// Package org gives one Base process a Base per org.
//
// An org's Base is a Base of its own under {DataDir}/orgs/{org}/, opened the
// first time a request arrives carrying that org. Isolation is physical: a
// different org is a different file, so there is no query that can read across
// two. Physical is all it is — OrgDEK derives a key from KMS that nothing opens
// a file with, so the files are plaintext at rest.
//
// Orgs and members are IAM's, read off the validated token. This package never
// writes them — a local copy is a second answer to "who is in this org", and
// the one a request arrives on wins.
//
// It publishes /v1/bases: which Bases the caller can reach, and what state each
// is in. There is no create verb; using an org opens its Base.
//
//	org.MustRegister(app, org.Config{
//		IAMEndpoint:     "https://hanzo.id",
//		KMSEndpoint:     "zap.kms.svc.cluster.local:9999",
//		IAMClientID:     "my-client-id",
//		IAMClientSecret: "my-client-secret",
//	})
package org

import (
	"fmt"
	"net/http"
	"os"
	"slices"
	"strings"

	"github.com/hanzoai/base/apis"
	"github.com/hanzoai/base/core"
	"github.com/hanzoai/base/tools/hook"
	"github.com/hanzoai/base/tools/router"
)

const (
	// System collection names.
	collectionOrgs       = "_orgs"
	collectionOrgMembers = "_org_members"

	// Header for org context scoping.

	// Member roles.
	RoleOwner  = "owner"
	RoleAdmin  = "admin"
	RoleMember = "member"
	RoleViewer = "viewer"
)

// Config defines the configuration for the platform plugin.
type Config struct {
	// IAMEndpoint is the base URL for Hanzo IAM (default: "https://hanzo.id").
	IAMEndpoint string

	// KMSEndpoint is the KMS ZAP address — "host:port", "zap://host:port" or
	// "zap+mdns://_kms._tcp" (default: "zap.kms.svc.cluster.local:9999").
	// An http(s) URL is rejected at Register: Base speaks native ZAP to KMS.
	KMSEndpoint string

	// IAMClientID is the OAuth2 client ID for IAM authentication.
	IAMClientID string

	// IAMClientSecret is the OAuth2 client secret for IAM authentication.
	IAMClientSecret string

	// IAMOrg is the IAM organization identifier (optional, used by auth proxy).
	IAMOrg string

	// IAMApp is the IAM application identifier (optional, used by auth proxy).
	IAMApp string

	// ComplianceEndpoint is the base URL for Lux Compliance service (optional).
	// If set, enables KYC/AML screening and payment compliance for orgs.
	ComplianceEndpoint string

	// ComplianceAPIKey is the API key for the compliance service.
	ComplianceAPIKey string

	// PrincipalEncryptionKey is the master key per-principal keys are derived
	// from, by github.com/hanzoai/cek — see OrgDB.OrgDEK and OrgDB.UserDEK for
	// the namespace each one uses. It must be 32 bytes, which is what a master
	// key from KMS is. If empty, encryption is disabled (dev mode).
	PrincipalEncryptionKey string

	// Deprecated: use PrincipalEncryptionKey.
	OrgEncryptionKey string

	// OrgStorageEndpoint is the S3-compatible storage endpoint for per-org
	// object storage (e.g., "s3.hanzo.space" or "s3.hanzo.ai").
	// Each org and user gets isolated prefixes with SSE-C encryption.
	// If empty, no per-org S3 storage is provisioned.
	OrgStorageEndpoint string

	// OrgStorageBucket is the root S3 bucket name (default: "orgs").
	OrgStorageBucket string

	// DefaultTemplates defines collection schemas cloned per org on creation.
	// If nil, no default org collections are created.
	DefaultTemplates []CollectionTemplate
}

// MustRegister registers the platform plugin to the provided app instance
// and panics if it fails.
func MustRegister(app core.App, config Config) {
	if err := Register(app, config); err != nil {
		panic(err)
	}
}

// Register registers the platform plugin to the provided app instance.
//
// Hanzo Base is a pure IAM client — it never hosts identity. IAM must be
// reachable at boot via IAM_ENDPOINT (a hanzo.id base, or an in-process
// iam.Embed() served by the fused daemon). Base validates IAM JWTs against
// that endpoint's JWKS; there is no local password / OTP / MFA surface.
func Register(app core.App, config Config) error {
	if config.IAMEndpoint == "" || config.IAMEndpoint == "disabled" || config.IAMEndpoint == "none" {
		return fmt.Errorf(
			"platform: IAM_ENDPOINT is required — Hanzo Base is a pure IAM " +
				"client and does not host identity. Set IAM_ENDPOINT to a " +
				"Hanzo IAM instance (e.g. https://hanzo.id) or an in-process " +
				"iam.Embed() served by the fused daemon")
	}
	if config.KMSEndpoint == "" {
		config.KMSEndpoint = defaultKMSEndpoint
	}

	kmsClient, err := NewKMSClient(config.KMSEndpoint)
	if err != nil {
		return err
	}

	p := &plugin{
		app:        app,
		config:     config,
		iam:        NewIAMClient(config.IAMEndpoint),
		compliance: NewComplianceClient(config.ComplianceEndpoint, config.ComplianceAPIKey),
		org:        &OrgService{app: app, kms: kmsClient, config: config},
		orgDB:      NewOrgDB(app, config.OrgEncryptionKey),
		jwksURL:    strings.TrimRight(config.IAMEndpoint, "/") + "/v1/iam/.well-known/jwks",
	}
	p.bases = newBases(p)

	// Bootstrap: ensure platform system collections exist.
	app.OnBootstrap().BindFunc(func(e *core.BootstrapEvent) error {
		if err := e.Next(); err != nil {
			return err
		}
		return p.ensureSystemCollections()
	})

	// Terminate: release the long-lived KMS connection and the Bases this
	// process opened.
	app.OnTerminate().BindFunc(func(e *core.TerminateEvent) error {
		kmsClient.Close()
		p.bases.close()
		return e.Next()
	})

	// Which Base serves an org. Reading it is how a request reaches the right
	// one; without it every read lands on the process's own Base, which is what
	// used to happen.
	app.Store().Set(apis.StoreKeyBases, apis.Bases(p.bases.base))

	p.declare(app)

	app.Logger().Info("platform: IAM is the only auth source",
		"jwksURL", p.jwksURL,
		"authEndpoint", config.IAMEndpoint,
	)

	// Serve: register API routes, identity header middleware, and org-scoping.
	app.OnServe().BindFunc(func(e *core.ServeEvent) error {
		// Expose OrgService in app store for Goja JS access.
		app.Store().Set("org", p.org)

		// API key middleware: resolve hk-/pk-/sk- keys via IAM.
		// Runs after loadAuthToken — if JWKS didn't set re.Auth (because
		// the token isn't a JWT), try resolving it as an IAM API key.
		// This gives every Base app native support for IAM-managed keys.
		e.Router.Bind(&hook.Handler[*core.RequestEvent]{
			Id:       "platformAPIKeyAuth",
			Priority: apis.DefaultLoadAuthTokenMiddlewarePriority + 1,
			Func: func(re *core.RequestEvent) error {
				// Skip if already authenticated via JWKS/JWT
				if re.Auth != nil {
					return re.Next()
				}

				// Extract Bearer token
				auth := re.Request.Header.Get("Authorization")
				token := ""
				if len(auth) > 7 && auth[:7] == "Bearer " {
					token = auth[7:]
				}
				// Also check query param (for /v1/config?key=pk-xxx)
				if token == "" {
					token = re.Request.URL.Query().Get("key")
				}

				if token == "" || !IsAPIKey(token) {
					return re.Next()
				}

				// Resolve key via IAM
				user, err := p.iam.ResolveAPIKey(token)
				if err != nil {
					app.Logger().Debug("platform: API key resolution failed",
						"error", err, "prefix", token[:6]+"...")
					return re.Next() // fail-open to next middleware
				}

				// A key acts in the org IAM issued it under. Where the request
				// also names one, the key must already carry it — the same rule
				// a token gets, stated separately because a key is a machine and
				// holds none of the cross-tenant scope a platform operator does.
				org := ""
				if len(user.OrgIDs) > 0 {
					org = user.OrgIDs[0]
				}
				if stated := apis.StatedOrg(re); stated != "" {
					if !slices.Contains(user.OrgIDs, stated) {
						return re.ForbiddenError("The key does not carry the requested organization.", nil)
					}
					org = stated
				}
				if org == "" {
					return re.ForbiddenError("The key carries no organization.", nil)
				}

				base, err := p.bases.base(org)
				if err != nil {
					return re.InternalServerError("Failed to open the Base for this organization.", err)
				}
				re.App = base

				// Set identity context from resolved key
				re.Set("authSub", user.ID)
				re.Set("authName", user.Name)
				re.Set("authEmail", user.Email)
				re.Set("authOwner", org)
				re.Set(apis.RequestEventKeyOrg, org)
				re.Request.Header.Set("X-User-Id", user.ID)
				re.Request.Header.Set("X-Org-Id", org)
				re.Request.Header.Set("X-User-Email", user.Email)

				// Store key type for permission checks downstream
				if IsPublishableKey(token) {
					re.Set("authKeyType", "publishable") // read-only
				} else {
					re.Set("authKeyType", "secret") // full access
				}

				return re.Next()
			},
		})

		// Publishable key enforcement: pk- keys are read-only.
		// Blocks non-GET methods for publishable keys. Secret keys and
		// JWTs have no method restrictions.
		//
		// Priority +3: runs after identity headers are set, before route handlers.
		//
		// Scope rules:
		//   pk- → GET only (read market data, config, public info)
		//   sk- → all methods (create orders, manage accounts, admin)
		//   hk- → all methods (IAM service key, legacy compat)
		//   JWT → all methods (user session via IAM OIDC)
		e.Router.Bind(&hook.Handler[*core.RequestEvent]{
			Id:       "platformKeyTypeEnforcement",
			Priority: apis.DefaultLoadAuthTokenMiddlewarePriority + 3,
			Func: func(re *core.RequestEvent) error {
				keyType, _ := re.Get("authKeyType").(string)
				if keyType != "publishable" {
					return re.Next() // JWT, sk-, hk-, or unauthenticated — no restriction
				}

				method := re.Request.Method
				if method == http.MethodGet || method == http.MethodHead || method == http.MethodOptions {
					return re.Next() // reads allowed
				}

				// Exception: a publishable key MAY create in a PUBLIC-FORM
				// collection (createRule == "" ⇒ anonymous create allowed) — the
				// public submit path (contact forms, signups, waitlists). Every
				// other write stays blocked. Org is derived from the key by the
				// stampOrgOwnership create hook, never from the request.
				if method == http.MethodPost {
					if name := recordsCreateCollectionName(re.Request.URL.Path); name != "" {
						if c, err := re.App.FindCachedCollectionByNameOrId(name); err == nil &&
							c.CreateRule != nil && *c.CreateRule == "" {
							return re.Next()
						}
					}
				}

				// pk- key trying to write — block it
				return re.JSON(http.StatusForbidden, map[string]any{
					"error":   "publishable keys are read-only",
					"message": "Use a secret key (sk-) or JWT for write operations.",
					"code":    "key_read_only",
				})
			},
		})

		p.registerRoutes(e.Router)
		p.registerOrgRoutes(e.Router)

		// /v1/iam mount: transparent reverse proxy to IAM_ENDPOINT so the
		// admin UI and SDKs see IAM at a stable local path.
		p.registerIAMProxy(e.Router)

		// /v1/idv mount: thin reverse proxy to the standalone hanzoai/idv
		// service via IDV_ENDPOINT. Registered regardless of IAM mode —
		// IDV is its own service domain, not a child of IAM. With
		// IDV_ENDPOINT unset, /v1/idv/status returns {enabled:false}
		// and the rest return 503.
		p.registerIDVProxy(e.Router)

		return e.Next()
	})

	return nil
}

type plugin struct {
	app        core.App
	config     Config
	iam        *IAMClient
	compliance *ComplianceClient
	org        *OrgService
	orgDB      *OrgDB
	bases      *bases
	jwksURL    string
}

// declare is what this plugin states on every Base it opens, the process's own
// included.
//
// A tenant's Base has to answer the way the platform's does — same single auth
// source, same ownership stamped from the validated principal, same OrgService
// bound for extensions — because it is the same product serving a different
// org. Stating it in one function is what keeps the two from drifting; the
// alternative is a handler that reads a store key set on only one Base and
// silently takes the zero value on the others.
func (p *plugin) declare(app core.App) {
	// IAM is the only auth source. Every Base route validates JWTs against
	// IAM's JWKS; the legacy local-password / OTP / MFA paths are unreachable
	// (see apis/record_auth_*: 410 Gone with a Location pointer to the IAM
	// equivalent). Base never hosts identity — it only validates.
	app.Store().Set(apis.StoreKeyExternalAuthOnly, true)
	app.Store().Set(apis.StoreKeyJWKSURL, p.jwksURL)

	// OrgService, reachable from Goja extensions.
	app.Store().Set("org", p.org)

	// Stamp owner+org on every base-collection create from the VALIDATED
	// principal — never from client body/headers. A caller cannot attribute a
	// record to another user or org.
	app.OnRecordCreateRequest().BindFunc(stampOrgOwnership)
}

// --------------------------------------------------------------------------
// Bootstrap: system collections
// --------------------------------------------------------------------------

func (p *plugin) ensureSystemCollections() error {
	if err := p.ensureOrgsCollection(); err != nil {
		return fmt.Errorf("platform: ensure _orgs: %w", err)
	}
	if err := p.ensureMembersCollection(); err != nil {
		return fmt.Errorf("platform: ensure _org_members: %w", err)
	}
	if err := p.ensureOrgConfigsCollection(); err != nil {
		return fmt.Errorf("platform: ensure %s: %w", collectionOrgConfigs, err)
	}
	if err := p.ensureOrgCustomersCollection(); err != nil {
		return fmt.Errorf("platform: ensure %s: %w", collectionOrgCustomers, err)
	}
	return nil
}

func (p *plugin) ensureOrgsCollection() error {
	_, err := p.app.FindCollectionByNameOrId(collectionOrgs)
	if err == nil {
		return nil // already exists
	}

	c := core.NewBaseCollection(collectionOrgs)
	c.System = true
	c.Fields.Add(
		&core.TextField{Name: "name", Required: true, Min: 1, Max: 100},
		&core.TextField{Name: "slug", Required: true, Min: 1, Max: 50},
		&core.TextField{Name: "ownerId", Required: true},
		&core.TextField{Name: "iamOrgId"},
		&core.TextField{Name: "kmsProjectId"},
		&core.AutodateField{Name: "created", OnCreate: true},
		&core.AutodateField{Name: "updated", OnCreate: true, OnUpdate: true},
	)

	p.app.Logger().Info("creating platform system collection", "name", collectionOrgs)
	return p.app.Save(c)
}

func (p *plugin) ensureMembersCollection() error {
	_, err := p.app.FindCollectionByNameOrId(collectionOrgMembers)
	if err == nil {
		return nil
	}

	c := core.NewBaseCollection(collectionOrgMembers)
	c.System = true
	c.Fields.Add(
		&core.TextField{Name: "orgId", Required: true},
		&core.TextField{Name: "userId", Required: true},
		&core.SelectField{
			Name:      "role",
			Required:  true,
			MaxSelect: 1,
			Values:    []string{RoleOwner, RoleAdmin, RoleMember, RoleViewer},
		},
		&core.AutodateField{Name: "created", OnCreate: true},
		&core.AutodateField{Name: "updated", OnCreate: true, OnUpdate: true},
	)

	p.app.Logger().Info("creating platform system collection", "name", collectionOrgMembers)
	return p.app.Save(c)
}

// --------------------------------------------------------------------------
// Route registration
// --------------------------------------------------------------------------

func (p *plugin) registerRoutes(r *router.Router[*core.RequestEvent]) {
	// A Base is what an org gets: its own collections, its own files, its own
	// key. So the resource a client addresses is /v1/bases, and never /v1/orgs —
	// the org is IAM's noun and Base does not get a second one. It is not
	// /v1/platform either: that is the PaaS at platform.hanzo.ai, a different
	// product, and sharing its name here is what made "where are my Bases?"
	// unanswerable.
	//
	// ORGS AND MEMBERSHIPS ARE IAM'S. Base READS them and never writes them:
	// minting an org here made a second registry with its own slug rules and its
	// own uniqueness check, so two systems both believed they owned the noun and
	// the one a token was issued against was not necessarily the one Base knew.
	//
	// IAM already answers both questions — /v1/iam/organizations owns the org, and
	// a validated token carries the caller's memberships (IAMUser.OrgIDs), so the
	// read needs no call at all. Create, delete and invite live at IAM.
	// Declared whole rather than on a group, so the collection root is
	// /v1/bases and not /v1/bases/ — an empty leaf composes to the trailing
	// slash, which is an address nobody calls.
	r.GET(basesPath, p.handleListBases)
	r.GET(basesPath+"/{id}", p.handleGetBase)

	// Compliance is its own service domain, a sibling of /v1/iam — not a child
	// of Base's own resource. Optional: registered only where it is configured.
	if p.compliance != nil && p.compliance.Enabled() {
		c := r.Group("/v1/compliance")
		c.POST("/application", p.handleCreateComplianceApp)
		c.POST("/kyc/{applicationId}", p.handleInitiateKYC)
		c.GET("/kyc/{applicationId}", p.handleGetKYCStatus)
		c.POST("/screen", p.handleScreenIndividual)
		c.POST("/payment/validate", p.handleValidatePayment)
	}
}

// --------------------------------------------------------------------------
// Route handlers
// --------------------------------------------------------------------------

// baseView is what a Base IS to a caller: the org it belongs to, whether one
// has been opened yet, and how big it has grown.
//
// There is no verb to create one. A Base is opened the first time a request
// arrives carrying its org, so "exists" reports what happened rather than what
// somebody remembered to provision — which is why this reports a fact from the
// filesystem and not a row from a table.
type baseView struct {
	Org    string `json:"org"`
	Exists bool   `json:"exists"`
	Bytes  int64  `json:"bytes,omitempty"`
}

// base reports one org's Base. An org the caller belongs to that has never been
// used answers exists:false, which is the honest answer and not an error.
func (p *plugin) base(org string) baseView {
	v := baseView{Org: org}
	path, ok := p.orgDB.GetOrgDBPath(org)
	if !ok {
		return v
	}
	v.Exists = true
	if st, err := os.Stat(path); err == nil {
		v.Bytes = st.Size()
	}
	return v
}

// handleListBases answers which Bases the caller can reach.
//
// The orgs come from the validated token — IAM puts the membership on it, so
// this needs no local table and no call back to IAM. The local membership rows
// this used to read were a copy that could disagree with the token a request
// actually arrived on.
func (p *plugin) handleListBases(e *core.RequestEvent) error {
	user, err := p.requireAuth(e)
	if err != nil {
		return err
	}

	out := make([]baseView, 0, len(user.OrgIDs))
	for _, org := range user.OrgIDs {
		if org == "" {
			continue
		}
		out = append(out, p.base(org))
	}

	return e.JSON(http.StatusOK, out)
}

// handleGetBase answers for one Base.
//
// It used to read a record out of the local _orgs collection, which IAM owns
// and Base never writes — so it answered 404 for every org, including the
// caller's own. Membership is the token's to state.
func (p *plugin) handleGetBase(e *core.RequestEvent) error {
	user, err := p.requireAuth(e)
	if err != nil {
		return err
	}

	org := e.Request.PathValue("id")
	if org == "" {
		return e.BadRequestError("missing org", nil)
	}

	if !slices.Contains(user.OrgIDs, org) {
		return e.ForbiddenError("not a member of "+org, nil)
	}

	return e.JSON(http.StatusOK, p.base(org))
}

func (p *plugin) requireAuth(e *core.RequestEvent) (*IAMUser, error) {
	// If Base already authenticated the user, use that.
	//
	// The memberships come off the request rather than off the record, because a
	// record holds fields and membership is not one of Base's — IAM signs it onto
	// the token and resolveJWKSToken puts it here. Omitting it left OrgIDs empty,
	// so every Base was refused and the list of them came back empty: not "you
	// have no orgs" but "nobody thought to bring them along".
	if e.Auth != nil {
		orgs, _ := e.Get(apis.RequestEventKeyOrgs).([]string)
		return &IAMUser{
			ID:     e.Auth.Id,
			Email:  e.Auth.GetString("email"),
			Name:   e.Auth.GetString("name"),
			OrgIDs: orgs,
		}, nil
	}

	// Extract bearer token.
	authHeader := e.Request.Header.Get("Authorization")
	if !strings.HasPrefix(authHeader, "Bearer ") {
		return nil, e.UnauthorizedError("missing or invalid authorization", nil)
	}
	token := strings.TrimPrefix(authHeader, "Bearer ")

	user, err := p.iam.ValidateToken(token)
	if err != nil {
		return nil, e.UnauthorizedError("invalid or expired token", err)
	}

	return user, nil
}

// --------------------------------------------------------------------------
// Membership helpers
// --------------------------------------------------------------------------

func findMembership(app core.App, userId, orgId string) (*core.Record, error) {
	records, err := app.FindRecordsByFilter(
		collectionOrgMembers,
		"userId = {:userId} && orgId = {:orgId}",
		"",
		1, 0,
		map[string]any{"userId": userId, "orgId": orgId},
	)
	if err != nil || len(records) == 0 {
		return nil, fmt.Errorf("membership not found")
	}
	return records[0], nil
}

func addMember(app core.App, orgId, userId, role string) error {
	col, err := app.FindCollectionByNameOrId(collectionOrgMembers)
	if err != nil {
		return fmt.Errorf("_org_members collection not found: %w", err)
	}

	record := core.NewRecord(col)
	record.Set("orgId", orgId)
	record.Set("userId", userId)
	record.Set("role", role)

	return app.Save(record)
}

// checkAccess verifies that userId has at least the required permission level
// for the given org.
//
// Hierarchy: owner(4) > admin(3) > member(2) > viewer/read(1).
func checkAccess(app core.App, orgId, userId, permission string) bool {
	m, err := findMembership(app, userId, orgId)
	if err != nil {
		return false
	}
	return roleHasPermission(m.GetString("role"), permission)
}

func roleHasPermission(role, permission string) bool {
	levels := map[string]int{
		RoleViewer: 1, RoleMember: 2, RoleAdmin: 3, RoleOwner: 4,
	}
	required := map[string]int{
		"read": 1, "member": 2, "admin": 3, "owner": 4,
	}

	roleLevel, ok := levels[role]
	if !ok {
		return false
	}
	requiredLevel, ok := required[permission]
	if !ok {
		return false
	}
	return roleLevel >= requiredLevel
}

// --------------------------------------------------------------------------
// Compliance handlers
// --------------------------------------------------------------------------

func (p *plugin) handleCreateComplianceApp(e *core.RequestEvent) error {
	user, err := p.requireAuth(e)
	if err != nil {
		return err
	}

	var body struct {
		GivenName  string `json:"given_name"`
		FamilyName string `json:"family_name"`
		Country    string `json:"country"`
	}
	if err := e.BindBody(&body); err != nil {
		return e.BadRequestError("invalid request body", err)
	}

	appID, err := p.compliance.CreateApplication(body.GivenName, body.FamilyName, user.Email, body.Country)
	if err != nil {
		return e.InternalServerError("compliance: create application failed", err)
	}

	return e.JSON(http.StatusCreated, map[string]string{
		"application_id": appID,
		"user_id":        user.ID,
	})
}

func (p *plugin) handleInitiateKYC(e *core.RequestEvent) error {
	if _, err := p.requireAuth(e); err != nil {
		return err
	}

	applicationID := e.Request.PathValue("applicationId")
	if applicationID == "" {
		return e.BadRequestError("missing applicationId", nil)
	}

	var body struct {
		Provider string `json:"provider,omitempty"`
	}
	e.BindBody(&body)

	verID, redirectURL, err := p.compliance.InitiateKYC(applicationID, body.Provider)
	if err != nil {
		return e.InternalServerError("compliance: initiate KYC failed", err)
	}

	return e.JSON(http.StatusOK, map[string]string{
		"verification_id": verID,
		"redirect_url":    redirectURL,
	})
}

func (p *plugin) handleGetKYCStatus(e *core.RequestEvent) error {
	if _, err := p.requireAuth(e); err != nil {
		return err
	}

	applicationID := e.Request.PathValue("applicationId")
	if applicationID == "" {
		return e.BadRequestError("missing applicationId", nil)
	}

	status, err := p.compliance.GetKYCStatus(applicationID)
	if err != nil {
		return e.InternalServerError("compliance: get KYC status failed", err)
	}

	return e.JSON(http.StatusOK, status)
}

func (p *plugin) handleScreenIndividual(e *core.RequestEvent) error {
	if _, err := p.requireAuth(e); err != nil {
		return err
	}

	var body struct {
		GivenName  string `json:"given_name"`
		FamilyName string `json:"family_name"`
		Country    string `json:"country"`
	}
	if err := e.BindBody(&body); err != nil {
		return e.BadRequestError("invalid request body", err)
	}

	result, err := p.compliance.ScreenIndividual(body.GivenName, body.FamilyName, body.Country)
	if err != nil {
		return e.InternalServerError("compliance: screening failed", err)
	}

	return e.JSON(http.StatusOK, result)
}

func (p *plugin) handleValidatePayment(e *core.RequestEvent) error {
	if _, err := p.requireAuth(e); err != nil {
		return err
	}

	var body struct {
		FromAccountID string  `json:"from_account_id"`
		ToAccountID   string  `json:"to_account_id"`
		Amount        float64 `json:"amount"`
		Currency      string  `json:"currency"`
		Jurisdiction  string  `json:"jurisdiction"`
	}
	if err := e.BindBody(&body); err != nil {
		return e.BadRequestError("invalid request body", err)
	}

	approved, reason, err := p.compliance.ValidatePayment(
		body.FromAccountID, body.ToAccountID, body.Amount, body.Currency, body.Jurisdiction,
	)
	if err != nil {
		return e.InternalServerError("compliance: payment validation failed", err)
	}

	return e.JSON(http.StatusOK, map[string]interface{}{
		"approved": approved,
		"reason":   reason,
	})
}

// --------------------------------------------------------------------------
// Slug validation
// --------------------------------------------------------------------------

// isValidSlug checks that s contains only lowercase alphanumeric chars and
// hyphens, is non-empty, and does not start/end with a hyphen.
func isValidSlug(s string) bool {
	if s == "" || s[0] == '-' || s[len(s)-1] == '-' {
		return false
	}
	for _, ch := range s {
		if !((ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9') || ch == '-') {
			return false
		}
	}
	return true
}

// stampOrgOwnership force-sets owner+org on record create from the VALIDATED
// principal (IAM JWT auth record, or resolved IAM API key) — never from the
// request body or headers, so base attribution cannot be forged. Collections
// without owner/org fields (non-base) are left untouched. An org-scoped record
// with no trusted org (anonymous, no key) is refused — this is what makes the
// publishable key mandatory even when a collection's createRule is "".
func stampOrgOwnership(e *core.RecordRequestEvent) error {
	col := e.Collection
	hasOwner := col.Fields.GetByName("owner") != nil
	hasOrg := col.Fields.GetByName("org") != nil
	if col.System || col.IsAuth() || (!hasOwner && !hasOrg) {
		return e.Next()
	}

	// Derive identity from the validated principal ONLY.
	//
	// The org is the one the request acts in, resolved once from the credential
	// that authenticated it. This used to read the record's own org_id, which
	// was mirrored from the `owner` claim — the org of the APPLICATION the token
	// came through — so a record created by a member of alpha was stamped with
	// whichever org owned the sign-in app.
	owner := ""
	if e.Auth != nil { // IAM JWT session
		owner = e.Auth.Id
	} else if sub, _ := e.Get("authSub").(string); sub != "" { // IAM API key (pk-/sk-)
		owner = sub
	}
	org, _ := e.Get(apis.RequestEventKeyOrg).(string)

	if hasOrg {
		if org == "" {
			return e.ForbiddenError("Organization context required to create this record.", nil)
		}
		e.Record.Set("org", org)
	}
	if hasOwner {
		e.Record.Set("owner", owner) // may be "" for a pure-anon public submit
	}
	return e.Next()
}

// recordsCreateCollectionName returns the collection name if path targets the
// records CREATE endpoint (…/collections/<name>/records, no trailing id), else
// "". Prefix-agnostic (works under any BASE_API_PREFIX).
func recordsCreateCollectionName(path string) string {
	const marker = "/collections/"
	i := strings.Index(path, marker)
	if i < 0 {
		return ""
	}
	parts := strings.Split(strings.Trim(path[i+len(marker):], "/"), "/") // ["<name>","records"]
	if len(parts) == 2 && parts[1] == "records" {
		return parts[0]
	}
	return ""
}
