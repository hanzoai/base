package platform

// Social-OAuth (Google, GitHub) and SIWE (Sign-In With Ethereum)
// providers for the embedded IAM. Each provider is opt-in via env so
// the embedded IAM ships zero-config by default and a deployment can
// turn on any subset without touching code:
//
//   GOOGLE_CLIENT_ID / GOOGLE_CLIENT_SECRET     → Continue with Google
//   GITHUB_CLIENT_ID / GITHUB_CLIENT_SECRET     → Continue with GitHub
//   WALLET_LOGIN_ENABLED=true                   → Connect Wallet (SIWE)
//
// All three flows funnel into the existing `pendingCode` mechanism so
// the OIDC PKCE contract is identical to the email/password path: the
// caller's `redirect_uri` ends up with `?code=<our-opaque>&state=...`
// and the token endpoint trades it for our RS256 JWT.

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/hanzoai/base/core"
	"github.com/hanzoai/base/tools/router"
	luxcrypto "github.com/luxfi/crypto"
)

// socialProvider is the minimal OAuth2 + OIDC userinfo plumbing the
// embedded IAM needs to ferry a user from a 3rd-party login back into
// our `_iam_users` table.
type socialProvider struct {
	Key          string // url-safe slug used in /v1/iam/oauth/social/{key}
	Name         string // human label rendered on the button ("Google")
	AuthURL      string
	TokenURL     string
	UserinfoURL  string
	Scopes       string
	ClientID     string
	ClientSecret string
}

// enabledSocialProviders returns the social providers configured via
// env at process start. Empty slice = no buttons rendered.
func enabledSocialProviders() []*socialProvider {
	var out []*socialProvider
	if id, secret := os.Getenv("GOOGLE_CLIENT_ID"), os.Getenv("GOOGLE_CLIENT_SECRET"); id != "" && secret != "" {
		out = append(out, &socialProvider{
			Key:          "google",
			Name:         "Google",
			AuthURL:      "https://accounts.google.com/o/oauth2/v2/auth",
			TokenURL:     "https://oauth2.googleapis.com/token",
			UserinfoURL:  "https://openidconnect.googleapis.com/v1/userinfo",
			Scopes:       "openid email profile",
			ClientID:     id,
			ClientSecret: secret,
		})
	}
	if id, secret := os.Getenv("GITHUB_CLIENT_ID"), os.Getenv("GITHUB_CLIENT_SECRET"); id != "" && secret != "" {
		out = append(out, &socialProvider{
			Key:          "github",
			Name:         "GitHub",
			AuthURL:      "https://github.com/login/oauth/authorize",
			TokenURL:     "https://github.com/login/oauth/access_token",
			UserinfoURL:  "https://api.github.com/user",
			Scopes:       "read:user user:email",
			ClientID:     id,
			ClientSecret: secret,
		})
	}
	if id, secret := os.Getenv("APPLE_CLIENT_ID"), os.Getenv("APPLE_CLIENT_SECRET"); id != "" && secret != "" {
		out = append(out, &socialProvider{
			Key:          "apple",
			Name:         "Apple",
			AuthURL:      "https://appleid.apple.com/auth/authorize",
			TokenURL:     "https://appleid.apple.com/auth/token",
			UserinfoURL:  "", // Apple returns the id_token at /auth/token; no separate userinfo endpoint.
			Scopes:       "name email",
			ClientID:     id,
			ClientSecret: secret, // Apple expects a signed JWT here; operator pre-mints it.
		})
	}
	return out
}

func emailOTPEnabled() bool {
	// Email-OTP is enabled when SMTP credentials are configured. We
	// don't gate on a separate flag so the same env that turns on
	// email notifications turns on passwordless email login.
	return os.Getenv("SMTP_HOST") != "" || os.Getenv("EMAIL_OTP_ENABLED") == "true"
}

