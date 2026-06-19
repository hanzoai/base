package bootnode

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/hanzoai/base/core"
	"github.com/hanzoai/base/plugins/bootnode/auth"
)

// handleOAuthCallback exchanges an OAuth2 authorization code for an access
// token, deriving the per-network IAM client id from the redirect_uri. Ports
// POST /oauth/callback from bootnode/api/auth/oauth.py.
//
// The frontend MUST send the redirect_uri it used so the backend can pick the
// matching client id (all cloud networks share the lux-web3 IAM app with
// per-network redirect URIs registered).
func (p *plugin) handleOAuthCallback(e *core.RequestEvent) error {
	var body struct {
		Code        string `json:"code"`
		State       string `json:"state"`
		RedirectURI string `json:"redirect_uri"`
	}
	if err := e.BindBody(&body); err != nil {
		return e.BadRequestError("invalid request body", err)
	}
	if body.Code == "" {
		return e.BadRequestError("code is required", nil)
	}

	redirectURI := body.RedirectURI
	if redirectURI == "" {
		redirectURI = strings.TrimRight(p.config.FrontendURL, "/") + "/auth/callback"
	}
	clientID := auth.ClientIDForRedirect(redirectURI, p.config.IAMClientID)

	token, expiresIn, err := p.exchangeCode(e.Request.Context(), body.Code, redirectURI, clientID)
	if err != nil {
		return e.BadRequestError("failed to exchange authorization code for token", err)
	}

	// Verify the token and enforce the org allow-list before returning it.
	user, err := p.iam.ValidateToken(token)
	if err != nil {
		return e.UnauthorizedError("token verification failed", err)
	}
	if len(user.OrgIDs) > 0 && !p.config.orgAllowed(user.OrgIDs[0]) {
		return e.ForbiddenError("organization '"+user.OrgIDs[0]+"' is not allowed", nil)
	}

	return e.JSON(http.StatusOK, map[string]any{
		"access_token": token,
		"token_type":   "Bearer",
		"expires_in":   expiresIn,
	})
}

// handleMe returns the authenticated IAM user. Ports GET /me.
func (p *plugin) handleMe(e *core.RequestEvent) error {
	id, err := p.requireUser(e)
	if err != nil {
		return err
	}
	return e.JSON(http.StatusOK, map[string]any{
		"id":    id.UserID,
		"email": id.Email,
		"org":   id.Org,
	})
}

// exchangeCode performs the OAuth2 authorization-code → token exchange against
// IAM with the given (per-network) client id and the bootnode service's client
// secret. Kept here rather than in the iam package because the client id is
// per-request (multi-network), not a fixed service credential.
func (p *plugin) exchangeCode(ctx context.Context, code, redirectURI, clientID string) (token string, expiresIn int, err error) {
	endpoint := strings.TrimRight(p.config.IAMEndpoint, "/") + "/oauth/token"
	form := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {redirectURI},
		"client_id":     {clientID},
		"client_secret": {p.config.IAMClientSecret},
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return "", 0, fmt.Errorf("build token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", 0, fmt.Errorf("token request failed: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return "", 0, fmt.Errorf("IAM token endpoint returned %d", resp.StatusCode)
	}

	var out struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", 0, fmt.Errorf("decode token response: %w", err)
	}
	if out.AccessToken == "" {
		return "", 0, fmt.Errorf("token response missing access_token")
	}
	if out.ExpiresIn == 0 {
		out.ExpiresIn = 3600
	}
	return out.AccessToken, out.ExpiresIn, nil
}
