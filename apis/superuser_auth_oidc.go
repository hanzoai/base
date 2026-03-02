package apis

import (
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/hanzoai/base/core"
	"github.com/hanzoai/base/tools/router"
	"github.com/hanzoai/base/tools/security"
)

const (
	oidcStateCookieName    = "base_oidc_state"
	oidcVerifierCookieName = "base_oidc_verifier"
	oidcStateTTL           = 5 * time.Minute
)

// oidcConfig holds the resolved OIDC configuration from env vars.
type oidcConfig struct {
	Issuer       string
	ClientID     string
	ClientSecret string
	RedirectURL  string
	AutoCreate   bool
}

// oidcDiscovery holds the relevant fields from the OIDC discovery document.
type oidcDiscovery struct {
	AuthorizationEndpoint string `json:"authorization_endpoint"`
	TokenEndpoint         string `json:"token_endpoint"`
	UserinfoEndpoint      string `json:"userinfo_endpoint"`
}

// oidcTokenResponse holds the token endpoint response.
type oidcTokenResponse struct {
	AccessToken string `json:"access_token"`
	IDToken     string `json:"id_token"`
	TokenType   string `json:"token_type"`
}

// loadOIDCConfig reads OIDC configuration from environment variables.
// Returns nil if OIDC is not configured (no issuer set).
func loadOIDCConfig(appURL string) *oidcConfig {
	issuer := os.Getenv("BASE_OIDC_ISSUER")
	if issuer == "" {
		return nil
	}

	redirectURL := strings.TrimRight(appURL, "/") + "/_/auth/oidc/callback"

	return &oidcConfig{
		Issuer:       strings.TrimRight(issuer, "/"),
		ClientID:     os.Getenv("BASE_OIDC_CLIENT_ID"),
		ClientSecret: os.Getenv("BASE_OIDC_CLIENT_SECRET"),
		RedirectURL:  redirectURL,
		AutoCreate:   os.Getenv("BASE_OIDC_AUTO_CREATE_SUPERUSER") == "true",
	}
}