func smsOTPEnabled() bool {
	// Twilio is the canonical SMS adapter. Custom adapters can set
	// SMS_OTP_ENABLED=true + their own delivery wiring.
	if os.Getenv("TWILIO_ACCOUNT_SID") != "" && os.Getenv("TWILIO_AUTH_TOKEN") != "" {
		return true
	}
	return os.Getenv("SMS_OTP_ENABLED") == "true"
}

func walletLoginEnabled() bool {
	return os.Getenv("WALLET_LOGIN_ENABLED") == "true"
}

// requireSecondFactor returns true when the operator wants every
// primary authentication followed by an OTP step. Per-user opt-in is
// the next iteration — for now this is a tenant-wide knob.
func requireSecondFactor() bool {
	return os.Getenv("IAM_REQUIRE_2FA") == "true"
}

// AuthMethod is the contract the SPA reads from /v1/iam/auth/methods
// to render only the buttons that are actually enabled server-side.
// One source of truth: the server emits what's wired, the client
// renders exactly that. No client-side guessing, no env mismatch.
type AuthMethod struct {
	Kind        string `json:"kind"`        // "password" | "social" | "wallet" | "email_otp" | "sms_otp"
	Provider    string `json:"provider,omitempty"` // "google" | "github" | "apple" (when kind=social)
	Label       string `json:"label"`
	IsPrimary   bool   `json:"is_primary"`  // true = can start a login; false = 2FA step only
	IsSecondary bool   `json:"is_secondary"`// true = available as 2FA step
}

// EnabledAuthMethods returns the full live list of enabled methods.
// Wallet first (per UX directive: most distinct method), then social,
// then email-OTP/SMS-OTP if configured, finally password (always on
// as the universal fallback).
func EnabledAuthMethods() []AuthMethod {
	out := []AuthMethod{}
	if walletLoginEnabled() {
		out = append(out, AuthMethod{Kind: "wallet", Label: "Connect Wallet", IsPrimary: true})
	}
	for _, p := range enabledSocialProviders() {
		out = append(out, AuthMethod{Kind: "social", Provider: p.Key, Label: "Continue with " + p.Name, IsPrimary: true})
	}
	if emailOTPEnabled() {
		out = append(out, AuthMethod{Kind: "email_otp", Label: "Email code", IsPrimary: true, IsSecondary: true})
	}
	if smsOTPEnabled() {
		out = append(out, AuthMethod{Kind: "sms_otp", Label: "SMS code", IsPrimary: false, IsSecondary: true})
	}
	out = append(out, AuthMethod{Kind: "password", Label: "Email + password", IsPrimary: true})
	return out
}

// pendingFlow holds the OIDC PKCE params from the original /authorize
// request so the social-OAuth callback (or wallet verify) can resume
// the flow without server-side session cookies. Keyed by a random
// state string used as the OAuth `state` param.
type pendingFlow struct {
	clientID    string
	redirectURI string
	state       string // the original state from the caller
	expires     time.Time
}

var pendingFlows = struct {
	sync.Mutex
	m map[string]*pendingFlow
}{m: map[string]*pendingFlow{}}

const pendingFlowTTL = 10 * time.Minute

func pendingFlowsPut(key string, f *pendingFlow) {
	pendingFlows.Lock()
	defer pendingFlows.Unlock()
	// Opportunistic GC.
	now := time.Now()
	for k, v := range pendingFlows.m {
		if now.After(v.expires) {
			delete(pendingFlows.m, k)
		}
	}
	pendingFlows.m[key] = f
}

func pendingFlowsTake(key string) (*pendingFlow, bool) {
	pendingFlows.Lock()
	defer pendingFlows.Unlock()
	f, ok := pendingFlows.m[key]
	if !ok {
		return nil, false
	}
	delete(pendingFlows.m, key)
	if time.Now().After(f.expires) {
		return nil, false
	}
	return f, true
}

