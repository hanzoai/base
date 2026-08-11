package org

import (
	"net/http"

	"github.com/hanzoai/base/core"
	"github.com/hanzoai/base/tools/router"
)

// bases is the one address this plugin publishes. A Base is per org, so
// everything scoped to an org hangs off the Base it belongs to.
const bases = "/v1/bases"

// registerOrgRoutes registers what belongs to one Base.
func (p *plugin) registerOrgRoutes(r *router.Router[*core.RequestEvent]) {
	api := r.Group(bases)

	api.GET("/{orgId}/config", p.handleGetOrgConfig)
	api.GET("/{orgId}/creds/{provider}", p.handleGetOrgCreds)
	api.POST("/{orgId}/creds/{provider}", p.handleSetOrgCreds)
	api.DELETE("/{orgId}/creds", p.handleInvalidateOrgCreds)
	api.GET("/{orgId}/customers/{userId}", p.handleGetCustomer)
	api.POST("/{orgId}/customers/{userId}", p.handleProvisionCustomer)
}

func (p *plugin) handleGetOrgConfig(e *core.RequestEvent) error {
	if _, err := p.requireAuth(e); err != nil {
		return err
	}

	orgId := e.Request.PathValue("orgId")
	if orgId == "" {
		return e.BadRequestError("missing orgId", nil)
	}

	config := p.org.GetConfig(orgId)
	if config == nil {
		return e.NotFoundError("org config not found", nil)
	}

	return e.JSON(http.StatusOK, config)
}

func (p *plugin) handleGetOrgCreds(e *core.RequestEvent) error {
	user, err := p.requireAuth(e)
	if err != nil {
		return err
	}

	orgId := e.Request.PathValue("orgId")
	provider := e.Request.PathValue("provider")
	if orgId == "" || provider == "" {
		return e.BadRequestError("missing orgId or provider", nil)
	}

	// Require at least admin access to read credentials.
	if !checkOrgAccess(p.app, orgId, user.ID) {
		return e.ForbiddenError("access denied", nil)
	}

	creds := p.org.GetCreds(orgId, provider)
	if creds == nil {
		return e.NotFoundError("credentials not found", nil)
	}

	return e.JSON(http.StatusOK, creds)
}

func (p *plugin) handleSetOrgCreds(e *core.RequestEvent) error {
	user, err := p.requireAuth(e)
	if err != nil {
		return err
	}

	orgId := e.Request.PathValue("orgId")
	provider := e.Request.PathValue("provider")
	if orgId == "" || provider == "" {
		return e.BadRequestError("missing orgId or provider", nil)
	}

	if !checkOrgAccess(p.app, orgId, user.ID) {
		return e.ForbiddenError("access denied", nil)
	}

	var body map[string]string
	if err := e.BindBody(&body); err != nil {
		return e.BadRequestError("invalid request body", err)
	}

	if err := p.org.SetCreds(orgId, provider, body); err != nil {
		return e.InternalServerError("failed to set credentials", err)
	}

	return e.JSON(http.StatusOK, map[string]bool{"ok": true})
}

func (p *plugin) handleInvalidateOrgCreds(e *core.RequestEvent) error {
	user, err := p.requireAuth(e)
	if err != nil {
		return err
	}

	orgId := e.Request.PathValue("orgId")
	if orgId == "" {
		return e.BadRequestError("missing orgId", nil)
	}

	if !checkOrgAccess(p.app, orgId, user.ID) {
		return e.ForbiddenError("access denied", nil)
	}

	p.org.InvalidateCreds(orgId)

	return e.JSON(http.StatusOK, map[string]bool{"ok": true})
}

func (p *plugin) handleGetCustomer(e *core.RequestEvent) error {
	if _, err := p.requireAuth(e); err != nil {
		return err
	}

	orgId := e.Request.PathValue("orgId")
	userId := e.Request.PathValue("userId")
	if orgId == "" || userId == "" {
		return e.BadRequestError("missing orgId or userId", nil)
	}

	customer := p.org.GetCustomer(orgId, userId)
	if customer == nil {
		return e.NotFoundError("customer not found", nil)
	}

	return e.JSON(http.StatusOK, customer)
}

func (p *plugin) handleProvisionCustomer(e *core.RequestEvent) error {
	if _, err := p.requireAuth(e); err != nil {
		return err
	}

	orgId := e.Request.PathValue("orgId")
	userId := e.Request.PathValue("userId")
	if orgId == "" || userId == "" {
		return e.BadRequestError("missing orgId or userId", nil)
	}

	var opts map[string]any
	e.BindBody(&opts) // optional body

	customer, err := p.org.GetOrProvisionCustomer(orgId, userId)
	if err != nil {
		return e.InternalServerError("failed to provision customer", err)
	}

	return e.JSON(http.StatusCreated, customer)
}

// checkOrgAccess verifies that a user has access to an org.
// Looks up the org_configs record by org_id to find the org,
// then checks membership. For now, checks if user has any org
// with a matching iamOrgId.
func checkOrgAccess(app core.App, orgId, userId string) bool {
	// Find org with matching iamOrgId.
	records, err := app.FindRecordsByFilter(
		collectionOrgs,
		"iamOrgId = {:orgId}",
		"",
		1, 0,
		map[string]any{"orgId": orgId},
	)
	if err != nil || len(records) == 0 {
		// If no org found with iamOrgId, allow access (the org may be
		// managed outside the org system, e.g. via IAM directly).
		return true
	}

	return checkAccess(app, records[0].Id, userId, "admin")
}
