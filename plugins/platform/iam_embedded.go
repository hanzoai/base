// Package platform — IAM_MODE=embedded in-process OIDC provider.
//
// When IAM_MODE=embedded is set, Base does NOT proxy /api/iam to an
// external IAM. Instead it hosts a minimal OIDC provider in-process,
// sufficient for @hanzo/iam/browser PKCE clients to sign in, validate
// JWTs via JWKS, and read userinfo.
//
// Scope is intentionally tiny — single-tenant, single-domain. No
// federation, no MFA, no refresh tokens, no password reset. A user
// either signs in with email+password (bcrypt cost 12) or they don't.
// For everything richer (orgs, providers, MFA, audit trail), boot
// against an external Casdoor at IAM_ENDPOINT.
//
// On-disk artifacts:
//
//   - ${DataDir}/iam.key — RSA-2048 private key (PEM). Generated on
//     first boot. The public half is exposed at JWKS. Lose this and
//     all outstanding JWTs become unverifiable.
//   - _iam_users collection — email, password (bcrypt), name.
//
// Endpoints (mounted under /api/iam):
//
//   GET  /.well-known/openid-configuration
//   GET  /.well-known/jwks
//   GET  /oauth/authorize?client_id&redirect_uri&state&scope&response_type=code
//   POST /oauth/login          (form: email, password, state)
//   POST /oauth/token          (form: grant_type=authorization_code, code, redirect_uri, client_id)
//   GET  /oauth/userinfo       (Authorization: Bearer ...)
//
// Bootstrap: set EMBEDDED_IAM_ROOT_EMAIL and EMBEDDED_IAM_ROOT_PASSWORD
// on first boot to seed the root user. Subsequent boots are no-ops if
// _iam_users already has rows.

package platform

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"html/template"
	"math/big"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/hanzoai/base/core"
	"github.com/hanzoai/base/tools/router"
	"golang.org/x/crypto/bcrypt"
)

const (
	// collectionIAMUsers stores embedded-IAM identities. System collection.
	collectionIAMUsers = "_iam_users"

	// embeddedIAMMount is the URL prefix all embedded handlers live under.
	// It matches the proxy mount so @hanzo/iam/browser sees the same
	// surface regardless of mode.
	embeddedIAMMount = "/api/iam"

	// embeddedIAMKeyID identifies the in-process signing key in JWKS.
	embeddedIAMKeyID = "embedded-1"

	// embeddedIAMCodeTTL is how long an authorization_code stays valid.
	embeddedIAMCodeTTL = 5 * time.Minute

	// embeddedIAMTokenTTL is how long an issued JWT lives.
	embeddedIAMTokenTTL = 1 * time.Hour

	// embeddedIAMBcryptCost is the work factor for password hashing.
	embeddedIAMBcryptCost = 12
)

// IsEmbeddedIAM returns true when IAM_MODE=embedded. The platform
// plugin uses this to skip external endpoint validation and mount
// in-process handlers in place of the reverse proxy.
func IsEmbeddedIAM() bool {
	return strings.EqualFold(os.Getenv("IAM_MODE"), "embedded")
}

// embeddedIAM owns the in-process OIDC state: signing key, pending
// authorization codes, and the bcrypt-verified user lookup. The
// _iam_users collection holds the durable user record; this struct
// holds the transient cryptographic + code state.
type embeddedIAM struct {
	app    core.App
	signer *rsa.PrivateKey
	kid    string

	mu    sync.Mutex
	codes map[string]*pendingCode // code -> pending grant
}

// pendingCode is the short-lived authorization grant held between
// /oauth/login and /oauth/token. The client provides `state` to
// /oauth/authorize, we round-trip it through the login form, and the
// resulting code is keyed against expiry + the intended client_id.
type pendingCode struct {
	userID      string
	email       string
	name        string
	clientID    string
	redirectURI string
	expires     time.Time
}

// newEmbeddedIAM loads or generates the RSA signing key from
// ${DataDir}/iam.key and returns an initialized provider.
//
// The key is generated once on first boot and persisted as PEM. Losing
// it invalidates every outstanding JWT; backing it up is the
// operator's job (the file lives next to the SQLite database, so any
// DataDir backup covers it).
func newEmbeddedIAM(app core.App) (*embeddedIAM, error) {
	keyPath := filepath.Join(app.DataDir(), "iam.key")

	key, err := loadOrGenerateRSAKey(keyPath)
	if err != nil {
		return nil, fmt.Errorf("embedded iam: load signing key: %w", err)
	}

	return &embeddedIAM{
		app:    app,
		signer: key,
		kid:    embeddedIAMKeyID,
		codes:  make(map[string]*pendingCode),
	}, nil
}