func randomURLSafe(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// findProvider returns the configured provider matching key, or nil.
func findProvider(key string) *socialProvider {
	for _, p := range enabledSocialProviders() {
		if p.Key == key {
			return p
		}
	}
	return nil
}

// --------------------------------------------------------------------------
// Routes
// --------------------------------------------------------------------------

func (p *plugin) registerEmbeddedSocialRoutes(r *router.Router[*core.RequestEvent]) {
	if p.embeddedIAM == nil {
		return
	}
	r.GET(embeddedIAMMount+"/oauth/social/{provider}", p.handleSocialStart)
	r.GET(embeddedIAMMount+"/oauth/social/{provider}/callback", p.handleSocialCallback)
	if walletLoginEnabled() {
		r.POST(embeddedIAMMount+"/oauth/wallet/challenge", p.handleWalletChallenge)
		r.POST(embeddedIAMMount+"/oauth/wallet/verify", p.handleWalletVerify)
	}
}

// handleSocialStart redirects the user to the upstream provider's
// authorize endpoint with our callback URL and a random state. The
// state also keys a `pendingFlow` so the callback can resume the
// original OIDC PKCE flow.
func (p *plugin) handleSocialStart(e *core.RequestEvent) error {
	key := e.Request.PathValue("provider")
	prov := findProvider(key)
	if prov == nil {
		return e.NotFoundError("unknown provider", nil)
	}

	q := e.Request.URL.Query()
	clientID := q.Get("client_id")
	redirectURI := q.Get("redirect_uri")
	origState := q.Get("state")
	if clientID == "" || redirectURI == "" {
		return e.BadRequestError("client_id and redirect_uri are required", nil)
	}

	state, err := randomURLSafe(32)
	if err != nil {
		return e.InternalServerError("state", err)
	}
	pendingFlowsPut(state, &pendingFlow{
		clientID:    clientID,
		redirectURI: redirectURI,
		state:       origState,
		expires:     time.Now().Add(pendingFlowTTL),
	})

	cb := requestOrigin(e.Request) + embeddedIAMMount + "/oauth/social/" + prov.Key + "/callback"
	authURL := prov.AuthURL + "?" + url.Values{
		"client_id":     {prov.ClientID},
		"redirect_uri":  {cb},
		"response_type": {"code"},
		"scope":         {prov.Scopes},
		"state":         {state},
	}.Encode()

	e.Response.Header().Set("Location", authURL)
	e.Response.WriteHeader(http.StatusFound)
	return nil
}

// handleSocialCallback runs after the provider redirects back. It
// exchanges the code for an access token, fetches user info, finds or
// creates a `_iam_users` record by email, then continues the original
// OIDC PKCE flow by redirecting to the caller's redirect_uri with our
// own opaque code.
func (p *plugin) handleSocialCallback(e *core.RequestEvent) error {
	key := e.Request.PathValue("provider")
	prov := findProvider(key)
	if prov == nil {
		return e.NotFoundError("unknown provider", nil)
	}

	q := e.Request.URL.Query()
	code := q.Get("code")
	state := q.Get("state")
	if code == "" || state == "" {
		return e.BadRequestError("missing code or state", nil)
	}

	flow, ok := pendingFlowsTake(state)
	if !ok {
		return e.BadRequestError("expired or unknown state", nil)
	}

	email, name, err := exchangeAndFetch(e.Request.Context(), prov, code, requestOrigin(e.Request))
	if err != nil {
		return e.Error(http.StatusBadGateway, "provider exchange failed: "+err.Error(), nil)
	}
	if email == "" {
		return e.Error(http.StatusBadGateway, "provider returned no email", nil)
	}

	user, err := p.findOrCreateUserByEmail(email, name)
	if err != nil {
		return e.InternalServerError("user upsert", err)
	}

	return p.issueCodeAndRedirect(e, user, flow)
}

// exchangeAndFetch posts the auth code to the provider's token
// endpoint, then GETs the userinfo URL with the resulting access
// token. Returns email + name.
func exchangeAndFetch(ctx interface{ Done() <-chan struct{} }, prov *socialProvider, code, origin string) (email, name string, err error) {
	cb := origin + embeddedIAMMount + "/oauth/social/" + prov.Key + "/callback"
	form := url.Values{
		"code":          {code},
		"client_id":     {prov.ClientID},
		"client_secret": {prov.ClientSecret},
		"redirect_uri":  {cb},
		"grant_type":    {"authorization_code"},
	}
	req, _ := http.NewRequest("POST", prov.TokenURL, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return "", "", fmt.Errorf("token endpoint %d: %s", resp.StatusCode, string(body))
	}
	var tokResp struct {
		AccessToken string `json:"access_token"`
		TokenType   string `json:"token_type"`
	}
	if err := json.Unmarshal(body, &tokResp); err != nil || tokResp.AccessToken == "" {
		// GitHub sometimes returns form-encoded — fall back to that.
		vals, perr := url.ParseQuery(string(body))
		if perr != nil || vals.Get("access_token") == "" {
			return "", "", fmt.Errorf("invalid token response: %s", string(body))
		}
		tokResp.AccessToken = vals.Get("access_token")
	}

	uReq, _ := http.NewRequest("GET", prov.UserinfoURL, nil)
	uReq.Header.Set("Authorization", "Bearer "+tokResp.AccessToken)
	uReq.Header.Set("Accept", "application/json")
	uResp, err := http.DefaultClient.Do(uReq)
	if err != nil {
		return "", "", err
	}
	defer uResp.Body.Close()
	uBody, _ := io.ReadAll(uResp.Body)
	if uResp.StatusCode >= 400 {
		return "", "", fmt.Errorf("userinfo %d: %s", uResp.StatusCode, string(uBody))
	}
	var info map[string]any
	if err := json.Unmarshal(uBody, &info); err != nil {
		return "", "", err
	}
	// OIDC / Google: "email", "name". GitHub: "email" can be null
	// (use the additional /user/emails endpoint), "login" as fallback name.
	email, _ = info["email"].(string)
	name, _ = info["name"].(string)
	if name == "" {
		if v, ok := info["login"].(string); ok {
			name = v
		}
	}
	if email == "" && prov.Key == "github" {
		// GitHub: when the user's primary email is private, the /user
		// endpoint returns email=null. Fetch /user/emails and pick
		// the verified primary.
		if e, err := githubPrimaryEmail(tokResp.AccessToken); err == nil {
			email = e
		}
	}
	return email, name, nil
}

func githubPrimaryEmail(accessToken string) (string, error) {
	req, _ := http.NewRequest("GET", "https://api.github.com/user/emails", nil)
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("/user/emails %d: %s", resp.StatusCode, string(body))
	}
	var emails []struct {
		Email    string `json:"email"`
		Primary  bool   `json:"primary"`
		Verified bool   `json:"verified"`
	}
	if err := json.Unmarshal(body, &emails); err != nil {
		return "", err
	}
	for _, e := range emails {
		if e.Primary && e.Verified {
			return e.Email, nil
		}
	}
	return "", errors.New("no verified primary email")
}

