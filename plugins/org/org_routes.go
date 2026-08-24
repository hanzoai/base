package org

import (
	"net/http"
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

// caller is the person the credential names, spelled the ONE way IAM spells a
// person: <owner>/<name>, the account's own org and its username. It is the
// name a customer row is filed under, so one person is one row whichever door
// they came through.
//
// The two doors learn different things about the same account. IAM's key
// endpoint answers with the account and never its opaque id, so a key can only
// name a person owner/name. A token carries that id in `sub` — and carries the
// membership set home org first, and the username in `name`. IAM builds both
// halves from the same user row (its own natural key is owner/name and its
// `orgs` claim leads with the account's owner), so owner/name is the one
// spelling both doors produce.
//
// A credential that names no person — a machine token, which carries neither a
// membership nor a username — is its own subject.
func caller(e *core.RequestEvent) string {
	name, _ := e.Get(apis.RequestEventKeyName).(string)
	orgs, _ := e.Get(apis.RequestEventKeyOrgs).([]string)
	if name != "" && len(orgs) > 0 && orgs[0] != "" {
		return orgs[0] + "/" + name
	}

	return subject(e)
}

// subject is what the credential itself says its subject is: owner/name from a
// key, IAM's opaque account id from a token. A row filed before the two doors
// spelled a person one way is filed under this, so it is the second name the
// same person is recognised by.
//
// An IAM key mints no auth record, so e.Auth answers nothing for one; and
// e.Auth.Id is a record id derived from the subject rather than the subject
// itself, so it does not compare against anything IAM issued.
func subject(e *core.RequestEvent) string {
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
// that person — under either name the credential carries for them — or it
// carries the org rather than a person: an org admin's token, the org's own
// secret key, or a platform operator.
//
// Both names belong to the one account the credential resolved, so recognising
// either widens nothing. It is what keeps a person's own row reachable while
// rows filed under the older of the two names are still about.
func actsForUser(e *core.RequestEvent, user string) bool {
	if user != "" && (user == caller(e) || user == subject(e)) {
		return true
	}

	return actsForOrg(e)
}

// actsForOrg reports whether the credential carries authority over the org it
// acts in rather than only over its own subject: an org admin's token, the
// org's own secret key, or a platform operator. A member's token is not that.
func actsForOrg(e *core.RequestEvent) bool {
	admin, _ := e.Get(apis.RequestEventKeyOrgAdmin).(bool)
	return admin || e.HasSuperuserAuth()
}

// addressed is what an address under /v1/bases names: the org, whether the
// address names the org's own credentials, and — where the address has one —
// the user.
//
// root is the collection itself, /v1/bases, the ONE address under this prefix
// that legitimately names no org: it answers the membership question. Anywhere
// else, naming no org is a fact about the address and not a permission.
type addressed struct {
	root  bool
	org   string
	creds bool
	user  string
}

// address reads what the ROUTER matched as an address under /v1/bases,
// reporting false for a request it matched somewhere else.
//
// The router's answer is the one the handler acts on, so it is the one the rule
// has to read. http.ServeMux matches by splitting the ESCAPED path on "/" and
// unescaping each segment, so one address has many spellings and all of them
// reach the same handler. A rule that compares the escaped string against a
// literal prefix answers about a different address than the one being served.
//
// The router's answer is the pattern it chose and the values it filled, so this
// takes its segments from the pattern: the value bound where the pattern names
// a wildcard, the segment itself where the pattern states it — so a route that
// writes the org into its address is read like one that takes it as a
// parameter. The handlers below read those same values, which is what leaves
// rule and handler no way to disagree about who is addressed.
//
// A segment names what it names however the address continues past it. Reading
// the user only where the address STOPS there would recognise the seven shapes
// this file publishes and no others, and the rules exist precisely because
// routes are registered here and elsewhere at the same addresses.
func address(r *http.Request) (addressed, bool) {
	matched := patternPath(r.Pattern)
	if matched != basesPath && !strings.HasPrefix(matched, basesPath+"/") {
		return addressed{}, false
	}
	if matched == basesPath {
		return addressed{root: true}, true
	}

	segments := strings.Split(strings.Trim(matched, "/"), "/") // v1, bases, ...

	a := addressed{org: matchedSegment(r, segments, 2)}
	switch matchedSegment(r, segments, 3) {
	case "creds":
		a.creds = true
	case "customers":
		a.user = matchedSegment(r, segments, 4)
	}

	return a, true
}

// patternPath is the path of a matched pattern. A pattern is written
// "[METHOD ][HOST]/[PATH]" and so begins at its first slash; a request the
// router matched against no pattern has none, and reaches nothing here.
func patternPath(pattern string) string {
	if i := strings.IndexByte(pattern, '/'); i >= 0 {
		return pattern[i:]
	}
	return ""
}

// matchedSegment reads one segment of the matched pattern: the value the router
// filled for a wildcard, the segment itself where the pattern states it.
func matchedSegment(r *http.Request, segments []string, i int) string {
	if i >= len(segments) {
		return ""
	}

	name, wild := strings.CutPrefix(segments[i], "{")
	if !wild {
		return segments[i]
	}

	return r.PathValue(strings.TrimSuffix(strings.TrimSuffix(name, "}"), "..."))
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
	if _, ok := address(e.Request); !ok {
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
//
// An address under this prefix that BINDS no org is refused too, and the two
// refusals are the same one: a request whose address names no org and whose
// credential resolves none compares "" against "" and matches. Only the
// collection itself may name no org, and it says so by being that exact
// address. Everything else under the prefix carries an org somewhere in it —
// /v1/bases/ as a subtree pattern binds it into a later segment, where a rule
// reading position 2 cannot see it — so an address that binds none is one this
// rule cannot answer about, and what it cannot answer about it refuses.
func actsInNamedOrg(e *core.RequestEvent) error {
	named, ok := address(e.Request)
	if !ok || named.root {
		return e.Next() // /v1/bases itself names no org and answers per membership
	}

	if named.org == "" || named.org != actingOrg(e) {
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
	named, ok := address(e.Request)
	if !ok || named.user == "" {
		return e.Next()
	}

	if !actsForUser(e, named.user) {
		return e.ForbiddenError("The credential acts for its own user only.", nil)
	}

	return e.Next()
}

// secretsBelongToTheOrg refuses the org's provider credentials to a credential
// that carries only membership.
//
// A provider credential is the ORG's — the key its Stripe account charges on,
// the secret its webhooks are verified with — and every member of an org held
// one. Belonging to an org is not the same fact as speaking for it, which is
// the same distinction /customers/{userId} draws and the same one an org admin
// exists to hold.
//
// Read and write are one fact here, so both are refused together: whoever reads
// the key can spend it, and whoever writes it points the org's integration at
// an account of their own choosing.
//
// An org admin's token, the org's own secret key, and a platform operator all
// act for the org and reach them.
func secretsBelongToTheOrg(e *core.RequestEvent) error {
	named, ok := address(e.Request)
	if !ok || !named.creds {
		return e.Next()
	}

	if !actsForOrg(e) {
		return e.ForbiddenError("The organization's credentials are reached by a credential that acts for the organization.", nil)
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
