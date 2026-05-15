// Package platform — IAM_MODE=embedded in-process OIDC provider.
//
// When IAM_MODE=embedded is set, Base does NOT proxy /v1/iam to an
// external IAM. Instead it hosts a minimal OIDC provider in-process,
// sufficient for @hanzo/iam/browser PKCE clients to sign in, validate
// JWTs via JWKS, and read userinfo.
//
// Scope is intentionally tiny — single-tenant, single-domain. No
// federation, no MFA, no refresh tokens, no password reset. A user
// either signs in with email+password (bcrypt cost 12) or they don't.
// For everything richer (orgs, providers, MFA, audit trail), boot
// against an external Hanzo IAM at IAM_ENDPOINT.
//
// On-disk artifacts:
//
//   - ${DataDir}/iam.key — RSA-2048 private key (PEM). Generated on
//     first boot. The public half is exposed at JWKS. Lose this and
//     all outstanding JWTs become unverifiable.
//   - _iam_users collection — email, password (bcrypt), name.
//
// Endpoints (mounted under /v1/iam):
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
	"encoding/json"
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
	// surface regardless of mode. One path: /v1/iam. Not Casdoor's /api.
	embeddedIAMMount = "/v1/iam"

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
	return EnsureIAMUsersCollection(p.app)
}

// EnsureIAMUsersCollection creates the _iam_users system collection if
// missing. Idempotent; safe to call from CLI subcommands that boot
// the app without going through the platform OnBootstrap path.
func EnsureIAMUsersCollection(app core.App) error {
	if _, err := app.FindCollectionByNameOrId(collectionIAMUsers); err == nil {
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

	app.Logger().Info("creating embedded IAM collection", "name", collectionIAMUsers)
	return app.Save(c)
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

	// Mirror the root identity into _superusers so admin endpoints
	// (collections, settings, logs, backups) authorize after the OIDC
	// round-trip. The collection has no password field in IAM-native
	// mode — IAM owns the credential. Email is the only join key.
	// Idempotent: skip if a row already exists.
	if su, _ := p.app.FindAuthRecordByEmail(core.CollectionNameSuperusers, email); su == nil {
		col, err := p.app.FindCollectionByNameOrId(core.CollectionNameSuperusers)
		if err != nil {
			return fmt.Errorf("bootstrap root superuser: find _superusers: %w", err)
		}
		su := core.NewRecord(col)
		su.SetEmail(email)
		if err := p.app.Save(su); err != nil {
			return fmt.Errorf("bootstrap root superuser: save: %w", err)
		}
	}

	p.app.Logger().Info("embedded iam: bootstrapped root user + superuser", "email", email)
	return nil
}

// createIAMUser inserts a user with a bcrypt-hashed password into the
// _iam_users collection. Used by bootstrap and the CLI subcommand.
func (p *plugin) createIAMUser(email, password, name string) (*core.Record, error) {
	return CreateEmbeddedIAMUser(p.app, email, password, name)
}

// CreateEmbeddedIAMUser is the public entry point that the iam-user
// CLI subcommand uses to seed a user against the _iam_users
// collection. The collection must already exist (it does once the
// platform plugin has bootstrapped against IAM_MODE=embedded). The
// password is bcrypted at cost 12 before persisting; the plaintext
// never reaches disk.
func CreateEmbeddedIAMUser(app core.App, email, password, name string) (*core.Record, error) {
	email = strings.TrimSpace(strings.ToLower(email))
	if email == "" || password == "" {
		return nil, fmt.Errorf("email and password are required")
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), embeddedIAMBcryptCost)
	if err != nil {
		return nil, fmt.Errorf("hash password: %w", err)
	}

	col, err := app.FindCollectionByNameOrId(collectionIAMUsers)
	if err != nil {
		return nil, fmt.Errorf("collection %s not found (is IAM_MODE=embedded?): %w", collectionIAMUsers, err)
	}

	rec := core.NewRecord(col)
	rec.Set("email", email)
	rec.Set("password", string(hash))
	rec.Set("name", name)
	if err := app.Save(rec); err != nil {
		return nil, fmt.Errorf("save user: %w", err)
	}
	return rec, nil
}