// findOrCreateUserByEmail is the shared upsert for social + wallet
// login. Password is empty for these users — they can only sign in
// via their original provider.
func (p *plugin) findOrCreateUserByEmail(email, name string) (*core.Record, error) {
	email = strings.TrimSpace(strings.ToLower(email))
	if email == "" {
		return nil, errors.New("email required")
	}
	rec, err := p.app.FindFirstRecordByData(collectionIAMUsers, "email", email)
	if err == nil && rec != nil {
		return rec, nil
	}
	col, err := p.app.FindCollectionByNameOrId(collectionIAMUsers)
	if err != nil {
		return nil, err
	}
	rec = core.NewRecord(col)
	rec.Set("email", email)
	rec.Set("name", name)
	// Empty password — social/wallet users can't sign in with a password.
	// The bcrypt validator in AuthenticateEmbeddedIAMUser will reject it,
	// which is the correct behavior.
	rec.Set("password", "")
	if err := p.app.Save(rec); err != nil {
		return nil, err
	}
	return rec, nil
}

// issueCodeAndRedirect mirrors handleEmbeddedLogin's tail: mint an
// opaque code keyed to the user + original flow, then redirect the
// browser back to the original redirect_uri so the OIDC PKCE
// caller continues unchanged.
func (p *plugin) issueCodeAndRedirect(e *core.RequestEvent, user *core.Record, flow *pendingFlow) error {
	code, err := generateOpaqueCode()
	if err != nil {
		return e.InternalServerError("issue code", err)
	}
	p.embeddedIAM.mu.Lock()
	p.embeddedIAM.codes[code] = &pendingCode{
		userID:      user.Id,
		email:       user.GetString("email"),
		name:        user.GetString("name"),
		clientID:    flow.clientID,
		redirectURI: flow.redirectURI,
		expires:     time.Now().Add(embeddedIAMCodeTTL),
	}
	p.embeddedIAM.evictExpiredCodesLocked()
	p.embeddedIAM.mu.Unlock()

	u, err := url.Parse(flow.redirectURI)
	if err != nil {
		return e.BadRequestError("invalid redirect_uri", err)
	}
	q := u.Query()
	q.Set("code", code)
	if flow.state != "" {
		q.Set("state", flow.state)
	}
	u.RawQuery = q.Encode()

	e.Response.Header().Set("Location", u.String())
	e.Response.WriteHeader(http.StatusFound)
	return nil
}