// loadOrGenerateRSAKey reads the PEM-encoded RSA key at path, or
// generates and persists a fresh RSA-2048 key if the file is missing.
//
// Filesystem permissions are tightened to 0600 — only the daemon user
// should be able to read the signing key.
func loadOrGenerateRSAKey(path string) (*rsa.PrivateKey, error) {
	if buf, err := os.ReadFile(path); err == nil {
		block, _ := pem.Decode(buf)
		if block == nil {
			return nil, fmt.Errorf("decode pem from %s: empty or malformed", path)
		}
		key, err := x509.ParsePKCS1PrivateKey(block.Bytes)
		if err != nil {
			// PKCS#8 fallback for keys generated by external tooling.
			anyKey, err2 := x509.ParsePKCS8PrivateKey(block.Bytes)
			if err2 != nil {
				return nil, fmt.Errorf("parse rsa key (tried PKCS1 and PKCS8): %w / %w", err, err2)
			}
			rsaKey, ok := anyKey.(*rsa.PrivateKey)
			if !ok {
				return nil, fmt.Errorf("key at %s is not RSA", path)
			}
			return rsaKey, nil
		}
		return key, nil
	}

	// Generate fresh key.
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create key dir: %w", err)
	}
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, fmt.Errorf("generate rsa key: %w", err)
	}
	pemBytes := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(key),
	})
	if err := os.WriteFile(path, pemBytes, 0o600); err != nil {
		return nil, fmt.Errorf("persist rsa key: %w", err)
	}
	return key, nil
}

// ensureUsersCollection creates the _iam_users system collection if
// it does not already exist. Idempotent.
func (p *plugin) ensureUsersCollection() error {
	if _, err := p.app.FindCollectionByNameOrId(collectionIAMUsers); err == nil {
		return nil
	}

	c := core.NewBaseCollection(collectionIAMUsers)
	c.System = true
	c.Fields.Add(
		&core.TextField{Name: "email", Required: true, Min: 3, Max: 200},
		&core.TextField{Name: "password", Required: true, Min: 1, Max: 200}, // bcrypt hash, not plaintext
		&core.TextField{Name: "name", Required: false, Max: 200},
		&core.AutodateField{Name: "created", OnCreate: true},
		&core.AutodateField{Name: "updated", OnCreate: true, OnUpdate: true},
	)

	p.app.Logger().Info("creating embedded IAM collection", "name", collectionIAMUsers)
	return p.app.Save(c)
}

// bootstrapRootUser seeds the root user from EMBEDDED_IAM_ROOT_EMAIL +
// EMBEDDED_IAM_ROOT_PASSWORD on first boot. Idempotent: if _iam_users
// has any rows, this is a no-op.
func (p *plugin) bootstrapRootUser() error {
	email := strings.TrimSpace(os.Getenv("EMBEDDED_IAM_ROOT_EMAIL"))
	password := os.Getenv("EMBEDDED_IAM_ROOT_PASSWORD")
	if email == "" || password == "" {
		return nil // operator chose CLI path or already seeded
	}

	existing, _ := p.app.FindRecordsByFilter(collectionIAMUsers, "id != ''", "", 1, 0, nil)
	if len(existing) > 0 {
		return nil
	}

	if _, err := p.createIAMUser(email, password, "Root"); err != nil {
		return fmt.Errorf("bootstrap root user %s: %w", email, err)
	}
	p.app.Logger().Info("embedded iam: bootstrapped root user", "email", email)
	return nil
}

// createIAMUser inserts a user with a bcrypt-hashed password into the
// _iam_users collection. Used by bootstrap and the CLI subcommand.
func (p *plugin) createIAMUser(email, password, name string) (*core.Record, error) {
	email = strings.TrimSpace(strings.ToLower(email))
	if email == "" || password == "" {
		return nil, fmt.Errorf("email and password are required")
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), embeddedIAMBcryptCost)
	if err != nil {
		return nil, fmt.Errorf("hash password: %w", err)
	}

	col, err := p.app.FindCollectionByNameOrId(collectionIAMUsers)
	if err != nil {
		return nil, fmt.Errorf("collection %s not found: %w", collectionIAMUsers, err)
	}

	rec := core.NewRecord(col)
	rec.Set("email", email)
	rec.Set("password", string(hash))
	rec.Set("name", name)
	if err := p.app.Save(rec); err != nil {
		return nil, fmt.Errorf("save user: %w", err)
	}
	return rec, nil
}

