package platform

// Dev-only convenience: an "Sign in as root (dev)" button that
// auto-authenticates as the seeded embedded-IAM root user without
// prompting for a password. Strictly opt-in via env so it can't
// accidentally ship to prod:
//
//   IAM_DEV_AUTOLOGIN=true
//
// AND the bootstrap root must exist (EMBEDDED_IAM_ROOT_EMAIL must
// have a matching row in _iam_users). Without both, the endpoint
// returns 403 — guard rails for dev-only behavior.

import (
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/hanzoai/base/core"
	"github.com/hanzoai/base/tools/router"
)

func devAutoLoginEnabled() bool {
	return os.Getenv("IAM_DEV_AUTOLOGIN") == "true"
}

func (p *plugin) registerEmbeddedDevRoutes(r *router.Router[*core.RequestEvent]) {
	if p.embeddedIAM == nil || !devAutoLoginEnabled() {
		return
	}
	r.GET(embeddedIAMMount+"/oauth/dev-login", p.handleDevAutoLogin)
}

// handleDevAutoLogin mints an OIDC code for the bootstrap root user
// and redirects to the caller's redirect_uri — equivalent to a
// successful password login, no credentials prompt.
func (p *plugin) handleDevAutoLogin(e *core.RequestEvent) error {
	if !devAutoLoginEnabled() {
		return e.ForbiddenError("dev autologin disabled", nil)
	}
	q := e.Request.URL.Query()
	clientID := q.Get("client_id")
	redirectURI := q.Get("redirect_uri")
	state := q.Get("state")
	if clientID == "" || redirectURI == "" {
		return e.BadRequestError("client_id and redirect_uri required", nil)
	}

	rootEmail := strings.TrimSpace(strings.ToLower(os.Getenv("EMBEDDED_IAM_ROOT_EMAIL")))
	if rootEmail == "" {
		return e.ForbiddenError("EMBEDDED_IAM_ROOT_EMAIL not set", nil)
	}

	user, err := p.app.FindFirstRecordByData(collectionIAMUsers, "email", rootEmail)
	if err != nil || user == nil {
		return e.ForbiddenError("root user not seeded", err)
	}

	code, err := generateOpaqueCode()
	if err != nil {
		return e.InternalServerError("issue code", err)
	}
	p.embeddedIAM.mu.Lock()
	p.embeddedIAM.codes[code] = &pendingCode{
		userID:      user.Id,
		email:       user.GetString("email"),
		name:        user.GetString("name"),
		clientID:    clientID,
		redirectURI: redirectURI,
		expires:     time.Now().Add(embeddedIAMCodeTTL),
	}
	p.embeddedIAM.evictExpiredCodesLocked()
	p.embeddedIAM.mu.Unlock()

	u, err := url.Parse(redirectURI)
	if err != nil {
		return e.BadRequestError("invalid redirect_uri", err)
	}
	qp := u.Query()
	qp.Set("code", code)
	if state != "" {
		qp.Set("state", state)
	}
	u.RawQuery = qp.Encode()

	e.Response.Header().Set("Location", u.String())
	e.Response.WriteHeader(http.StatusFound)
	return nil
}
