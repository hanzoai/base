// Package platform implements a multi-org platform plugin for Hanzo Base.
//
// Each org gets isolated collections with prefix-based namespacing.
// Authentication is handled via Hanzo IAM (hanzo.id) OAuth2 and secrets via
// Hanzo KMS (kms.hanzo.ai) Universal Auth.
//
// Example:
//
//	platform.MustRegister(app, platform.PlatformConfig{
//		IAMEndpoint:     "https://hanzo.id",
//		KMSEndpoint:     "https://kms.hanzo.ai",
//		IAMClientID:     "my-client-id",
//		IAMClientSecret: "my-client-secret",
//	})
package platform

import (
	"fmt"
	"log/slog"
	"net/http"
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

// PlatformConfig defines the configuration for the platform plugin.
type PlatformConfig struct {
	// IAMEndpoint is the base URL for Hanzo IAM (default: "https://hanzo.id").
	IAMEndpoint string

	// KMSEndpoint is the base URL for Hanzo KMS (default: "https://kms.hanzo.ai").
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

	// OrgIsolation controls how org data is physically separated.
	//   "prefix"   — (default) t_{slug}_ prefixed collections in shared DB
	//   "sqlite"   — separate encrypted SQLite file per org in DataDir/orgs/
	//   "cloudsql" — separate PostgreSQL database per org (requires cloudsql plugin)
	//
	// For "sqlite" mode, each org gets its own database file at:
	//   {DataDir}/orgs/{slug}/data.db
	// The file can be independently encrypted, backed up, and deleted.
	// PII is physically isolated — zero data commingling.
	OrgIsolation string

	// OrgEncryptionKey is the master key for deriving per-org DEKs.
	// Used for both SQLite encryption AND S3 SSE-C key derivation.
	// Each org gets: HMAC-SHA256(masterKey, orgSlug)
	// Each user gets: HMAC-SHA256(masterKey, orgSlug:userId)
	// If empty, encryption is disabled (dev mode).
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
func MustRegister(app core.App, config PlatformConfig) {
	if err := Register(app, config); err != nil {
		panic(err)
	}
}

// Register registers the platform plugin to the provided app instance.
func Register(app core.App, config PlatformConfig) error {
	if config.IAMEndpoint == "" {
		config.IAMEndpoint = "https://hanzo.id"
	}
	if config.KMSEndpoint == "" {
		config.KMSEndpoint = "https://kms.hanzo.ai"
	}

	kmsClient := NewKMSClient(config.KMSEndpoint, "")

	p := &plugin{
		app:        app,
		config:     config,
		iam:        NewIAMClient(config.IAMEndpoint),
		compliance: NewComplianceClient(config.ComplianceEndpoint, config.ComplianceAPIKey),
		org:        &OrgService{app: app, kms: kmsClient, config: config},
	}

	// Bootstrap: ensure system collections exist.
	app.OnBootstrap().BindFunc(func(e *core.BootstrapEvent) error {
		if err := e.Next(); err != nil {
			return err
		}
		return p.ensureSystemCollections()
	})

	// Configure the external identity provider as the exclusive auth source.
	// All Base routes accept OIDC/JWKS tokens; built-in auth endpoints
	// (password, OTP, email-change, etc.) are disabled for non-superuser
	// collections.
	jwksURL := strings.TrimRight(config.IAMEndpoint, "/") + "/.well-known/jwks"
	app.Store().Set(apis.StoreKeyJWKSURL, jwksURL)
	app.Store().Set(apis.StoreKeyExternalAuthOnly, true)

	app.Logger().Info("platform: external auth enabled — built-in auth disabled for non-superuser collections",
		slog.String("jwksURL", jwksURL),
		slog.String("authEndpoint", config.IAMEndpoint),
	)

	// Serve: register API routes, identity header middleware, and org-scoping.
	app.OnServe().BindFunc(func(e *core.ServeEvent) error {
		// Expose OrgService in app store for Goja JS access.
		app.Store().Set("org", p.org)

		// Global middleware: set identity headers from authenticated user.
		// Runs after loadAuthToken (which has priority -1020), so e.Auth
		// is already set if the token was valid (local or IAM JWKS).
		e.Router.Bind(&hook.Handler[*core.RequestEvent]{
			Id:       "platformIdentityHeaders",
			Priority: apis.DefaultLoadAuthTokenMiddlewarePriority + 1,
			Func: func(re *core.RequestEvent) error {
				if re.Auth != nil {
					userId := re.Auth.Id
					email := re.Auth.GetString("email")
					orgId := re.Auth.GetString("org_id")

					// If org_id wasn't on the record, check claims stored by loadAuthToken.
					if orgId == "" {
						if v, _ := re.Get("authOwner").(string); v != "" {
							orgId = v
						}
					}

					// Set identity headers for downstream handlers.
					// Standard X-User-Id / X-Org-Id — one way, no vendor prefix.
					re.Request.Header.Set("X-User-Id", userId)
					re.Request.Header.Set("X-Org-Id", orgId)
					re.Request.Header.Set("X-User-Email", email)
				}
				return re.Next()
			},
		})

		p.registerRoutes(e.Router)
		p.registerAuthRoutes(e.Router)
		p.registerOrgRoutes(e.Router)
		return e.Next()
	})

	return nil
}

type plugin struct {
	app        core.App
	config     PlatformConfig
	iam        *IAMClient
	compliance *ComplianceClient
	org        *OrgService
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

	p.app.Logger().Info("creating platform system collection", slog.String("name", collectionOrgs))
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

	p.app.Logger().Info("creating platform system collection", slog.String("name", collectionOrgMembers))
	return p.app.Save(c)
}

// --------------------------------------------------------------------------
// Route registration
// --------------------------------------------------------------------------

func (p *plugin) registerRoutes(r *router.Router[*core.RequestEvent]) {
	api := r.Group("/api/platform")

	// Org CRUD
	api.POST("/orgs", p.handleCreateOrg)
	api.GET("/orgs", p.handleListOrgs)
	api.GET("/orgs/{id}", p.handleGetOrg)
	api.DELETE("/orgs/{id}", p.handleDeleteOrg)

	// Member management
	api.POST("/orgs/{id}/members", p.handleInviteMember)

	// Compliance (optional — only registered if endpoint configured)
	if p.compliance != nil && p.compliance.Enabled() {
		api.POST("/compliance/application", p.handleCreateComplianceApp)
		api.POST("/compliance/kyc/{applicationId}", p.handleInitiateKYC)
		api.GET("/compliance/kyc/{applicationId}", p.handleGetKYCStatus)
		api.POST("/compliance/screen", p.handleScreenIndividual)
		api.POST("/compliance/payment/validate", p.handleValidatePayment)
	}
}

// --------------------------------------------------------------------------
// Route handlers
// --------------------------------------------------------------------------

func (p *plugin) handleCreateOrg(e *core.RequestEvent) error {
	user, err := p.requireAuth(e)
	if err != nil {
		return err
	}

	var body struct {
		Name string `json:"name"`
		Slug string `json:"slug"`
	}
	if err := e.BindBody(&body); err != nil {
		return e.BadRequestError("Invalid request body.", err)
	}

	body.Name = strings.TrimSpace(body.Name)
	body.Slug = strings.TrimSpace(body.Slug)

	if body.Name == "" || body.Slug == "" {
		return e.BadRequestError("name and slug are required", nil)
	}
	if !isValidSlug(body.Slug) {
		return e.BadRequestError("slug must be lowercase alphanumeric with hyphens, no leading/trailing hyphen", nil)
	}

	// Check slug uniqueness.
	existing, _ := p.app.FindFirstRecordByData(collectionOrgs, "slug", body.Slug)
	if existing != nil {
		return e.BadRequestError("slug already in use", nil)
	}

	// Create KMS project for org (best-effort).
	var kmsProjectId string
	if p.config.IAMClientID != "" && p.config.KMSEndpoint != "" {
		pid, kmsErr := CreateOrgProject(body.Slug, p.config)
		if kmsErr != nil {
			p.app.Logger().Warn("failed to create KMS project",
				slog.String("slug", body.Slug),
				slog.String("error", kmsErr.Error()),
			)
		} else {
			kmsProjectId = pid
		}
	}

	// Create org record.
	col, err := p.app.FindCollectionByNameOrId(collectionOrgs)
	if err != nil {
		return e.InternalServerError("_orgs collection not found", err)
	}

	org := core.NewRecord(col)
	org.Set("name", body.Name)
	org.Set("slug", body.Slug)
	org.Set("ownerId", user.ID)
	org.Set("iamOrgId", "")
	org.Set("kmsProjectId", kmsProjectId)

	if err := p.app.Save(org); err != nil {
		return e.InternalServerError("failed to create org", err)
	}

	// Create owner membership.
	if err := addMember(p.app, org.Id, user.ID, RoleOwner); err != nil {
		_ = p.app.Delete(org)
		return e.InternalServerError("failed to create owner membership", err)
	}

	// Create org collections from templates.
	if len(p.config.DefaultTemplates) > 0 {
		if err := CreateOrgCollections(p.app, body.Slug, p.config.DefaultTemplates); err != nil {
			p.app.Logger().Warn("failed to create org collections",
				slog.String("slug", body.Slug),
				slog.String("error", err.Error()),
			)
		}
	}

	return e.JSON(http.StatusCreated, map[string]any{
		"id":           org.Id,
		"name":         body.Name,
		"slug":         body.Slug,
		"kmsProjectId": kmsProjectId,
	})
}

func (p *plugin) handleListOrgs(e *core.RequestEvent) error {
	user, err := p.requireAuth(e)
	if err != nil {
		return err
	}

	// Find all memberships for this user.
	members, err := p.app.FindRecordsByFilter(
		collectionOrgMembers,
		"userId = {:userId}",
		"",
		0, 0,
		map[string]any{"userId": user.ID},
	)
	if err != nil {
		return e.InternalServerError("failed to query memberships", err)
	}

	if len(members) == 0 {
		return e.JSON(http.StatusOK, []any{})
	}

	orgIds := make([]string, 0, len(members))
	rolesByOrg := make(map[string]string, len(members))
	for _, m := range members {
		oid := m.GetString("orgId")
		orgIds = append(orgIds, oid)
		rolesByOrg[oid] = m.GetString("role")
	}

	orgs, err := p.app.FindRecordsByIds(collectionOrgs, orgIds)
	if err != nil {
		return e.InternalServerError("failed to fetch orgs", err)
	}

	result := make([]map[string]any, 0, len(orgs))
	for _, o := range orgs {
		result = append(result, map[string]any{
			"id":   o.Id,
			"name": o.GetString("name"),
			"slug": o.GetString("slug"),
			"role": rolesByOrg[o.Id],
		})
	}

	return e.JSON(http.StatusOK, result)
}

func (p *plugin) handleGetOrg(e *core.RequestEvent) error {
	user, err := p.requireAuth(e)
	if err != nil {
		return err
	}

	orgId := e.Request.PathValue("id")
	if orgId == "" {
		return e.BadRequestError("missing org id", nil)
	}

	if !checkAccess(p.app, orgId, user.ID, "read") {
		return e.ForbiddenError("access denied", nil)
	}

	org, err := p.app.FindRecordById(collectionOrgs, orgId)
	if err != nil {
		return e.NotFoundError("org not found", err)
	}

	return e.JSON(http.StatusOK, map[string]any{
		"id":           org.Id,
		"name":         org.GetString("name"),
		"slug":         org.GetString("slug"),
		"ownerId":      org.GetString("ownerId"),
		"iamOrgId":     org.GetString("iamOrgId"),
		"kmsProjectId": org.GetString("kmsProjectId"),
	})
}

func (p *plugin) handleDeleteOrg(e *core.RequestEvent) error {
	user, err := p.requireAuth(e)
	if err != nil {
		return err
	}

	orgId := e.Request.PathValue("id")
	if orgId == "" {
		return e.BadRequestError("missing org id", nil)
	}

	org, err := p.app.FindRecordById(collectionOrgs, orgId)
	if err != nil {
		return e.NotFoundError("org not found", err)
	}

	// Owner only.
	if org.GetString("ownerId") != user.ID {
		return e.ForbiddenError("only the owner can delete an org", nil)
	}

	slug := org.GetString("slug")

	// Delete all org-prefixed collections.
	prefix := OrgPrefix(slug)
	allCollections, err := p.app.FindAllCollections()
	if err == nil {
		for _, col := range allCollections {
			if strings.HasPrefix(col.Name, prefix) {
				if delErr := p.app.Delete(col); delErr != nil {
					p.app.Logger().Warn("failed to delete org collection",
						slog.String("collection", col.Name),
						slog.String("error", delErr.Error()),
					)
				}
			}
		}
	}

	// Delete all memberships.
	memberships, _ := p.app.FindRecordsByFilter(
		collectionOrgMembers,
		"orgId = {:orgId}",
		"", 0, 0,
		map[string]any{"orgId": orgId},
	)
	for _, m := range memberships {
		_ = p.app.Delete(m)
	}

	// Delete org record.
	if err := p.app.Delete(org); err != nil {
		return e.InternalServerError("failed to delete org", err)
	}

	return e.JSON(http.StatusOK, map[string]bool{"deleted": true})
}

func (p *plugin) handleInviteMember(e *core.RequestEvent) error {
	user, err := p.requireAuth(e)
	if err != nil {
		return err
	}

	orgId := e.Request.PathValue("id")
	if orgId == "" {
		return e.BadRequestError("missing org id", nil)
	}

	// Require admin or owner.
	if !checkAccess(p.app, orgId, user.ID, "admin") {
		return e.ForbiddenError("only owners and admins can invite members", nil)
	}

	var body struct {
		UserID string `json:"userId"`
		Role   string `json:"role"`
	}
	if err := e.BindBody(&body); err != nil {
		return e.BadRequestError("invalid request body", err)
	}

	body.UserID = strings.TrimSpace(body.UserID)
	body.Role = strings.TrimSpace(body.Role)

	if body.UserID == "" {
		return e.BadRequestError("userId is required", nil)
	}

	// Validate role.
	validRoles := map[string]bool{RoleAdmin: true, RoleMember: true, RoleViewer: true}
	if !validRoles[body.Role] {
		body.Role = RoleMember
	}

	// Check if already a member.
	existing, _ := findMembership(p.app, body.UserID, orgId)
	if existing != nil {
		return e.BadRequestError("user is already a member of this org", nil)
	}

	if err := addMember(p.app, orgId, body.UserID, body.Role); err != nil {
		return e.InternalServerError("failed to add member", err)
	}

	return e.JSON(http.StatusCreated, map[string]any{
		"orgId":  orgId,
		"userId": body.UserID,
		"role":   body.Role,
	})
}

// --------------------------------------------------------------------------
// Auth helper
// --------------------------------------------------------------------------

// requireAuth extracts and validates the Bearer token. Checks Base built-in
// auth first, then falls back to IAM token validation.
func (p *plugin) requireAuth(e *core.RequestEvent) (*IAMUser, error) {
	// If Base already authenticated the user, use that.
	if e.Auth != nil {
		return &IAMUser{
			ID:    e.Auth.Id,
			Email: e.Auth.GetString("email"),
			Name:  e.Auth.GetString("name"),
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
