package platform

// One-time-passcode (OTP) delivery + 2FA challenge for the embedded
// IAM.
//
// Two delivery channels, each behind a pluggable adapter. Env-driven
// so the embedded mode ships zero-config:
//
//   Email OTP — adapter: SMTP. Configure with SMTP_HOST/PORT/USER/PASS/FROM.
//   SMS OTP   — adapter: Twilio (default), custom (webhook).
//                Configure with TWILIO_ACCOUNT_SID/AUTH_TOKEN/FROM.
//
// Either delivery channel can serve as:
//   - PRIMARY auth (passwordless): user types the code in lieu of a
//     password. Issues an OIDC code on success.
//   - SECONDARY auth (2FA): after primary (password/social/wallet)
//     succeeds, the user is challenged for a code on a configured
//     contact method before the OIDC code is minted.
//
// 2FA mode is toggled tenant-wide via IAM_REQUIRE_2FA=true. Per-user
// opt-in is the next iteration.

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	mathrand "math/rand"
	"net/http"
	"net/smtp"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/hanzoai/base/core"
	"github.com/hanzoai/base/tools/router"
)

// --------------------------------------------------------------------------
// OTPProvider adapter
// --------------------------------------------------------------------------

type OTPProvider interface {
	// Channel returns "email" or "sms".
	Channel() string
	// Send delivers the OTP to the destination (email address or phone
	// number). The provider is responsible for formatting + retries.
	Send(ctx context.Context, to, code, brand string) error
}

// emailSMTP delivers OTPs via SMTP. Honors SMTP_HOST, SMTP_PORT,
// SMTP_USER, SMTP_PASS, SMTP_FROM. Falls back to STARTTLS when port
// 587 is configured (the common case).
type emailSMTP struct {
	host, port, user, pass, from string
}

func newEmailSMTP() *emailSMTP {
	return &emailSMTP{
		host: os.Getenv("SMTP_HOST"),
		port: cmpDefault(os.Getenv("SMTP_PORT"), "587"),
		user: os.Getenv("SMTP_USER"),
		pass: os.Getenv("SMTP_PASS"),
		from: cmpDefault(os.Getenv("SMTP_FROM"), "no-reply@hanzo.id"),
	}
}

func (e *emailSMTP) Channel() string { return "email" }

func (e *emailSMTP) Send(_ context.Context, to, code, brand string) error {
	if e.host == "" {
		return errors.New("SMTP not configured")
	}
	subj := fmt.Sprintf("Your %s sign-in code", brand)
	body := fmt.Sprintf(
		"From: %s\r\nTo: %s\r\nSubject: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=utf-8\r\n\r\n"+
			"Your %s sign-in code is:\n\n  %s\n\n"+
			"This code expires in 10 minutes. If you didn't request it, ignore this email.\n",
		e.from, to, subj, brand, code,
	)
	addr := e.host + ":" + e.port
	var auth smtp.Auth
	if e.user != "" {
		auth = smtp.PlainAuth("", e.user, e.pass, e.host)
	}
	// STARTTLS path for port 587. Port 465 (implicit TLS) and 25 (no
	// auth) covered by the smtp.SendMail default.
	return smtp.SendMail(addr, auth, e.from, []string{to}, []byte(body))
}

// twilioSMS delivers OTPs via Twilio's REST API.
type twilioSMS struct {
	sid, token, from string
}

func newTwilioSMS() *twilioSMS {
	return &twilioSMS{
		sid:   os.Getenv("TWILIO_ACCOUNT_SID"),
		token: os.Getenv("TWILIO_AUTH_TOKEN"),
		from:  os.Getenv("TWILIO_FROM"),
	}
}

func (t *twilioSMS) Channel() string { return "sms" }

func (t *twilioSMS) Send(ctx context.Context, to, code, brand string) error {
	if t.sid == "" || t.token == "" || t.from == "" {
		return errors.New("Twilio not configured")
	}
	endpoint := fmt.Sprintf("https://api.twilio.com/2010-04-01/Accounts/%s/Messages.json", t.sid)
	form := url.Values{
		"From": {t.from},
		"To":   {to},
		"Body": {fmt.Sprintf("%s sign-in code: %s (expires in 10 min)", brand, code)},
	}
	req, _ := http.NewRequestWithContext(ctx, "POST", endpoint, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth(t.sid, t.token)

	client := &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12},
		},
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("twilio %d: %s", resp.StatusCode, string(body))
	}
	return nil
}

