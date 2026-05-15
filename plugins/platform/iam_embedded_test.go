package platform

import (
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/hanzoai/base/core"
	"github.com/hanzoai/base/tests"
)

// newReqEvent constructs a *core.RequestEvent for handler tests.
// RequestEvent's Request/Response fields live on the embedded
// router.Event struct, so they have to be assigned, not composed
// in a literal.
func newReqEvent(app core.App, req *http.Request, rec http.ResponseWriter) *core.RequestEvent {
	e := new(core.RequestEvent)
	e.App = app
	e.Request = req
	e.Response = rec
	return e
}

// newEmbeddedTestPlugin builds a Base test app and a platform plugin
// configured for embedded IAM, with users/orgs collections pre-created.
func newEmbeddedTestPlugin(t *testing.T) (*tests.TestApp, *plugin) {
	t.Helper()
	t.Setenv("IAM_MODE", "embedded")

	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("new test app: %v", err)
	}
	t.Cleanup(func() { app.Cleanup() })

	eiam, err := newEmbeddedIAM(app)
	if err != nil {
		t.Fatalf("new embedded iam: %v", err)
	}
	p := &plugin{
		app:         app,
		embeddedIAM: eiam,
		config:      PlatformConfig{IAMEndpoint: embeddedIAMMount},
	}
	if err := p.ensureUsersCollection(); err != nil {
		t.Fatalf("ensure users collection: %v", err)
	}
	return app, p
}

func TestEmbeddedIAM_KeyPersistence(t *testing.T) {
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "iam.key")

	k1, err := loadOrGenerateRSAKey(keyPath)
	if err != nil {
		t.Fatalf("first generate: %v", err)
	}
	info, err := os.Stat(keyPath)
	if err != nil {
		t.Fatalf("stat key: %v", err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Errorf("expected 0600 perms, got %o", mode)
	}

	// Second call must load the exact same key, not regenerate.
	k2, err := loadOrGenerateRSAKey(keyPath)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if k1.N.Cmp(k2.N) != 0 {
		t.Fatal("RSA modulus changed on reload — key not persisted")
	}

	// Sanity: file is a valid PEM RSA PRIVATE KEY block.
	buf, _ := os.ReadFile(keyPath)
	block, _ := pem.Decode(buf)
	if block == nil || block.Type != "RSA PRIVATE KEY" {
		t.Fatalf("unexpected PEM block: %+v", block)
	}
}

func TestEmbeddedIAM_BcryptCreateAndAuth(t *testing.T) {
	_, p := newEmbeddedTestPlugin(t)

	rec, err := p.createIAMUser("Z@Example.com", "correct horse battery", "Z")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	if rec.GetString("email") != "z@example.com" {
		t.Errorf("email should be lowercased, got %q", rec.GetString("email"))
	}
	if hash := rec.GetString("password"); !strings.HasPrefix(hash, "$2") {
		t.Errorf("password should be bcrypt-hashed (starts with $2*), got %q", hash)
	}

	// Good creds.
	if _, err := p.authenticateIAMUser("z@example.com", "correct horse battery"); err != nil {
		t.Fatalf("authenticate with correct creds: %v", err)
	}
	// Wrong password.
	if _, err := p.authenticateIAMUser("z@example.com", "wrong"); err == nil {
		t.Fatal("authenticate should fail for wrong password")
	}
	// Wrong email.
	if _, err := p.authenticateIAMUser("nope@example.com", "anything"); err == nil {
		t.Fatal("authenticate should fail for unknown email")
	}
}