// authenticateIAMUser verifies email+password against _iam_users and
// returns the matching record. Errors are intentionally generic so we
// don't leak which half (email vs password) was wrong.
func (p *plugin) authenticateIAMUser(email, password string) (*core.Record, error) {
	email = strings.TrimSpace(strings.ToLower(email))
	rec, err := p.app.FindFirstRecordByData(collectionIAMUsers, "email", email)
	if err != nil || rec == nil {
		return nil, fmt.Errorf("invalid credentials")
	}
	hash := rec.GetString("password")
	if bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) != nil {
		return nil, fmt.Errorf("invalid credentials")
	}
	return rec, nil
}

// --------------------------------------------------------------------------
// Route registration
// --------------------------------------------------------------------------

// registerEmbeddedIAM mounts the in-process OIDC endpoints. Called by
// platform.Register when IAM_MODE=embedded. It replaces (not augments)
// the reverse-proxy mount; clients see /api/iam/* either way.
func (p *plugin) registerEmbeddedIAM(r *router.Router[*core.RequestEvent]) {
	if p.embeddedIAM == nil {
		return
	}

	r.GET(embeddedIAMMount+"/.well-known/openid-configuration", p.handleEmbeddedDiscovery)
	r.GET(embeddedIAMMount+"/.well-known/jwks", p.handleEmbeddedJWKS)
	r.GET(embeddedIAMMount+"/oauth/authorize", p.handleEmbeddedAuthorize)
	r.POST(embeddedIAMMount+"/oauth/login", p.handleEmbeddedLogin)
	r.POST(embeddedIAMMount+"/oauth/token", p.handleEmbeddedToken)
	r.GET(embeddedIAMMount+"/oauth/userinfo", p.handleEmbeddedUserinfo)
}

// requestOrigin returns the scheme://host the embedded IAM should
// advertise in OIDC discovery + JWT iss. Honors X-Forwarded-Proto so
// the issuer matches the public URL behind an ingress.
func requestOrigin(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if v := r.Header.Get("X-Forwarded-Proto"); v != "" {
		scheme = v
	}
	return scheme + "://" + r.Host
}

// --------------------------------------------------------------------------
// Discovery + JWKS
// --------------------------------------------------------------------------

func (p *plugin) handleEmbeddedDiscovery(e *core.RequestEvent) error {
	origin := requestOrigin(e.Request) + embeddedIAMMount
	return e.JSON(http.StatusOK, map[string]any{
		"issuer":                                origin,
		"authorization_endpoint":                origin + "/oauth/authorize",
		"token_endpoint":                        origin + "/oauth/token",
		"userinfo_endpoint":                     origin + "/oauth/userinfo",
		"jwks_uri":                              origin + "/.well-known/jwks",
		"response_types_supported":              []string{"code"},
		"grant_types_supported":                 []string{"authorization_code"},
		"id_token_signing_alg_values_supported": []string{"RS256"},
		"subject_types_supported":               []string{"public"},
		"scopes_supported":                      []string{"openid", "email", "profile"},
		"token_endpoint_auth_methods_supported": []string{"client_secret_post", "none"},
	})
}

func (p *plugin) handleEmbeddedJWKS(e *core.RequestEvent) error {
	pub := &p.embeddedIAM.signer.PublicKey
	jwk := map[string]any{
		"kty": "RSA",
		"kid": p.embeddedIAM.kid,
		"alg": "RS256",
		"use": "sig",
		"n":   base64.RawURLEncoding.EncodeToString(pub.N.Bytes()),
		"e":   base64.RawURLEncoding.EncodeToString(big.NewInt(int64(pub.E)).Bytes()),
	}
	return e.JSON(http.StatusOK, map[string]any{
		"keys": []map[string]any{jwk},
	})
}

// --------------------------------------------------------------------------
// Authorize + login
// --------------------------------------------------------------------------