// fetchOIDCDiscovery fetches the OIDC discovery document from the issuer.
func fetchOIDCDiscovery(issuer string) (*oidcDiscovery, error) {
	resp, err := http.Get(issuer + "/.well-known/openid-configuration")
	if err != nil {
		return nil, fmt.Errorf("failed to fetch OIDC discovery: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("OIDC discovery returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("failed to read OIDC discovery response: %w", err)
	}

	var disc oidcDiscovery
	if err := json.Unmarshal(body, &disc); err != nil {
		return nil, fmt.Errorf("failed to parse OIDC discovery: %w", err)
	}

	if disc.AuthorizationEndpoint == "" || disc.TokenEndpoint == "" {
		return nil, errors.New("OIDC discovery missing required endpoints")
	}

	return &disc, nil
}

// generatePKCE generates a PKCE code verifier and its S256 challenge.
func generatePKCE() (verifier, challenge string) {
	verifier = security.RandomString(64)
	h := sha256.New()
	h.Write([]byte(verifier))
	challenge = strings.TrimRight(base64.URLEncoding.EncodeToString(h.Sum(nil)), "=")
	return
}

// bindSuperuserOIDCApi registers the OIDC superuser auth routes.
func bindSuperuserOIDCApi(app core.App, r *router.Router[*core.RequestEvent]) {
	r.GET("/_/auth/oidc/redirect", superuserOIDCRedirect(app)).Bind(
		SkipSuccessActivityLog(),
	)
	r.GET("/_/auth/oidc/callback", superuserOIDCCallback(app)).Bind(
		SkipSuccessActivityLog(),
	)
	r.GET("/_/api/oidc/config", superuserOIDCConfigHandler(app))
}

// superuserOIDCConfigHandler returns a handler that reports whether OIDC is enabled.
func superuserOIDCConfigHandler(app core.App) func(*core.RequestEvent) error {
	return func(e *core.RequestEvent) error {
		cfg := loadOIDCConfig(app.Settings().Meta.AppURL)

		result := map[string]any{
			"enabled": cfg != nil,
		}
		if cfg != nil {
			result["issuer"] = cfg.Issuer
		}

		return e.JSON(http.StatusOK, result)
	}
}

// superuserOIDCRedirect returns a handler that starts the OIDC authorization flow.
func superuserOIDCRedirect(app core.App) func(*core.RequestEvent) error {
	return func(e *core.RequestEvent) error {
		cfg := loadOIDCConfig(app.Settings().Meta.AppURL)
		if cfg == nil {
			return e.BadRequestError("OIDC is not configured.", nil)
		}

		disc, err := fetchOIDCDiscovery(cfg.Issuer)
		if err != nil {
			return e.InternalServerError("Failed to load OIDC configuration.", err)
		}

		state := security.RandomString(32)
		verifier, challenge := generatePKCE()

		// Store state and verifier in short-lived cookies.
		setOIDCCookie(e, oidcStateCookieName, state, oidcStateTTL)
		setOIDCCookie(e, oidcVerifierCookieName, verifier, oidcStateTTL)

		params := url.Values{
			"response_type":         {"code"},
			"client_id":             {cfg.ClientID},
			"redirect_uri":          {cfg.RedirectURL},
			"scope":                 {"openid email profile"},
			"state":                 {state},
			"code_challenge":        {challenge},
			"code_challenge_method": {"S256"},
		}

		return e.Redirect(http.StatusTemporaryRedirect, disc.AuthorizationEndpoint+"?"+params.Encode())
	}
}

// superuserOIDCCallback returns a handler that completes the OIDC authorization flow.
// On success it serves a small HTML page that stores the auth token in localStorage
// and redirects the browser to the admin UI.
func superuserOIDCCallback(app core.App) func(*core.RequestEvent) error {
	return func(e *core.RequestEvent) error {
		cfg := loadOIDCConfig(app.Settings().Meta.AppURL)
		if cfg == nil {
			return e.BadRequestError("OIDC is not configured.", nil)
		}

		// Validate state.
		stateCookie, err := e.Request.Cookie(oidcStateCookieName)
		if err != nil || stateCookie.Value == "" {
			return e.BadRequestError("Missing OIDC state cookie.", nil)
		}
		queryState := e.Request.URL.Query().Get("state")
		if queryState == "" || queryState != stateCookie.Value {
			return e.BadRequestError("Invalid OIDC state parameter.", nil)
		}

		// Check for error from the authorization server.
		if oidcErr := e.Request.URL.Query().Get("error"); oidcErr != "" {
			desc := e.Request.URL.Query().Get("error_description")
			return e.BadRequestError("OIDC authorization failed: "+oidcErr+" "+desc, nil)
		}

		code := e.Request.URL.Query().Get("code")
		if code == "" {
			return e.BadRequestError("Missing authorization code.", nil)
		}

		// Retrieve PKCE verifier.
		verifierCookie, err := e.Request.Cookie(oidcVerifierCookieName)
		if err != nil || verifierCookie.Value == "" {
			return e.BadRequestError("Missing OIDC verifier cookie.", nil)
		}

		// Clear cookies.
		clearOIDCCookie(e, oidcStateCookieName)
		clearOIDCCookie(e, oidcVerifierCookieName)

		// Exchange code for tokens.
		disc, err := fetchOIDCDiscovery(cfg.Issuer)
		if err != nil {
			return e.InternalServerError("Failed to load OIDC configuration.", err)
		}

		tokenResp, err := exchangeCode(disc.TokenEndpoint, code, verifierCookie.Value, cfg)
		if err != nil {
			return e.InternalServerError("Failed to exchange authorization code.", err)
		}

		// Extract email from the ID token or userinfo.
		email, err := extractEmail(tokenResp, disc, cfg.Issuer)
		if err != nil {
			return e.InternalServerError("Failed to extract email from OIDC token.", err)
		}

		if email == "" {
			return e.BadRequestError("OIDC provider did not return an email address.", nil)
		}

		// Find the superuser by email.
		superuser, err := app.FindAuthRecordByEmail(core.CollectionNameSuperusers, email)
		if err != nil {
			if !errors.Is(err, sql.ErrNoRows) {
				return e.InternalServerError("Failed to look up superuser.", err)
			}

			// No matching superuser found.
			if !cfg.AutoCreate {
				return e.ForbiddenError(
					fmt.Sprintf("No superuser account found for email %q. Contact an administrator.", email),
					nil,
				)
			}

			// Auto-create superuser.
			col, colErr := app.FindCachedCollectionByNameOrId(core.CollectionNameSuperusers)
			if colErr != nil {
				return e.InternalServerError("Failed to find superusers collection.", colErr)
			}

			superuser = core.NewRecord(col)
			superuser.SetEmail(email)
			superuser.SetRandomPassword()
			superuser.SetVerified(true)

			if saveErr := app.Save(superuser); saveErr != nil {
				return e.InternalServerError("Failed to create superuser.", saveErr)
			}

			app.Logger().Info("Auto-created superuser via OIDC", "email", email)
		}

		// Generate a Base auth token.
		token, tokenErr := superuser.NewAuthToken()
		if tokenErr != nil {
			return e.InternalServerError("Failed to create auth token.", tokenErr)
		}

		// Unhide fields for the admin record (mirrors RecordAuthResponse behavior).
		superuser.Unhide(superuser.Collection().Fields.FieldNames()...)
		superuser.IgnoreEmailVisibility(true)

		recordJSON, marshalErr := json.Marshal(superuser)
		if marshalErr != nil {
			return e.InternalServerError("Failed to serialize superuser record.", marshalErr)
		}

		// Serve an HTML page that stores the auth in localStorage and redirects.
		html := `<!DOCTYPE html>
<html>
<head><meta charset="utf-8"><title>Signing in...</title></head>
<body>
<p>Signing in...</p>
<script>
try {
    var data = JSON.stringify({
        token: ` + jsonStringLiteral(token) + `,
        record: ` + string(recordJSON) + `
    });
    localStorage.setItem("__superuser_auth__", data);
} catch(e) {
    console.error("Failed to store auth:", e);
}
window.location.replace("/_/");
</script>
</body>
</html>`

		e.Response.Header().Set("Content-Type", "text/html; charset=utf-8")
		e.Response.Header().Set("Cache-Control", "no-store")
		e.Response.WriteHeader(http.StatusOK)
		_, writeErr := e.Response.Write([]byte(html))
		return writeErr
	}
}

// jsonStringLiteral returns a JSON-encoded string literal safe for embedding in JS.
func jsonStringLiteral(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// exchangeCode exchanges the authorization code for tokens at the token endpoint.
func exchangeCode(tokenEndpoint, code, verifier string, cfg *oidcConfig) (*oidcTokenResponse, error) {
	data := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {cfg.RedirectURL},
		"client_id":     {cfg.ClientID},
		"code_verifier": {verifier},
	}

	if cfg.ClientSecret != "" {
		data.Set("client_secret", cfg.ClientSecret)
	}

	resp, err := http.PostForm(tokenEndpoint, data)
	if err != nil {
		return nil, fmt.Errorf("token request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("failed to read token response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("token endpoint returned status %d: %s", resp.StatusCode, string(body))
	}

	var tokenResp oidcTokenResponse
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return nil, fmt.Errorf("failed to parse token response: %w", err)
	}

	return &tokenResp, nil
}

// extractEmail extracts the email claim from the ID token (JWT payload) or
// falls back to the userinfo endpoint.
func extractEmail(tokenResp *oidcTokenResponse, disc *oidcDiscovery, issuer string) (string, error) {
	// Try the ID token first (decode JWT payload without signature verification
	// since we just received it directly from the token endpoint over TLS).
	if tokenResp.IDToken != "" {
		email, err := emailFromJWTPayload(tokenResp.IDToken, issuer)
		if err == nil && email != "" {
			return email, nil
		}
	}

	// Fall back to the userinfo endpoint.
	if disc.UserinfoEndpoint != "" && tokenResp.AccessToken != "" {
		email, err := emailFromUserinfo(disc.UserinfoEndpoint, tokenResp.AccessToken)
		if err == nil && email != "" {
			return email, nil
		}
	}

	return "", errors.New("no email found in OIDC response")
}

// emailFromJWTPayload extracts the email from a JWT's payload section.
// It does NOT verify the signature since we received the token directly
// from the token endpoint over HTTPS (standard OIDC code flow).
func emailFromJWTPayload(rawJWT string, expectedIssuer string) (string, error) {
	parts := strings.Split(rawJWT, ".")
	if len(parts) != 3 {
		return "", errors.New("invalid JWT format")
	}

	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return "", fmt.Errorf("failed to decode JWT payload: %w", err)
	}

	var claims struct {
		Email         string `json:"email"`
		EmailVerified any    `json:"email_verified"`
		Issuer        string `json:"iss"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return "", fmt.Errorf("failed to parse JWT claims: %w", err)
	}

	// Basic issuer check.
	if expectedIssuer != "" && claims.Issuer != expectedIssuer {
		return "", fmt.Errorf("issuer mismatch: got %q, expected %q", claims.Issuer, expectedIssuer)
	}

	return claims.Email, nil
}

// emailFromUserinfo fetches the email from the OIDC userinfo endpoint.
func emailFromUserinfo(userinfoURL, accessToken string) (string, error) {
	req, err := http.NewRequest(http.MethodGet, userinfoURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("userinfo request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("userinfo returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", fmt.Errorf("failed to read userinfo response: %w", err)
	}

	var claims struct {
		Email string `json:"email"`
	}
	if err := json.Unmarshal(body, &claims); err != nil {
		return "", fmt.Errorf("failed to parse userinfo response: %w", err)
	}

	return claims.Email, nil
}

// setOIDCCookie sets a short-lived, HTTP-only, SameSite=Lax cookie.
func setOIDCCookie(e *core.RequestEvent, name, value string, maxAge time.Duration) {
	http.SetCookie(e.Response, &http.Cookie{
		Name:     name,
		Value:    value,
		Path:     "/_/auth/oidc/",
		MaxAge:   int(maxAge.Seconds()),
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   e.Request.TLS != nil || e.Request.Header.Get("X-Forwarded-Proto") == "https",
	})
}

// clearOIDCCookie expires a cookie immediately.
func clearOIDCCookie(e *core.RequestEvent, name string) {
	http.SetCookie(e.Response, &http.Cookie{
		Name:     name,
		Value:    "",
		Path:     "/_/auth/oidc/",
		MaxAge:   -1,
		HttpOnly: true,
	})
}