// authenticateIAMUser verifies email+password against _iam_users and
// returns the matching record. Errors are intentionally generic so we
// don't leak which half (email vs password) was wrong.
func (p *plugin) authenticateIAMUser(email, password string) (*core.Record, error) {
	return AuthenticateEmbeddedIAMUser(p.app, email, password)
}

// AuthenticateEmbeddedIAMUser is the public verifier used by the OIDC
// /oauth/login handler AND by tests. It looks up the user by email
// (case-insensitive), then bcrypt-compares the supplied password
// against the stored hash. Returns a generic "invalid credentials"
// error on any failure to avoid leaking which half was wrong.
func AuthenticateEmbeddedIAMUser(app core.App, email, password string) (*core.Record, error) {
	email = strings.TrimSpace(strings.ToLower(email))
	rec, err := app.FindFirstRecordByData(collectionIAMUsers, "email", email)
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
// the reverse-proxy mount; clients see /v1/iam/* either way.
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

	// Social (Google / GitHub / Apple) + wallet (SIWE). Each provider
	// is opt-in via env, so this is a no-op if nothing is configured.
	p.registerEmbeddedSocialRoutes(r)
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

// authorizeForm renders the embedded IAM login page. Self-contained:
// no JS, no external fonts, no CSS framework. Dark theme + canonical
// block-H mark + Hanzo Red CTA. Brand name + accent are env-driven
// (BRAND_NAME / BRAND_ACCENT) so white-label deployments override
// without touching the binary. Error state re-renders this template
// with `Error` set so failed credentials don't drop to a generic
// JSON 401.
var authorizeForm = template.Must(template.New("login").Parse(authorizeFormHTML))

// Monochrome. Black + white only — no accent colour. The Hanzo brand
// is brand-neutral so white-label deployments can drop in their own
// mark/name without restyling. The only knobs are BRAND_NAME and
// BRAND_SUBTITLE; everything visual is grayscale.
const authorizeFormHTML = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Sign in to {{.BrandName}}</title>
<style>
*{box-sizing:border-box}
html,body{margin:0;padding:0;height:100%}
body{
  background:#000;color:#f5f5f5;
  font-family:-apple-system,BlinkMacSystemFont,"Segoe UI",Roboto,Oxygen,Ubuntu,Cantarell,"Helvetica Neue",sans-serif;
  -webkit-font-smoothing:antialiased;
  display:flex;align-items:center;justify-content:center;
  padding:24px;
}
.card{
  width:100%;max-width:380px;background:#0a0a0a;border:1px solid #262626;
  border-radius:16px;padding:32px 28px 28px;
  box-shadow:0 24px 48px -20px rgba(0,0,0,.7),0 2px 6px rgba(0,0,0,.5);
}
.brand{display:flex;align-items:center;gap:12px;margin-bottom:24px}
.brand .mark{
  width:36px;height:36px;background:#000;border-radius:8px;
  display:flex;align-items:center;justify-content:center;flex-shrink:0;
  border:1px solid #262626;
}
.brand .mark svg{width:26px;height:26px}
.brand .title{font-size:14px;font-weight:600;letter-spacing:-.01em;color:#f5f5f5;line-height:1.2}
.brand .sub{font-size:10px;letter-spacing:.12em;text-transform:uppercase;color:#737373;margin-top:2px}
h1{font-size:22px;font-weight:600;letter-spacing:-.02em;margin:0 0 6px;color:#f5f5f5}
.hint{font-size:13px;color:#a3a3a3;margin:0 0 24px;line-height:1.5}
.providers{display:flex;flex-direction:column;gap:8px;margin-bottom:18px}
.providers a, .providers button{
  display:flex;align-items:center;justify-content:center;gap:10px;
  width:100%;padding:10px 14px;font-size:13px;font-weight:500;
  background:#0a0a0a;color:#f5f5f5;border:1px solid #262626;border-radius:8px;
  text-decoration:none;cursor:pointer;font-family:inherit;
  transition:background .12s,border-color .12s;
}
.providers a:hover,.providers button:hover{background:#1a1a1a;border-color:#404040}
.providers svg{width:16px;height:16px;flex-shrink:0}
.divider{
  display:flex;align-items:center;gap:10px;margin:18px 0;
  font-size:11px;color:#737373;text-transform:uppercase;letter-spacing:.1em;
}
.divider:before,.divider:after{content:"";flex:1;height:1px;background:#262626}
.field{margin-bottom:14px}
.field label{display:block;font-size:12px;font-weight:500;color:#d4d4d4;margin-bottom:6px}
.field input{
  width:100%;padding:10px 12px;font-size:14px;line-height:1.4;
  background:#000;color:#f5f5f5;border:1px solid #262626;border-radius:8px;
  outline:none;transition:border-color .12s,box-shadow .12s;
  font-family:inherit;
}
.field input:focus{border-color:#737373;box-shadow:0 0 0 3px rgba(255,255,255,.06)}
.field input::placeholder{color:#525252}
button[type=submit]{
  width:100%;margin-top:8px;padding:11px 16px;font-size:14px;font-weight:600;
  background:#f5f5f5;color:#000;border:0;border-radius:8px;cursor:pointer;
  transition:background .12s,transform .04s;
  font-family:inherit;
}
button[type=submit]:hover{background:#ffffff}
button[type=submit]:active{transform:translateY(1px);background:#e5e5e5}
.error{
  margin-bottom:16px;padding:10px 12px;font-size:13px;
  background:#1a1a1a;color:#f5f5f5;
  border:1px solid #404040;border-radius:8px;
}
</style>
</head>
<body>
<div class="card">
  <div class="brand">
    <span class="mark" aria-hidden="true">
      <svg viewBox="0 0 67 67" fill="#fff" aria-hidden="true">
        <path d="M22.21 67V44.6369H0V67H22.21Z"/>
        <path d="M66.7038 22.3184H22.2534L0.0878906 44.6367H44.4634L66.7038 22.3184Z"/>
        <path d="M22.21 0H0V22.3184H22.21V0Z"/>
        <path d="M66.7198 0H44.5098V22.3184H66.7198V0Z"/>
        <path d="M66.7198 67V44.6369H44.5098V67H66.7198Z"/>
      </svg>
    </span>
    <div>
      <div class="title">{{.BrandName}}</div>
      <div class="sub">{{.BrandSubtitle}}</div>
    </div>
  </div>

  <h1>Sign in</h1>
  <p class="hint">Continue with your {{.BrandName}} account.</p>

  {{if .Error}}<div class="error" role="alert">{{.Error}}</div>{{end}}

  {{if or .Providers .WalletEnabled}}
  <div class="providers">
    {{range .Providers}}
    <a href="/v1/iam/oauth/social/{{.Key}}?client_id={{$.ClientID}}&redirect_uri={{$.RedirectURI}}&state={{$.State}}">
      {{if eq .Key "google"}}<svg viewBox="0 0 24 24" aria-hidden="true"><path fill="#fff" d="M21.35 11.1h-9.17v2.93h5.27c-.23 1.24-1.6 3.64-5.27 3.64-3.17 0-5.76-2.62-5.76-5.86s2.59-5.86 5.76-5.86c1.8 0 3.01.77 3.7 1.43l2.53-2.43C16.74 3.36 14.7 2.4 12.18 2.4 6.96 2.4 2.78 6.58 2.78 11.8s4.18 9.4 9.4 9.4c5.43 0 9.02-3.81 9.02-9.18 0-.62-.07-1.09-.17-1.55l-7.85-.37z"/></svg>{{end}}
      {{if eq .Key "github"}}<svg viewBox="0 0 24 24" aria-hidden="true"><path fill="#fff" d="M12 2C6.48 2 2 6.48 2 12c0 4.42 2.87 8.17 6.84 9.5.5.09.68-.22.68-.48v-1.69c-2.78.6-3.37-1.34-3.37-1.34-.46-1.16-1.11-1.47-1.11-1.47-.91-.62.07-.6.07-.6 1 .07 1.53 1.03 1.53 1.03.87 1.52 2.34 1.07 2.91.83.09-.65.35-1.09.63-1.34-2.22-.25-4.55-1.11-4.55-4.94 0-1.1.39-1.99 1.03-2.69-.1-.25-.45-1.27.1-2.65 0 0 .84-.27 2.75 1.02.79-.22 1.65-.33 2.5-.33s1.71.11 2.5.33c1.91-1.29 2.75-1.02 2.75-1.02.55 1.38.2 2.4.1 2.65.64.7 1.03 1.59 1.03 2.69 0 3.84-2.34 4.68-4.57 4.93.36.31.69.92.69 1.85V21c0 .27.18.58.69.48A10.02 10.02 0 0022 12c0-5.52-4.48-10-10-10z"/></svg>{{end}}
      {{if eq .Key "apple"}}<svg viewBox="0 0 24 24" aria-hidden="true"><path fill="#fff" d="M17.05 20.28c-.98.95-2.05.8-3.08.35-1.09-.46-2.09-.48-3.24 0-1.44.62-2.2.44-3.06-.35C2.79 15.25 3.51 7.59 9.05 7.31c1.35.07 2.29.74 3.08.8 1.18-.24 2.31-.93 3.57-.84 1.51.12 2.65.72 3.4 1.8-3.12 1.87-2.38 5.98.48 7.13-.57 1.5-1.31 2.99-2.54 4.09zM12.03 7.25c-.15-2.23 1.66-4.07 3.74-4.25.29 2.58-2.34 4.5-3.74 4.25z"/></svg>{{end}}
      Continue with {{.Name}}
    </a>
    {{end}}
    {{if .WalletEnabled}}
    <button type="button" id="connect-wallet">
      <svg viewBox="0 0 24 24" aria-hidden="true"><path fill="#fff" d="M19 7H5c-1.1 0-2 .9-2 2v8c0 1.1.9 2 2 2h14c1.1 0 2-.9 2-2V9c0-1.1-.9-2-2-2zm0 10H5V9h14v8zm-3-4c0-.83.67-1.5 1.5-1.5S19 12.17 19 13s-.67 1.5-1.5 1.5S16 13.83 16 13zM3 6V5c0-1.1.9-2 2-2h12v2H5v1H3z"/></svg>
      Connect Wallet
    </button>
    {{end}}
  </div>
  <div class="divider">or continue with email</div>
  {{end}}

  <form method="POST" action="/v1/iam/oauth/login" novalidate>
    <input type="hidden" name="client_id" value="{{.ClientID}}">
    <input type="hidden" name="redirect_uri" value="{{.RedirectURI}}">
    <input type="hidden" name="state" value="{{.State}}">
    <div class="field">
      <label for="email">Email</label>
      <input id="email" type="email" name="email" required autocomplete="username"
             placeholder="you@example.com" autofocus value="{{.Email}}">
    </div>
    <div class="field">
      <label for="password">Password</label>
      <input id="password" type="password" name="password" required autocomplete="current-password">
    </div>
    <button type="submit">Sign in</button>
  </form>
</div>

{{if .WalletEnabled}}
<script>
document.getElementById('connect-wallet').addEventListener('click', async () => {
  if (!window.ethereum) { alert('No browser wallet detected. Install MetaMask or similar.'); return }
  try {
    const accounts = await window.ethereum.request({ method: 'eth_requestAccounts' })
    const address = accounts[0]
    const challResp = await fetch('/v1/iam/oauth/wallet/challenge', {
      method: 'POST', headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ address, client_id: {{.ClientIDJSON}}, redirect_uri: {{.RedirectURIJSON}}, state: {{.StateJSON}} })
    })
    if (!challResp.ok) { alert('Wallet challenge failed: ' + (await challResp.text())); return }
    const { nonce, message } = await challResp.json()
    const signature = await window.ethereum.request({ method: 'personal_sign', params: [message, address] })
    const verifyResp = await fetch('/v1/iam/oauth/wallet/verify', {
      method: 'POST', headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ address, message, signature, nonce })
    })
    if (!verifyResp.ok) { alert('Verify failed: ' + (await verifyResp.text())); return }
    const { redirect } = await verifyResp.json()
    window.location.href = redirect
  } catch (e) { alert(String(e && e.message || e)) }
})
</script>
{{end}}
</body>
</html>`

// brandFromEnv returns the brand name + subtitle for the embedded
// login template. Defaults match Hanzo. Override at deploy via
// BRAND_NAME / BRAND_SUBTITLE. The visual surface is intentionally
// monochrome — no accent colour — so white-label deployments don't
// need to restyle.
func brandFromEnv() (name, subtitle string) {
	name = os.Getenv("BRAND_NAME")
	if name == "" {
		name = "Hanzo"
	}
	subtitle = os.Getenv("BRAND_SUBTITLE")
	if subtitle == "" {
		subtitle = "Identity"
	}
	return
}

// providerView is the per-button data the login template iterates over.
type providerView struct {
	Key  string
	Name string
}

// authorizeView is the full data model for the login template.
type authorizeView struct {
	ClientID        string
	RedirectURI     string
	State           string
	Email           string
	Error           string
	BrandName       string
	BrandSubtitle   string
	Providers       []providerView
	WalletEnabled   bool
	ClientIDJSON    template.JS
	RedirectURIJSON template.JS
	StateJSON       template.JS
}

func jsString(s string) template.JS {
	b, _ := json.Marshal(s)
	return template.JS(b)
}

func (p *plugin) renderAuthorize(e *core.RequestEvent, clientID, redirectURI, state, email, errMsg string, status int) error {
	name, subtitle := brandFromEnv()

	var provs []providerView
	for _, pr := range enabledSocialProviders() {
		provs = append(provs, providerView{Key: pr.Key, Name: pr.Name})
	}

	view := authorizeView{
		ClientID:        clientID,
		RedirectURI:     redirectURI,
		State:           state,
		Email:           email,
		Error:           errMsg,
		BrandName:       name,
		BrandSubtitle:   subtitle,
		Providers:       provs,
		WalletEnabled:   walletLoginEnabled(),
		ClientIDJSON:    jsString(clientID),
		RedirectURIJSON: jsString(redirectURI),
		StateJSON:       jsString(state),
	}

	e.Response.Header().Set("Content-Type", "text/html; charset=utf-8")
	if status != 0 {
		e.Response.WriteHeader(status)
	}
	return authorizeForm.Execute(e.Response, view)
}

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

	return p.renderAuthorize(e, clientID, redirectURI, state, "", "", 0)
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
		// Re-render the polished form with an inline error so the
		// user lands on the same page rather than a generic JSON 401.
		return p.renderAuthorize(e, clientID, redirectURI, state, email, "Invalid email or password.", http.StatusUnauthorized)
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

	// If a _superusers record exists for the same email, prefer it so
	// admin operations (collections, settings, logs) authorize without
	// a second login. Identity comes from IAM; admin privilege is the
	// presence of a _superusers row keyed by email.
	if email != "" {
		if su, suErr := p.app.FindAuthRecordByEmail(core.CollectionNameSuperusers, email); suErr == nil && su != nil {
			rec = su
		}
	}

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