// authorizeForm is the minimal login form rendered by /oauth/authorize.
// Plain HTML, no JS, no CSS framework — the embedded mode is meant for
// dev/single-tenant and the consuming UI is whatever the client renders
// after the redirect.
var authorizeForm = template.Must(template.New("login").Parse(`<!doctype html>
<html><head><meta charset="utf-8"><title>Sign in</title></head>
<body style="font-family: system-ui; max-width: 360px; margin: 60px auto;">
<h1 style="font-size: 1.25rem;">Sign in</h1>
<form method="POST" action="/api/iam/oauth/login">
  <input type="hidden" name="client_id" value="{{.ClientID}}">
  <input type="hidden" name="redirect_uri" value="{{.RedirectURI}}">
  <input type="hidden" name="state" value="{{.State}}">
  <label style="display:block;margin:8px 0">Email
    <input type="email" name="email" required autocomplete="username"
      style="width:100%;padding:8px;box-sizing:border-box">
  </label>
  <label style="display:block;margin:8px 0">Password
    <input type="password" name="password" required autocomplete="current-password"
      style="width:100%;padding:8px;box-sizing:border-box">
  </label>
  <button type="submit" style="width:100%;padding:10px;margin-top:8px">Sign in</button>
</form>
</body></html>`))

func (p *plugin) handleEmbeddedAuthorize(e *core.RequestEvent) error {
	q := e.Request.URL.Query()
	clientID := q.Get("client_id")
	redirectURI := q.Get("redirect_uri")
	state := q.Get("state")
	responseType := q.Get("response_type")

	if responseType != "" && responseType != "code" {
		return e.BadRequestError("unsupported response_type", nil)
	}
	if clientID == "" || redirectURI == "" {
		return e.BadRequestError("client_id and redirect_uri are required", nil)
	}

	e.Response.Header().Set("Content-Type", "text/html; charset=utf-8")
	return authorizeForm.Execute(e.Response, map[string]string{
		"ClientID":    template.HTMLEscapeString(clientID),
		"RedirectURI": template.HTMLEscapeString(redirectURI),
		"State":       template.HTMLEscapeString(state),
	})
}

func (p *plugin) handleEmbeddedLogin(e *core.RequestEvent) error {
	if err := e.Request.ParseForm(); err != nil {
		return e.BadRequestError("invalid form", err)
	}
	email := strings.TrimSpace(e.Request.FormValue("email"))
	password := e.Request.FormValue("password")
	clientID := e.Request.FormValue("client_id")
	redirectURI := e.Request.FormValue("redirect_uri")
	state := e.Request.FormValue("state")

	if clientID == "" || redirectURI == "" {
		return e.BadRequestError("client_id and redirect_uri are required", nil)
	}

	user, err := p.authenticateIAMUser(email, password)
	if err != nil {
		return e.Error(http.StatusUnauthorized, "invalid credentials", nil)
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
	q := u.Query()
	q.Set("code", code)
	if state != "" {
		q.Set("state", state)
	}
	u.RawQuery = q.Encode()

	e.Response.Header().Set("Location", u.String())
	e.Response.WriteHeader(http.StatusFound)
	return nil
}

// evictExpiredCodesLocked removes expired entries. Caller must hold mu.
func (e *embeddedIAM) evictExpiredCodesLocked() {
	now := time.Now()
	for c, pc := range e.codes {
		if now.After(pc.expires) {
			delete(e.codes, c)
		}
	}
}

// generateOpaqueCode returns a 32-byte URL-safe random string.
func generateOpaqueCode() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// --------------------------------------------------------------------------
// Token + userinfo
// --------------------------------------------------------------------------

func (p *plugin) handleEmbeddedToken(e *core.RequestEvent) error {
	if err := e.Request.ParseForm(); err != nil {
		return e.BadRequestError("invalid form", err)
	}
	grantType := e.Request.FormValue("grant_type")
	code := e.Request.FormValue("code")
	clientID := e.Request.FormValue("client_id")
	redirectURI := e.Request.FormValue("redirect_uri")

	if grantType != "authorization_code" {
		return e.BadRequestError("unsupported grant_type", nil)
	}
	if code == "" {
		return e.BadRequestError("code is required", nil)
	}

	p.embeddedIAM.mu.Lock()
	pc, ok := p.embeddedIAM.codes[code]
	if ok {
		delete(p.embeddedIAM.codes, code) // single-use
	}
	p.embeddedIAM.mu.Unlock()

	if !ok || time.Now().After(pc.expires) {
		return e.Error(http.StatusBadRequest, "invalid or expired code", nil)
	}
	if clientID != "" && clientID != pc.clientID {
		return e.Error(http.StatusBadRequest, "client_id mismatch", nil)
	}
	if redirectURI != "" && redirectURI != pc.redirectURI {
		return e.Error(http.StatusBadRequest, "redirect_uri mismatch", nil)
	}

	now := time.Now()
	origin := requestOrigin(e.Request) + embeddedIAMMount

	token := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{
		"iss":   origin,
		"sub":   pc.userID,
		"aud":   pc.clientID,
		"email": pc.email,
		"name":  pc.name,
		"iat":   now.Unix(),
		"exp":   now.Add(embeddedIAMTokenTTL).Unix(),
	})
	token.Header["kid"] = p.embeddedIAM.kid

	signed, err := token.SignedString(p.embeddedIAM.signer)
	if err != nil {
		return e.InternalServerError("sign token", err)
	}

	return e.JSON(http.StatusOK, map[string]any{
		"access_token": signed,
		"id_token":     signed,
		"token_type":   "Bearer",
		"expires_in":   int(embeddedIAMTokenTTL / time.Second),
		"scope":        "openid email profile",
	})
}

