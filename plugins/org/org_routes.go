package org

import (
	"net/http"

	"github.com/hanzoai/base/apis"
	"github.com/hanzoai/base/core"
	"github.com/hanzoai/base/tools/router"
)

// basesPath is the one address this plugin publishes. A Base is per org, so
// everything scoped to an org hangs off the Base it belongs to.
const basesPath = "/v1/bases"

// registerOrgRoutes registers what belongs to one Base.
//
// Every route below names an org in its path, and one sentence governs all of
// them: the org a request names is the org it acts in. Which org that is was
// settled once, at the door, from the verified credential — the subject's home
// org, an org its membership set admits and the request selected, or any org at
// all for a platform operator carrying the reserved admin membership.
//
// The sentence is stated on the subtree rather than inside each handler, which
// is what makes a handler that forgets it impossible to write. Three of these
// seven forgot: /config and /customers asked nothing, and /creds asked a
// question about a local membership table that IAM owns and this package never
// writes, so it found no row and read that as permission.
func (p *plugin) registerOrgRoutes(r *router.Router[*core.RequestEvent]) {
	base := r.Group(basesPath + "/{orgId}").BindFunc(actsInNamedOrg)

	base.GET("", p.handleGetBase)
	base.GET("/config", p.handleGetOrgConfig)
	base.GET("/creds/{provider}", p.handleGetOrgCreds)
	base.POST("/creds/{provider}", p.handleSetOrgCreds)
	base.DELETE("/creds", p.handleInvalidateOrgCreds)
	base.GET("/customers/{userId}", p.handleGetCustomer)
	base.POST("/customers/{userId}", p.handleProvisionCustomer)
}

// actsInNamedOrg refuses a request naming an org other than the one its
// credential acts in.
//
// The refusal is 403 and never 404. A 404 is an answer about the data — it says
// the check passed and there was no such row — so a caller who gets one learns
// its token reaches that org, which is the fact worth withholding. It is also
// the only reason nothing has leaked yet: every org's config row is absent
// today, so the reads that were admitted found nothing to hand back.
//
// A credential that resolved no org at all reaches nothing here. That is the
// same posture the door already takes, where a token carrying no membership is
// a machine and is refused rather than served from the process's own Base.
func actsInNamedOrg(e *core.RequestEvent) error {
	named := e.Request.PathValue("orgId")
	acting, _ := e.Get(apis.RequestEventKeyOrg).(string)

	if named == "" || named != acting {
		return e.ForbiddenError("The credential does not act in the requested organization.", nil)
	}

	return e.Next()
}

func (p *plugin) handleGetOrgConfig(e *core.RequestEvent) error {
	config := p.org.GetConfig(e.Request.PathValue("orgId"))
	if config == nil {
		return e.NotFoundError("org config not found", nil)
	}

	return e.JSON(http.StatusOK, config)
}

func (p *plugin) handleGetOrgCreds(e *core.RequestEvent) error {
	creds := p.org.GetCreds(e.Request.PathValue("orgId"), e.Request.PathValue("provider"))
	if creds == nil {
		return e.NotFoundError("credentials not found", nil)
	}

	return e.JSON(http.StatusOK, creds)
}

func (p *plugin) handleSetOrgCreds(e *core.RequestEvent) error {
	var body map[string]string
	if err := e.BindBody(&body); err != nil {
		return e.BadRequestError("invalid request body", err)
	}

	err := p.org.SetCreds(e.Request.PathValue("orgId"), e.Request.PathValue("provider"), body)
	if err != nil {
		return e.InternalServerError("failed to set credentials", err)
	}

	return e.JSON(http.StatusOK, map[string]bool{"ok": true})
}

func (p *plugin) handleInvalidateOrgCreds(e *core.RequestEvent) error {
	p.org.InvalidateCreds(e.Request.PathValue("orgId"))

	return e.JSON(http.StatusOK, map[string]bool{"ok": true})
}

func (p *plugin) handleGetCustomer(e *core.RequestEvent) error {
	customer := p.org.GetCustomer(e.Request.PathValue("orgId"), e.Request.PathValue("userId"))
	if customer == nil {
		return e.NotFoundError("customer not found", nil)
	}

	return e.JSON(http.StatusOK, customer)
}

func (p *plugin) handleProvisionCustomer(e *core.RequestEvent) error {
	customer, err := p.org.GetOrProvisionCustomer(
		e.Request.PathValue("orgId"), e.Request.PathValue("userId"))
	if err != nil {
		return e.InternalServerError("failed to provision customer", err)
	}

	return e.JSON(http.StatusCreated, customer)
}