// --------------------------------------------------------------------------
// SIWE (wallet) login — EIP-4361
// --------------------------------------------------------------------------

var walletAddressRe = regexp.MustCompile(`^0x[0-9a-fA-F]{40}$`)

// walletNonces tracks the nonce → pendingFlow pairing so /verify can
// finish the OIDC flow after the wallet signs the SIWE message.
var walletNonces = struct {
	sync.Mutex
	m map[string]*pendingFlow
}{m: map[string]*pendingFlow{}}

type walletChallengeReq struct {
	Address     string `json:"address"`
	ClientID    string `json:"client_id"`
	RedirectURI string `json:"redirect_uri"`
	State       string `json:"state"`
}

type walletChallengeResp struct {
	Nonce   string `json:"nonce"`
	Message string `json:"message"`
}

func (p *plugin) handleWalletChallenge(e *core.RequestEvent) error {
	var req walletChallengeReq
	if err := json.NewDecoder(e.Request.Body).Decode(&req); err != nil {
		return e.BadRequestError("invalid body", err)
	}
	if !walletAddressRe.MatchString(req.Address) {
		return e.BadRequestError("invalid address", nil)
	}
	if req.ClientID == "" || req.RedirectURI == "" {
		return e.BadRequestError("client_id and redirect_uri are required", nil)
	}

	nonce, err := randomURLSafe(16)
	if err != nil {
		return e.InternalServerError("nonce", err)
	}

	walletNonces.Lock()
	// GC stale.
	now := time.Now()
	for k, v := range walletNonces.m {
		if now.After(v.expires) {
			delete(walletNonces.m, k)
		}
	}
	walletNonces.m[nonce] = &pendingFlow{
		clientID:    req.ClientID,
		redirectURI: req.RedirectURI,
		state:       req.State,
		expires:     now.Add(5 * time.Minute),
	}
	walletNonces.Unlock()

	domain := e.Request.Host
	uri := requestOrigin(e.Request)
	issuedAt := time.Now().UTC().Format(time.RFC3339)
	msg := fmt.Sprintf(
		"%s wants you to sign in with your Ethereum account:\n%s\n\nSign in to %s\n\nURI: %s\nVersion: 1\nChain ID: 1\nNonce: %s\nIssued At: %s",
		domain, req.Address, domain, uri, nonce, issuedAt,
	)

	return e.JSON(http.StatusOK, walletChallengeResp{
		Nonce:   nonce,
		Message: msg,
	})
}

type walletVerifyReq struct {
	Address   string `json:"address"`
	Message   string `json:"message"`
	Signature string `json:"signature"`
	Nonce     string `json:"nonce"`
}