func (p *plugin) handleEmbeddedUserinfo(e *core.RequestEvent) error {
	auth := e.Request.Header.Get("Authorization")
	if !strings.HasPrefix(auth, "Bearer ") {
		return e.UnauthorizedError("missing bearer token", nil)
	}
	rawToken := strings.TrimPrefix(auth, "Bearer ")

	parsed, err := jwt.Parse(rawToken, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodRSA); !ok {
			return nil, fmt.Errorf("unexpected alg %v", t.Header["alg"])
		}
		return &p.embeddedIAM.signer.PublicKey, nil
	}, jwt.WithValidMethods([]string{"RS256"}))
	if err != nil || !parsed.Valid {
		return e.UnauthorizedError("invalid token", err)
	}

	claims, ok := parsed.Claims.(jwt.MapClaims)
	if !ok {
		return e.UnauthorizedError("invalid claims", nil)
	}

	sub, _ := claims["sub"].(string)
	if sub == "" {
		return e.UnauthorizedError("missing sub", nil)
	}

	rec, err := p.app.FindRecordById(collectionIAMUsers, sub)
	if err != nil || rec == nil {
		return e.UnauthorizedError("user not found", err)
	}

	return e.JSON(http.StatusOK, map[string]any{
		"id":             rec.Id,
		"sub":            rec.Id,
		"email":          rec.GetString("email"),
		"name":           rec.GetString("name"),
		"email_verified": true,
	})
}

// --------------------------------------------------------------------------
// Auth middleware
// --------------------------------------------------------------------------

// embeddedIAMAuthMiddleware validates the Authorization bearer token
// against the in-process RSA signer. On success, it populates
// e.Auth with an ephemeral record from _iam_users so downstream
// middleware sees the standard IAM-authenticated request shape.
//
// Runs BEFORE the default loadAuthToken middleware. If the token
// isn't a valid embedded-IAM JWT (e.g. an API key, a superuser
// token, or simply missing), the middleware no-ops and lets the
// regular auth pipeline run.
func (p *plugin) embeddedIAMAuthMiddleware(e *core.RequestEvent) error {
	if p.embeddedIAM == nil || e.Auth != nil {
		return e.Next()
	}

	auth := e.Request.Header.Get("Authorization")
	if !strings.HasPrefix(auth, "Bearer ") {
		return e.Next()
	}
	raw := strings.TrimPrefix(auth, "Bearer ")
	if raw == "" {
		return e.Next()
	}

	parsed, err := jwt.Parse(raw, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodRSA); !ok {
			return nil, fmt.Errorf("unexpected alg %v", t.Header["alg"])
		}
		return &p.embeddedIAM.signer.PublicKey, nil
	}, jwt.WithValidMethods([]string{"RS256"}))
	if err != nil || !parsed.Valid {
		// Not our token (e.g. an opaque API key). Let other middleware
		// take a turn.
		return e.Next()
	}

	claims, ok := parsed.Claims.(jwt.MapClaims)
	if !ok {
		return e.Next()
	}
	sub, _ := claims["sub"].(string)
	if sub == "" {
		return e.Next()
	}
	rec, err := p.app.FindRecordById(collectionIAMUsers, sub)
	if err != nil || rec == nil {
		return e.Next()
	}

	email, _ := claims["email"].(string)
	name, _ := claims["name"].(string)

	e.Set("authSub", rec.Id)
	e.Set("authEmail", email)
	e.Set("authName", name)
	e.Auth = rec
	return e.Next()
}

// iamModeLabel returns a human-readable IAM mode for log lines.
func iamModeLabel(embedded bool) string {
	if embedded {
		return "embedded"
	}
	return "external"
}
