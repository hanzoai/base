package org

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/hanzoai/base/apis"
	"github.com/hanzoai/base/core"
	"github.com/hanzoai/base/tools/router"
)

// basesPath is the one address this plugin publishes. A Base is per org, so
// everything scoped to an org hangs off the Base it belongs to.
const basesPath = "/v1/bases"

// registerOrgRoutes registers what belongs to one Base.
//
// What these routes require of a credential is not stated here. It is a
// property of the address, stated once on the router — see the three rules
// below and where Register binds them — and it holds for whatever route sits at
// that address, this file's or anyone's.
func (p *plugin) registerOrgRoutes(r *router.Router[*core.RequestEvent]) {
	base := r.Group(basesPath + "/{orgId}")

	base.GET("", p.handleGetBase)
	base.GET("/config", p.handleGetOrgConfig)
	base.GET("/creds/{provider}", p.handleGetOrgCreds)
	base.POST("/creds/{provider}", p.handleSetOrgCreds)
	base.DELETE("/creds", p.handleInvalidateOrgCreds)
	base.GET("/customers/{userId}", p.handleGetCustomer)
	base.POST("/customers/{userId}", p.handleProvisionCustomer)
}

// caller is the subject the credential names, whichever door resolved it. An
// IAM key mints no auth record, so e.Auth answers nothing for one; and e.Auth.Id
// is a record id derived from the subject rather than the subject itself, so it
// does not compare against an id IAM issued.
func caller(e *core.RequestEvent) string {
	sub, _ := e.Get(apis.RequestEventKeySub).(string)
	return sub
}

// actingOrg is the org the request acts in, resolved at the door from the
// verified credential.
func actingOrg(e *core.RequestEvent) string {
	org, _ := e.Get(apis.RequestEventKeyOrg).(string)
	return org
}

// actsForUser reports whether the credential may act for the named user: it is
// that user, or it carries the org rather than a person — an org admin's token,
// the org's own secret key, or a platform operator.
func actsForUser(e *core.RequestEvent, user string) bool {
	if user != "" && user == caller(e) {
		return true
	}

	admin, _ := e.Get(apis.RequestEventKeyOrgAdmin).(bool)
	return admin || e.HasSuperuserAuth()
}

// addressed is what a path under /v1/bases names: the org, and — where the
// address has one — the user. Either may be empty.
type addressed struct {
	org  string
	user string
}

// address reads a request path as an address under /v1/bases, reporting false
// for a path that is not one.
//
// It reads the ESCAPED path and unescapes each segment itself, which is what
// the mux does to fill a path parameter. Reading URL.Path instead compares
// against something already decoded: /v1/bases/beta%2Fx arrives there as
// /v1/bases/beta/x, which is a different address with a different org in it.
//
// A segment names what it names however the address continues past it. Reading
// the user only where the path STOPS there would recognise the seven shapes
// this file publishes and no others, and the rules exist precisely because
// routes are registered here and elsewhere at the same addresses.
func address(escaped string) (addressed, bool) {
	if escaped == basesPath {
		return addressed{}, true
	}

	rest, ok := strings.CutPrefix(escaped, basesPath+"/")
	if !ok {
		return addressed{}, false
	}

	segments := strings.Split(strings.TrimSuffix(rest, "/"), "/")

	a := addressed{org: unescape(segments[0])}
	if len(segments) >= 3 && segments[1] == "customers" {
		a.user = unescape(segments[2])
	}

	return a, true
}

func unescape(segment string) string {
	if decoded, err := url.PathUnescape(segment); err == nil {
		return decoded
	}
	return segment
}

// publishableReachesNoBase refuses a publishable key everything under
// /v1/bases.
//
// A pk- key is the one you paste into a web page. It travels in ?key=, so
// reaching these routes with it needs no Authorization header and no secret at
// all — the page source is the credential. The only thing that stood between it
// and an org's provider secrets was a check that publishable keys may not
// WRITE, and every read here is a GET: the page's own key returned the
// platform's live Stripe key and every member's billing identity. Read-only was
// the wrong reading of publishable. Publishable means public, and nothing under
// this prefix is.
//
// It is refused by kind rather than by method, so a read is refused for the
// same reason a write is and no future GET can be added to the subtree without
// inheriting it.
func publishableReachesNoBase(e *core.RequestEvent) error {
	if _, ok := address(e.Request.URL.EscapedPath()); !ok {
		return e.Next()
	}

	if kind, _ := e.Get(keyKind).(string); kind == keyPublishable {
		return e.ForbiddenError("A publishable key is public and reaches no Base.", nil)
	}

	return e.Next()
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
	named, ok := address(e.Request.URL.EscapedPath())
	if !ok || named.org == "" {
		return e.Next() // /v1/bases itself names no org and answers per membership
	}

	if named.org != actingOrg(e) {
		return e.ForbiddenError("The credential does not act in the requested organization.", nil)
	}

	return e.Next()
}

// namesItsOwnUser refuses a request naming a user other than the caller.
//
// /customers/{userId} compared orgs and said nothing at all about users, so an
// ordinary member's own token read any colleague's row — the customer id, the
// broker account, the commerce customer, the vault. Belonging to an org is not
// the same fact as being that person.
//
// An org's own admin, and a secret key issued to the org, do act for the whole
// org and reach every member's row. A platform operator reaches it the way it
// reaches everything else.
func namesItsOwnUser(e *core.RequestEvent) error {
	named, ok := address(e.Request.URL.EscapedPath())
	if !ok || named.user == "" {
		return e.Next()
	}

	if !actsForUser(e, named.user) {
		return e.ForbiddenError("The credential acts for its own user only.", nil)
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