func TestEmbeddedIAM_BootstrapRootUser(t *testing.T) {
	t.Setenv("EMBEDDED_IAM_ROOT_EMAIL", "root@example.com")
	t.Setenv("EMBEDDED_IAM_ROOT_PASSWORD", "rootpass-123")

	_, p := newEmbeddedTestPlugin(t)

	if err := p.bootstrapRootUser(); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	rec, err := p.authenticateIAMUser("root@example.com", "rootpass-123")
	if err != nil {
		t.Fatalf("authenticate root: %v", err)
	}
	if rec.GetString("name") != "Root" {
		t.Errorf("expected name Root, got %q", rec.GetString("name"))
	}

	// Idempotent: second call must NOT create another user or fail.
	if err := p.bootstrapRootUser(); err != nil {
		t.Fatalf("bootstrap idempotent: %v", err)
	}
	all, _ := p.app.FindRecordsByFilter(collectionIAMUsers, "id != ''", "", 0, 0, nil)
	if len(all) != 1 {
		t.Errorf("expected 1 user after idempotent bootstrap, got %d", len(all))
	}
}

func TestEmbeddedIAM_BootstrapNoOpWhenEnvMissing(t *testing.T) {
	_, p := newEmbeddedTestPlugin(t)
	if err := p.bootstrapRootUser(); err != nil {
		t.Fatalf("bootstrap no-op: %v", err)
	}
	all, _ := p.app.FindRecordsByFilter(collectionIAMUsers, "id != ''", "", 0, 0, nil)
	if len(all) != 0 {
		t.Errorf("expected 0 users when env unset, got %d", len(all))
	}
}

func TestEmbeddedIAM_JWKSContainsPublicKey(t *testing.T) {
	_, p := newEmbeddedTestPlugin(t)

	req := httptest.NewRequest(http.MethodGet, "/v1/iam/.well-known/jwks", nil)
	rec := httptest.NewRecorder()
	e := newReqEvent(p.app, req, rec)

	if err := p.handleEmbeddedJWKS(e); err != nil {
		t.Fatalf("jwks: %v", err)
	}

	var resp struct {
		Keys []struct {
			Kty, Kid, Alg, Use, N, E string
		}
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode jwks: %v", err)
	}
	if len(resp.Keys) != 1 {
		t.Fatalf("expected 1 key, got %d", len(resp.Keys))
	}
	key := resp.Keys[0]
	if key.Kty != "RSA" || key.Alg != "RS256" || key.Use != "sig" {
		t.Errorf("unexpected key shape: %+v", key)
	}
	if key.Kid != p.embeddedIAM.kid {
		t.Errorf("kid mismatch: got %q want %q", key.Kid, p.embeddedIAM.kid)
	}

	// Reconstruct and confirm modulus matches signer.
	n, _ := base64.RawURLEncoding.DecodeString(key.N)
	e2, _ := base64.RawURLEncoding.DecodeString(key.E)
	pub := &rsa.PublicKey{
		N: big.NewInt(0).SetBytes(n),
		E: int(big.NewInt(0).SetBytes(e2).Int64()),
	}
	if pub.N.Cmp(p.embeddedIAM.signer.N) != 0 {
		t.Fatal("JWKS modulus does not match signer")
	}
}

func TestEmbeddedIAM_Discovery(t *testing.T) {
	_, p := newEmbeddedTestPlugin(t)

	req := httptest.NewRequest(http.MethodGet, "/v1/iam/.well-known/openid-configuration", nil)
	req.Host = "base.example.com"
	rec := httptest.NewRecorder()
	e := newReqEvent(p.app, req, rec)

	if err := p.handleEmbeddedDiscovery(e); err != nil {
		t.Fatalf("discovery: %v", err)
	}

	var doc map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
		t.Fatalf("decode discovery: %v", err)
	}

	wantIss := "http://base.example.com/v1/iam"
	if doc["issuer"] != wantIss {
		t.Errorf("issuer = %v, want %s", doc["issuer"], wantIss)
	}
	if doc["jwks_uri"] != wantIss+"/.well-known/jwks" {
		t.Errorf("jwks_uri = %v", doc["jwks_uri"])
	}
	if doc["authorization_endpoint"] != wantIss+"/oauth/authorize" {
		t.Errorf("authorization_endpoint = %v", doc["authorization_endpoint"])
	}
}