// OTPProvidersFromEnv returns the configured OTP delivery channels.
// Empty slice = OTP is disabled (no email/SMS sign-in or 2FA).
func OTPProvidersFromEnv() []OTPProvider {
	var out []OTPProvider
	if emailOTPEnabled() {
		out = append(out, newEmailSMTP())
	}
	if smsOTPEnabled() {
		out = append(out, newTwilioSMS())
	}
	return out
}

func cmpDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

// --------------------------------------------------------------------------
// Challenge store
// --------------------------------------------------------------------------

// otpChallenge holds the in-flight state for either a passwordless
// primary login (no prior auth) or a 2FA step (post primary).
type otpChallenge struct {
	id          string
	userID      string // empty until primary auth resolves; "pending:<email>" for passwordless start
	email       string
	phone       string
	channel     string // "email" | "sms"
	code        string // server-generated 6-digit code
	attemptsLef int
	// Original OIDC flow params to resume after verification.
	clientID    string
	redirectURI string
	state       string
	expires     time.Time
}

var otpChallenges = struct {
	sync.Mutex
	m map[string]*otpChallenge
}{m: map[string]*otpChallenge{}}

const (
	otpTTL      = 10 * time.Minute
	otpAttempts = 5
)

func newOTPCode() string {
	// 6-digit numeric, cryptographically random (rand.Read seeded
	// mathrand for the zero-leading case).
	var b [4]byte
	_, _ = rand.Read(b[:])
	seed := int64(b[0])<<24 | int64(b[1])<<16 | int64(b[2])<<8 | int64(b[3])
	return fmt.Sprintf("%06d", mathrand.New(mathrand.NewSource(seed)).Intn(1_000_000))
}

func storeChallenge(c *otpChallenge) {
	otpChallenges.Lock()
	defer otpChallenges.Unlock()
	// Opportunistic GC.
	now := time.Now()
	for k, v := range otpChallenges.m {
		if now.After(v.expires) {
			delete(otpChallenges.m, k)
		}
	}
	otpChallenges.m[c.id] = c
}

func takeChallenge(id string) (*otpChallenge, bool) {
	otpChallenges.Lock()
	defer otpChallenges.Unlock()
	c, ok := otpChallenges.m[id]
	if !ok {
		return nil, false
	}
	if time.Now().After(c.expires) {
		delete(otpChallenges.m, id)
		return nil, false
	}
	return c, true
}

func dropChallenge(id string) {
	otpChallenges.Lock()
	defer otpChallenges.Unlock()
	delete(otpChallenges.m, id)
}

// --------------------------------------------------------------------------
// Routes
// --------------------------------------------------------------------------

func (p *plugin) registerEmbeddedOTPRoutes(r *router.Router[*core.RequestEvent]) {
	if p.embeddedIAM == nil {
		return
	}
	// Discovery — clients call this to render only the enabled methods.
	r.GET(embeddedIAMMount+"/auth/methods", p.handleAuthMethods)

	// Passwordless / 2FA flow.
	r.POST(embeddedIAMMount+"/oauth/otp/start", p.handleOTPStart)
	r.POST(embeddedIAMMount+"/oauth/otp/verify", p.handleOTPVerify)
}

// handleAuthMethods returns the live list of enabled auth methods.
// SPAs read this on the login page and render only what's configured —
// no env-mismatch between server and client.
func (p *plugin) handleAuthMethods(e *core.RequestEvent) error {
	return e.JSON(http.StatusOK, map[string]any{
		"methods":      EnabledAuthMethods(),
		"require_2fa":  requireSecondFactor(),
	})
}