func (p *plugin) handleWalletVerify(e *core.RequestEvent) error {
	var req walletVerifyReq
	if err := json.NewDecoder(e.Request.Body).Decode(&req); err != nil {
		return e.BadRequestError("invalid body", err)
	}
	if !walletAddressRe.MatchString(req.Address) {
		return e.BadRequestError("invalid address", nil)
	}

	walletNonces.Lock()
	flow, ok := walletNonces.m[req.Nonce]
	if ok {
		delete(walletNonces.m, req.Nonce)
	}
	walletNonces.Unlock()
	if !ok || time.Now().After(flow.expires) {
		return e.BadRequestError("expired or unknown nonce", nil)
	}
	if !strings.Contains(req.Message, "Nonce: "+req.Nonce) {
		return e.BadRequestError("nonce missing from message", nil)
	}
	if !strings.Contains(strings.ToLower(req.Message), strings.ToLower(req.Address)) {
		return e.BadRequestError("address missing from message", nil)
	}

	recovered, err := siweRecoverAddress(req.Message, req.Signature)
	if err != nil {
		return e.BadRequestError("invalid signature: "+err.Error(), nil)
	}
	if !strings.EqualFold(recovered, req.Address) {
		return e.BadRequestError("signature does not match address", nil)
	}

	// Synthesize an email keyed on the wallet so the find-or-create
	// path stays uniform. White-label deployments can rewrite this
	// suffix later without a schema change.
	email := strings.ToLower(req.Address) + "@wallet.local"
	user, err := p.findOrCreateUserByEmail(email, "Wallet "+req.Address[:6]+"…"+req.Address[len(req.Address)-4:])
	if err != nil {
		return e.InternalServerError("user upsert", err)
	}

	// Build the redirect target the same way the social callback does.
	// The browser POSTed here via fetch, so return the URL as JSON;
	// the client-side script does window.location = url.
	u, err := url.Parse(flow.redirectURI)
	if err != nil {
		return e.BadRequestError("invalid redirect_uri", err)
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
		clientID:    flow.clientID,
		redirectURI: flow.redirectURI,
		expires:     time.Now().Add(embeddedIAMCodeTTL),
	}
	p.embeddedIAM.evictExpiredCodesLocked()
	p.embeddedIAM.mu.Unlock()

	q := u.Query()
	q.Set("code", code)
	if flow.state != "" {
		q.Set("state", flow.state)
	}
	u.RawQuery = q.Encode()

	return e.JSON(http.StatusOK, map[string]string{"redirect": u.String()})
}

// siweRecoverAddress runs eth_personalSign verification: prefix the
// message with the Ethereum signed-data preamble, keccak256, then
// ecrecover the signer's public key, and derive the 0x address.
func siweRecoverAddress(message, sigHex string) (string, error) {
	sig, err := hex.DecodeString(strings.TrimPrefix(sigHex, "0x"))
	if err != nil {
		return "", fmt.Errorf("decode signature: %w", err)
	}
	if len(sig) != 65 {
		return "", fmt.Errorf("signature length %d != 65", len(sig))
	}
	// Ethereum encodes the recovery id at the end (v); luxcrypto.Ecrecover
	// expects v in {0,1}.
	if sig[64] >= 27 {
		sig[64] -= 27
	}

	prefix := fmt.Sprintf("\x19Ethereum Signed Message:\n%d", len(message))
	hash := luxcrypto.Keccak256([]byte(prefix), []byte(message))

	pub, err := luxcrypto.Ecrecover(hash, sig)
	if err != nil {
		return "", fmt.Errorf("ecrecover: %w", err)
	}
	if len(pub) < 65 || pub[0] != 0x04 {
		return "", fmt.Errorf("unexpected pubkey format")
	}
	// Address = last 20 bytes of keccak256(pub[1:]).
	addrHash := luxcrypto.Keccak256(pub[1:])
	addr := addrHash[len(addrHash)-20:]
	return "0x" + hex.EncodeToString(addr), nil
}