func TestEmbeddedIAM_FullFlow(t *testing.T) {
	_, p := newEmbeddedTestPlugin(t)

	if _, err := p.createIAMUser("alice@example.com", "s3cret-pass", "Alice"); err != nil {
		t.Fatalf("create alice: %v", err)
	}

	// Step 1: /oauth/login (skipping the GET authorize HTML page since
	// it just renders a form with the same fields).
	form := url.Values{
		"email":        {"alice@example.com"},
		"password":     {"s3cret-pass"},
		"client_id":    {"test-client"},
		"redirect_uri": {"http://app.example.com/cb"},
		"state":        {"xyz"},
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/iam/oauth/login",
		strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Host = "base.example.com"

	rec := httptest.NewRecorder()
	e := newReqEvent(p.app, req, rec)
	if err := p.handleEmbeddedLogin(e); err != nil {
		t.Fatalf("login: %v", err)
	}
	if rec.Code != http.StatusFound {
		t.Fatalf("login status = %d, want 302; body=%s", rec.Code, rec.Body.String())
	}
	loc := rec.Header().Get("Location")
	u, err := url.Parse(loc)
	if err != nil {
		t.Fatalf("parse redirect: %v", err)
	}
	if !strings.HasPrefix(loc, "http://app.example.com/cb") {
		t.Errorf("redirect should be redirect_uri, got %q", loc)
	}
	code := u.Query().Get("code")
	if code == "" {
		t.Fatal("redirect missing code")
	}
	if u.Query().Get("state") != "xyz" {
		t.Errorf("state should round-trip, got %q", u.Query().Get("state"))
	}

	// Step 2: /oauth/token — exchange the code.
	tokForm := url.Values{
		"grant_type":   {"authorization_code"},
		"code":         {code},
		"redirect_uri": {"http://app.example.com/cb"},
		"client_id":    {"test-client"},
	}
	req2 := httptest.NewRequest(http.MethodPost, "/v1/iam/oauth/token",
		strings.NewReader(tokForm.Encode()))
	req2.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req2.Host = "base.example.com"
	rec2 := httptest.NewRecorder()
	e2 := newReqEvent(p.app, req2, rec2)
	if err := p.handleEmbeddedToken(e2); err != nil {
		t.Fatalf("token: %v", err)
	}
	if rec2.Code != http.StatusOK {
		t.Fatalf("token status = %d body=%s", rec2.Code, rec2.Body.String())
	}
	var tok struct {
		AccessToken string `json:"access_token"`
		IDToken     string `json:"id_token"`
		TokenType   string `json:"token_type"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.Unmarshal(rec2.Body.Bytes(), &tok); err != nil {
		t.Fatalf("decode token: %v", err)
	}
	if tok.AccessToken == "" || tok.TokenType != "Bearer" {
		t.Fatalf("bad token response: %+v", tok)
	}
	if tok.ExpiresIn != int(embeddedIAMTokenTTL/time.Second) {
		t.Errorf("expires_in = %d, want %d", tok.ExpiresIn, int(embeddedIAMTokenTTL/time.Second))
	}

	// Verify the JWT signs with our key.
	parsed, err := jwt.Parse(tok.AccessToken, func(_ *jwt.Token) (any, error) {
		return &p.embeddedIAM.signer.PublicKey, nil
	}, jwt.WithValidMethods([]string{"RS256"}))
	if err != nil || !parsed.Valid {
		t.Fatalf("verify jwt: %v", err)
	}
	claims := parsed.Claims.(jwt.MapClaims)
	if claims["email"] != "alice@example.com" {
		t.Errorf("email claim wrong: %v", claims["email"])
	}
	if claims["aud"] != "test-client" {
		t.Errorf("aud claim wrong: %v", claims["aud"])
	}
	if claims["iss"] != "http://base.example.com/v1/iam" {
		t.Errorf("iss claim wrong: %v", claims["iss"])
	}
	if _, ok := claims["sub"].(string); !ok {
		t.Errorf("sub claim missing")
	}
	if parsed.Header["kid"] != p.embeddedIAM.kid {
		t.Errorf("kid header missing or wrong: %v", parsed.Header["kid"])
	}

	// Step 3: code is single-use — second exchange must fail.
	rec2b := httptest.NewRecorder()
	clonedReq := req2.Clone(req2.Context())
	clonedReq.Body = io.NopCloser(strings.NewReader(tokForm.Encode()))
	e2b := newReqEvent(p.app, clonedReq, rec2b)
	if err := p.handleEmbeddedToken(e2b); err == nil && rec2b.Code < 400 {
		t.Errorf("expected second exchange to fail, got code=%d body=%s", rec2b.Code, rec2b.Body.String())
	}

	// Step 4: /oauth/userinfo with the JWT.
	req3 := httptest.NewRequest(http.MethodGet, "/v1/iam/oauth/userinfo", nil)
	req3.Header.Set("Authorization", "Bearer "+tok.AccessToken)
	rec3 := httptest.NewRecorder()
	e3 := newReqEvent(p.app, req3, rec3)
	if err := p.handleEmbeddedUserinfo(e3); err != nil {
		t.Fatalf("userinfo: %v", err)
	}
	if rec3.Code != http.StatusOK {
		t.Fatalf("userinfo status = %d body=%s", rec3.Code, rec3.Body.String())
	}
	var ui map[string]any
	if err := json.Unmarshal(rec3.Body.Bytes(), &ui); err != nil {
		t.Fatalf("decode userinfo: %v", err)
	}
	if ui["email"] != "alice@example.com" || ui["name"] != "Alice" {
		t.Errorf("unexpected userinfo: %+v", ui)
	}
}

func TestEmbeddedIAM_LoginRejectsBadPassword(t *testing.T) {
	_, p := newEmbeddedTestPlugin(t)
	if _, err := p.createIAMUser("a@b.com", "rightpass", ""); err != nil {
		t.Fatalf("create: %v", err)
	}
	form := url.Values{
		"email":        {"a@b.com"},
		"password":     {"wrongpass"},
		"client_id":    {"c"},
		"redirect_uri": {"http://x/cb"},
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/iam/oauth/login",
		strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	e := newReqEvent(p.app, req, rec)
	err := p.handleEmbeddedLogin(e)
	if err == nil {
		// The handler returns the error to the router which writes the status.
		// We expect either a non-nil error OR a 4xx response code.
		if rec.Code < 400 {
			t.Errorf("bad password should not succeed; code=%d body=%s", rec.Code, rec.Body.String())
		}
	}
}

func TestEmbeddedIAM_AuthorizeRendersForm(t *testing.T) {
	_, p := newEmbeddedTestPlugin(t)

	req := httptest.NewRequest(http.MethodGet,
		"/v1/iam/oauth/authorize?client_id=test&redirect_uri=http://x/cb&state=abc", nil)
	rec := httptest.NewRecorder()
	e := newReqEvent(p.app, req, rec)
	if err := p.handleEmbeddedAuthorize(e); err != nil {
		t.Fatalf("authorize: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("authorize status = %d", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{`name="email"`, `name="password"`, `value="test"`, `value="abc"`, "/v1/iam/oauth/login"} {
		if !strings.Contains(body, want) {
			t.Errorf("login form missing %q", want)
		}
	}
}

func TestEmbeddedIAM_IsEmbeddedIAM(t *testing.T) {
	t.Setenv("IAM_MODE", "")
	if IsEmbeddedIAM() {
		t.Error("IAM_MODE unset should not be embedded")
	}
	t.Setenv("IAM_MODE", "embedded")
	if !IsEmbeddedIAM() {
		t.Error("IAM_MODE=embedded should be embedded")
	}
	t.Setenv("IAM_MODE", "EMBEDDED")
	if !IsEmbeddedIAM() {
		t.Error("IAM_MODE=EMBEDDED should be embedded (case-insensitive)")
	}
	t.Setenv("IAM_MODE", "external")
	if IsEmbeddedIAM() {
		t.Error("IAM_MODE=external should not be embedded")
	}
}