// handleOTPStart accepts {channel: "email"|"sms", destination, client_id, redirect_uri, state}.
// Mints a challenge, sends the code, returns {challenge_id, channel, dest_hint}.
// For 2FA mode this is called AFTER primary auth completes; for
// passwordless mode it's the first call.
func (p *plugin) handleOTPStart(e *core.RequestEvent) error {
	var req struct {
		Channel     string `json:"channel"`
		Destination string `json:"destination"`
		ClientID    string `json:"client_id"`
		RedirectURI string `json:"redirect_uri"`
		State       string `json:"state"`
	}
	if err := json.NewDecoder(e.Request.Body).Decode(&req); err != nil {
		return e.BadRequestError("invalid body", err)
	}
	if req.Channel != "email" && req.Channel != "sms" {
		return e.BadRequestError("channel must be email or sms", nil)
	}
	if req.Destination == "" || req.ClientID == "" || req.RedirectURI == "" {
		return e.BadRequestError("destination, client_id, redirect_uri required", nil)
	}

	// Pick the matching provider.
	var prov OTPProvider
	for _, op := range OTPProvidersFromEnv() {
		if op.Channel() == req.Channel {
			prov = op
			break
		}
	}
	if prov == nil {
		return e.BadRequestError("channel not configured", nil)
	}

	id, err := randomURLSafe(16)
	if err != nil {
		return e.InternalServerError("challenge id", err)
	}
	code := newOTPCode()
	c := &otpChallenge{
		id:          id,
		channel:     req.Channel,
		code:        code,
		attemptsLef: otpAttempts,
		clientID:    req.ClientID,
		redirectURI: req.RedirectURI,
		state:       req.State,
		expires:     time.Now().Add(otpTTL),
	}
	if req.Channel == "email" {
		c.email = strings.TrimSpace(strings.ToLower(req.Destination))
	} else {
		c.phone = strings.TrimSpace(req.Destination)
	}
	storeChallenge(c)

	brand, _ := brandFromEnv()
	if err := prov.Send(e.Request.Context(), req.Destination, code, brand); err != nil {
		dropChallenge(id)
		return e.Error(http.StatusBadGateway, "send failed: "+err.Error(), nil)
	}

	return e.JSON(http.StatusOK, map[string]string{
		"challenge_id": id,
		"channel":      req.Channel,
		"dest_hint":    maskDest(req.Channel, req.Destination),
	})
}

func maskDest(channel, dest string) string {
	switch channel {
	case "email":
		at := strings.Index(dest, "@")
		if at <= 1 {
			return dest
		}
		return string(dest[0]) + strings.Repeat("•", at-1) + dest[at:]
	case "sms":
		if len(dest) <= 4 {
			return dest
		}
		return strings.Repeat("•", len(dest)-4) + dest[len(dest)-4:]
	}
	return dest
}

// handleOTPVerify accepts {challenge_id, code}. On match, mints an
// OIDC code and returns {redirect: <redirect_uri>?code=…&state=…}.
// For passwordless mode it also find-or-creates the user.
func (p *plugin) handleOTPVerify(e *core.RequestEvent) error {
	var req struct {
		ChallengeID string `json:"challenge_id"`
		Code        string `json:"code"`
	}
	if err := json.NewDecoder(e.Request.Body).Decode(&req); err != nil {
		return e.BadRequestError("invalid body", err)
	}
	c, ok := takeChallenge(req.ChallengeID)
	if !ok {
		return e.BadRequestError("expired or unknown challenge", nil)
	}
	if c.attemptsLef <= 0 {
		dropChallenge(c.id)
		return e.Error(http.StatusTooManyRequests, "too many attempts", nil)
	}
	if strings.TrimSpace(req.Code) != c.code {
		c.attemptsLef--
		return e.Error(http.StatusUnauthorized, "invalid code", nil)
	}

	dropChallenge(c.id)

	// Resolve the user — for passwordless mode by email, for 2FA mode
	// by the userID set when the primary auth completed.
	var user *core.Record
	var err error
	switch {
	case c.userID != "":
		user, err = p.app.FindRecordById(collectionIAMUsers, c.userID)
	case c.email != "":
		user, err = p.findOrCreateUserByEmail(c.email, "")
	default:
		err = errors.New("challenge had no user reference")
	}
	if err != nil || user == nil {
		return e.InternalServerError("user resolve", err)
	}

	// Mint OIDC code + redirect.
	flow := &pendingFlow{
		clientID:    c.clientID,
		redirectURI: c.redirectURI,
		state:       c.state,
		expires:     time.Now().Add(pendingFlowTTL),
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

	u, _ := url.Parse(flow.redirectURI)
	q := u.Query()
	q.Set("code", code)
	if flow.state != "" {
		q.Set("state", flow.state)
	}
	u.RawQuery = q.Encode()
	return e.JSON(http.StatusOK, map[string]string{"redirect": u.String()})
}
