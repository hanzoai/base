// Package org gives one Base process a Base per org.
//
// An org's Base is a Base of its own under {DataDir}/orgs/{org}/, opened the
// first time a request arrives carrying that org. Isolation is physical: a
// different org is a different file, so there is no query that can read across
// two. The file is opened under that org's own key — see [encryptedConnect] —
// so one org's data is unreadable with another's, and a deployment that
// configures no master key opens plaintext and says so rather than pretending
// otherwise.
//
// Orgs and members are IAM's, read off the validated token. This package never
// writes them — a local copy is a second answer to "who is in this org", and
// the one a request arrives on wins.
//
// It publishes /v1/bases: which Bases the caller can reach, and what state each
// is in. There is no create verb; using an org opens its Base.
//
//	org.MustRegister(app, org.Config{
//		IAMEndpoint:     "https://hanzo.id",
//		KMSEndpoint:     "zap.kms.svc.cluster.local:9999",
//		IAMClientID:     "my-client-id",
//		IAMClientSecret: "my-client-secret",
//	})
package org

import (
	"fmt"
	"net/http"
	"os"
	"slices"
	"strings"

	"github.com/hanzoai/base/apis"
	"github.com/hanzoai/base/core"
	"github.com/hanzoai/base/tools/hook"
	"github.com/hanzoai/base/tools/router"
)

// apiKeyAuthId names the middleware that resolves an IAM key, so that a route
// which must not be authenticated by Base can say which door it is closing.
const apiKeyAuthId = "platformAPIKeyAuth"

// keyKind carries which tier of IAM key authenticated a request, and the two
// tiers it can hold. A pk- key ships inside a web page and is public knowledge;
// an sk- or hk- key is the org's own server credential.
const (
	keyKind        = "authKeyType"
	keyPublishable = "publishable"
	keySecret      = "secret"
)

// Config defines the configuration for the platform plugin.
type Config struct {
	// IAMEndpoint is the base URL for Hanzo IAM (default: "https://hanzo.id").
	//
	// It names the BRAND, so minting a token always addresses it: IAM derives
	// the issuer from the request host, and a relying party that discovered
	// through one brand refuses a token issued by another.
	IAMEndpoint string

	// IAMAddress is where the service answers, when that is not where the brand
	// lives. Reading takes it — validating a token, resolving a key — because
	// none of those answers depend on which brand was addressed.
	//
	// A brand's public origin leaves the cluster and comes back to a pod one
	// hop away. "iam.hanzo.svc.cluster.local:9653" is the pod, over ZAP, which
	// is what a bare address means. Empty means read the brand's origin.
	IAMAddress string

	// KMSEndpoint is the KMS ZAP address — "host:port", "zap://host:port" or
	// "zap+mdns://_kms._tcp" (default: "zap.kms.svc.cluster.local:9999").
	// An http(s) URL is rejected at Register: Base speaks native ZAP to KMS.
	KMSEndpoint string

	// IAMClientID is the OAuth2 client ID for IAM authentication.
	IAMClientID string

	// IAMClientSecret is the OAuth2 client secret for IAM authentication.
	IAMClientSecret string

	// IAMOrg is the IAM organization identifier (optional, used by auth proxy).
	IAMOrg string

	// IAMApp is the IAM application identifier (optional, used by auth proxy).
	IAMApp string

	// ComplianceEndpoint is the base URL for Lux Compliance service (optional).
	// If set, enables KYC/AML screening and payment compliance for orgs.
	ComplianceEndpoint string

	// ComplianceAPIKey is the API key for the compliance service.
	ComplianceAPIKey string

	// PrincipalEncryptionKey is the master key per-principal keys are derived
	// from, by github.com/hanzoai/cek — see OrgDB.OrgDEK and OrgDB.UserDEK for
	// the namespace each one uses. It must be 32 bytes, which is what a master
	// key from KMS is. If empty, encryption is disabled (dev mode).
	PrincipalEncryptionKey string

	// Deprecated: use PrincipalEncryptionKey.
	OrgEncryptionKey string

	// OrgStorageEndpoint is the S3-compatible storage endpoint for per-org
	// object storage (e.g., "s3.hanzo.space" or "s3.hanzo.ai").
	// Each org and user gets isolated prefixes with SSE-C encryption.
	// If empty, no per-org S3 storage is provisioned.
	OrgStorageEndpoint string

	// OrgStorageBucket is the root S3 bucket name (default: "orgs").
	OrgStorageBucket string

	// DefaultTemplates defines collection schemas cloned per org on creation.
	// If nil, no default org collections are created.
	DefaultTemplates []CollectionTemplate
}

// principalKey is the master key per-principal DEKs are derived from.
//
// PrincipalEncryptionKey is the field the binary sets and OrgEncryptionKey is
// its deprecated spelling. Reading only the deprecated one meant the key the
// shipped binary supplies never arrived, masterKey stayed empty, and OrgDEK
// refused — so per-org encryption could not be turned on even by a deployment
// that had provided the key.
func (c Config) principalKey() string {
	if c.PrincipalEncryptionKey != "" {
		return c.PrincipalEncryptionKey
	}
	return c.OrgEncryptionKey
}

// MustRegister registers the platform plugin to the provided app instance
// and panics if it fails.
func MustRegister(app core.App, config Config) {
	if err := Register(app, config); err != nil {
		panic(err)
	}
}

// Register registers the platform plugin to the provided app instance.
//
// Hanzo Base is a pure IAM client — it never hosts identity. IAM must be
// reachable at boot via IAM_ENDPOINT (a hanzo.id base, or an in-process
// iam.Embed() served by the fused daemon). Base validates IAM JWTs against
// that endpoint's JWKS; there is no local password / OTP / MFA surface.
func Register(app core.App, config Config) error {
	if config.IAMEndpoint == "" || config.IAMEndpoint == "disabled" || config.IAMEndpoint == "none" {
		return fmt.Errorf(
			"platform: IAM_ENDPOINT is required — Hanzo Base is a pure IAM " +
				"client and does not host identity. Set IAM_ENDPOINT to a " +
				"Hanzo IAM instance (e.g. https://hanzo.id) or an in-process " +
				"iam.Embed() served by the fused daemon")
	}
	if config.KMSEndpoint == "" {
		config.KMSEndpoint = defaultKMSEndpoint
	}

	kmsClient, err := NewKMSClient(config.KMSEndpoint)
	if err != nil {
		return err
	}

	// The service's OWN IAM application credentials, installed where the client is
	// built. Every server-to-server read presents them — resolving a key to its
	// holder, reading a user by address — so a caller cannot reach IAM without one
	// and read whatever an anonymous request happens to answer with.
	iamClient := NewIAMClient(readAddress(config))
	iamClient.SetAdminCreds(AdminCreds{
		ClientID:     config.IAMClientID,
		ClientSecret: config.IAMClientSecret,
		Owner:        config.IAMOrg,
	})

	p := &plugin{
		app:        app,
		config:     config,
		iam:        iamClient,
		compliance: NewComplianceClient(config.ComplianceEndpoint, config.ComplianceAPIKey),
		org:        &OrgService{app: app, kms: kmsClient, config: config},
		orgDB:      NewOrgDB(app, config.principalKey()),
		jwksURL:    strings.TrimRight(config.IAMEndpoint, "/") + "/v1/iam/.well-known/jwks",
	}
	p.bases = newBases(p)

	// Bootstrap: ensure platform system collections exist.
	app.OnBootstrap().BindFunc(func(e *core.BootstrapEvent) error {
		if err := e.Next(); err != nil {
			return err
		}
		return p.ensureSystemCollections()
	})

	// Terminate: release the long-lived KMS connection and the Bases this
	// process opened.
	app.OnTerminate().BindFunc(func(e *core.TerminateEvent) error {
		kmsClient.Close()
		p.bases.close()
		return e.Next()
	})

	// Which Base serves an org. Reading it is how a request reaches the right
	// one; without it every read lands on the process's own Base, which is what
	// used to happen.
	app.Store().Set(apis.StoreKeyBases, apis.Bases(p.bases.base))

	p.declare(app)

	app.Logger().Info("platform: IAM is the only auth source",
		"jwksURL", p.jwksURL,
		"authEndpoint", config.IAMEndpoint,
	)

	// Serve: register API routes, identity header middleware, and org-scoping.
	app.OnServe().BindFunc(func(e *core.ServeEvent) error {
		// Expose OrgService in app store for Goja JS access.
		app.Store().Set("org", p.org)

		// API key middleware: resolve hk-/pk-/sk- keys via IAM.
		// Runs after loadAuthToken — if JWKS didn't set re.Auth (because
		// the token isn't a JWT), try resolving it as an IAM API key.
		// This gives every Base app native support for IAM-managed keys.
		e.Router.Bind(&hook.Handler[*core.RequestEvent]{
			Id:       apiKeyAuthId,
			Priority: apis.DefaultLoadAuthTokenMiddlewarePriority + 1,
			Func: func(re *core.RequestEvent) error {
				// Skip if already authenticated via JWKS/JWT
				if re.Auth != nil {
					return re.Next()
				}

				// Extract Bearer token
				auth := re.Request.Header.Get("Authorization")
				token := ""
				if len(auth) > 7 && auth[:7] == "Bearer " {
					token = auth[7:]
				}
				// Also check query param (for /v1/config?key=pk-xxx)
				if token == "" {
					token = re.Request.URL.Query().Get("key")
				}

				if token == "" || !IsAPIKey(token) {
					return re.Next()
				}

				// Resolve key via IAM
				user, err := p.iam.ResolveAPIKey(token)
				if err != nil {
					app.Logger().Debug("platform: API key resolution failed",
						"error", err, "prefix", token[:6]+"...")
					return re.Next() // fail-open to next middleware
				}

				// A key acts in the org IAM issued it under. Where the request
				// also names one, the key must already carry it — the same rule
				// a token gets, stated separately because a key is a machine and
				// holds none of the cross-tenant scope a platform operator does.
				org := ""
				if len(user.OrgIDs) > 0 {
					org = user.OrgIDs[0]
				}
				if stated := apis.StatedOrg(re); stated != "" {
					if !slices.Contains(user.OrgIDs, stated) {
						return re.ForbiddenError("The key does not carry the requested organization.", nil)
					}
					org = stated
				}
				if org == "" {
					return re.ForbiddenError("The key carries no organization.", nil)
				}

				base, err := p.bases.base(org)
				if err != nil {
					return re.InternalServerError("Failed to open the Base for this organization.", err)
				}
				re.App = base

				// Set identity context from resolved key
				re.Set(apis.RequestEventKeySub, user.ID)
				re.Set("authName", user.Name)
				re.Set("authEmail", user.Email)
				re.Set("authOwner", org)
				re.Set(apis.RequestEventKeyOrg, org)
				re.Request.Header.Set("X-User-Id", user.ID)
				re.Request.Header.Set("X-Org-Id", org)
				re.Request.Header.Set("X-User-Email", user.Email)

				// Which tier of key this is. It decides what the key may reach,
				// so it is stated once here, where the key was read.
				//
				// A secret key belongs to the org's own server rather than to
				// any one person, so it acts for the org: it reaches every
				// member's row the way an org admin's token does. A publishable
				// key acts for nobody — it is in a web page.
				if IsPublishableKey(token) {
					re.Set(keyKind, keyPublishable)
				} else {
					re.Set(keyKind, keySecret)
					re.Set(apis.RequestEventKeyOrgAdmin, true)
				}

				return re.Next()
			},
		})

		// A publishable key writes nothing, anywhere:
		//   pk- → GET, plus a create in a collection that is open to anyone
		//   sk- → all methods (create orders, manage accounts, admin)
		//   hk- → all methods (IAM service key, legacy compat)
		//   JWT → all methods (user session via IAM OIDC)
		//
		// This is a floor and not the whole rule, because a method says nothing
		// about what an answer contains. What a pk- key may READ is settled per
		// address — see publishableReachesNoBase, which refuses it every org
		// secret and every billing identity on the strength of what the key IS.
		//
		// Priority +3: runs after identity headers are set, before route handlers.
		e.Router.Bind(&hook.Handler[*core.RequestEvent]{
			Id:       "platformKeyTypeEnforcement",
			Priority: apis.DefaultLoadAuthTokenMiddlewarePriority + 3,
			Func: func(re *core.RequestEvent) error {
				kind, _ := re.Get(keyKind).(string)
				if kind != keyPublishable {
					return re.Next() // JWT, sk-, hk-, or unauthenticated — no restriction
				}

				method := re.Request.Method
				if method == http.MethodGet || method == http.MethodHead || method == http.MethodOptions {
					return re.Next() // reads allowed
				}

				// Exception: a publishable key MAY create in a PUBLIC-FORM
				// collection (createRule == "" ⇒ anonymous create allowed) — the
				// public submit path (contact forms, signups, waitlists). Every
				// other write stays blocked. Org is derived from the key by the
				// stampOrgOwnership create hook, never from the request.
				if method == http.MethodPost {
					if name := createsRecordIn(re.Request); name != "" {
						if c, err := re.App.FindCachedCollectionByNameOrId(name); err == nil &&
							c.CreateRule != nil && *c.CreateRule == "" {
							return re.Next()
						}
					}
				}

				// pk- key trying to write — block it
				return re.JSON(http.StatusForbidden, map[string]any{
					"error":   "publishable keys are read-only",
					"message": "Use a secret key (sk-) or JWT for write operations.",
					"code":    "key_read_only",
				})
			},
		})

		// What every address under /v1/bases requires of the credential that
		// reaches it, bound on the ROUTER and reading the path.
		//
		// Not bound on the group that declares those routes. A group states its
		// middleware down the GROUP tree and not down the URL, so a route
		// registered off the router at the identical address inherits nothing —
		// and two live mechanisms register exactly that way: /v1/bases itself,
		// just below, and jsvm's routerAdd, which takes a path string from an
		// extension and validates none of it. The comment this replaces claimed
		// that stating the rule on the subtree made a handler that forgets it
		// impossible to write. It is the address that carries the data, so it
		// has to be the address that carries the rule.
		//
		// Anonymous by construction: an anonymous middleware has no id and
		// Unbind removes nothing that has no id, so no group beneath this one
		// can drop the tenant boundary the way /v1/iam legitimately drops the
		// doors that resolve a credential.
		e.Router.BindFunc(publishableReachesNoBase, actsInNamedOrg, namesItsOwnUser)

		p.registerRoutes(e.Router)
		p.registerOrgRoutes(e.Router)

		// /v1/iam mount: transparent reverse proxy to IAM_ENDPOINT so the
		// admin UI and SDKs see IAM at a stable local path.
		p.registerIAMProxy(e.Router)

		// /v1/idv mount: thin reverse proxy to the standalone hanzoai/idv
		// service via IDV_ENDPOINT. Registered regardless of IAM mode —
		// IDV is its own service domain, not a child of IAM. With
		// IDV_ENDPOINT unset, /v1/idv/status returns {enabled:false}
		// and the rest return 503.
		p.registerIDVProxy(e.Router)

		return e.Next()
	})

	return nil
}

type plugin struct {
	app        core.App
	config     Config
	iam        *IAMClient
	compliance *ComplianceClient
	org        *OrgService
	orgDB      *OrgDB
	bases      *bases
	jwksURL    string
}

// declare is what this plugin states on every Base it opens, the process's own
// included.
//
// A tenant's Base has to answer the way the platform's does — same single auth
// source, same ownership stamped from the validated principal — because it is
// the same product serving a different org. Stating it in one function is what
// keeps the two from drifting; the alternative is a handler that reads a store
// key set on only one Base and silently takes the zero value on the others.
//
// What a tenant's Base must NOT get is anything that reaches past its own org.
func (p *plugin) declare(app core.App) {
	// IAM is the only auth source. Every Base route validates JWTs against
	// IAM's JWKS; the legacy local-password / OTP / MFA paths are unreachable
	// (see apis/record_auth_*: 410 Gone with a Location pointer to the IAM
	// equivalent). Base never hosts identity — it only validates.
	app.Store().Set(apis.StoreKeyExternalAuthOnly, true)
	app.Store().Set(apis.StoreKeyJWKSURL, p.jwksURL)

	// OrgService is NOT among what a tenant's Base gets. Its methods take an
	// org as an argument and check nothing, so an extension running on one
	// tenant's Base could read every other tenant's credentials and customers
	// by naming them — the whole boundary this package exists to draw, undone
	// by a store key. It stays on the process's own Base, where an operator's
	// extension runs, and it is set there by Register.
	//
	// Stamp owner+org on every base-collection create from the VALIDATED
	// principal — never from client body/headers. A caller cannot attribute a
	// record to another user or org.
	app.OnRecordCreateRequest().BindFunc(stampOrgOwnership)

	omitNested(app)
}

// omitNested states what a Base's backup does not carry: the stores that sit in
// its data directory and are not it.
//
// A backup quiesces one Base — its transaction, its write-ahead log checkpoint.
// An org's Base under the platform's data directory, and a user's database
// under that org's, each have their own handles and their own log, outside
// both. Copying one anyway takes it mid-write and pairs it with a log copied at
// another instant, so the archive holds a torn file and says nothing about it.
// Each is backed up where it is quiesced: on itself.
//
// One statement for every Base, rather than one per level. The platform has no
// users directory and an org has no orgs directory, so each Base names one
// thing it has and one it does not — which is cheaper than two statements that
// have to be kept in step, and the next level down cannot be the one somebody
// forgets.
//
// Both hooks, and it is the same function on each: a restore moves aside
// everything it is about to replace, so an archive that leaves a store out
// while a restore does not spare it would delete it.
//
// What the archive DOES carry, it says — see tools/archive, which writes the
// list into the zip's own comment.
func omitNested(app core.App) {
	omit := func(e *core.BackupEvent) error {
		e.Exclude = append(e.Exclude, orgsDirName, usersDirName)
		return e.Next()
	}
	app.OnBackupCreate().BindFunc(omit)
	app.OnBackupRestore().BindFunc(omit)
}

// --------------------------------------------------------------------------
// Bootstrap: system collections
// --------------------------------------------------------------------------

// ensureSystemCollections creates what this plugin reads and writes.
//
// Orgs and memberships are not among them. IAM owns both nouns and this package
// reads them off the validated token, so there is no _orgs or _org_members
// collection to keep in step — a local copy would be a second answer to who
// belongs to an org, and the credential the request arrived on is the one that
// answers it.
func (p *plugin) ensureSystemCollections() error {
	if err := p.ensureOrgConfigsCollection(); err != nil {
		return fmt.Errorf("platform: ensure %s: %w", collectionOrgConfigs, err)
	}
	if err := p.ensureOrgCustomersCollection(); err != nil {
		return fmt.Errorf("platform: ensure %s: %w", collectionOrgCustomers, err)
	}
	return nil
}

// --------------------------------------------------------------------------
// Route registration
// --------------------------------------------------------------------------

func (p *plugin) registerRoutes(r *router.Router[*core.RequestEvent]) {
	// A Base is what an org gets: its own collections, its own files, its own
	// key. So the resource a client addresses is /v1/bases, and never /v1/orgs —
	// the org is IAM's noun and Base does not get a second one. It is not
	// /v1/platform either: that is the PaaS at platform.hanzo.ai, a different
	// product, and sharing its name here is what made "where are my Bases?"
	// unanswerable.
	//
	// ORGS AND MEMBERSHIPS ARE IAM'S. Base READS them and never writes them:
	// minting an org here made a second registry with its own slug rules and its
	// own uniqueness check, so two systems both believed they owned the noun and
	// the one a token was issued against was not necessarily the one Base knew.
	//
	// IAM already answers both questions — /v1/iam/organizations owns the org, and
	// a validated token carries the caller's memberships (IAMUser.OrgIDs), so the
	// read needs no call at all. Create, delete and invite live at IAM.
	//
	// This is the collection, which names no org and so answers the membership
	// question: every Base on the caller's token. Everything that names one lives
	// under it and is registered together in registerOrgRoutes, where the rule for
	// naming an org is stated once. Declared whole rather than on a group, so the
	// collection root is /v1/bases and not /v1/bases/ — an empty leaf composes to
	// the trailing slash, which is an address nobody calls.
	r.GET(basesPath, p.handleListBases)

	// Compliance is its own service domain, a sibling of /v1/iam — not a child
	// of Base's own resource. Optional: registered only where it is configured.
	if p.compliance != nil && p.compliance.Enabled() {
		c := r.Group("/v1/compliance")
		c.POST("/application", p.handleCreateComplianceApp)
		c.POST("/kyc/{applicationId}", p.handleInitiateKYC)
		c.GET("/kyc/{applicationId}", p.handleGetKYCStatus)
		c.POST("/screen", p.handleScreenIndividual)
		c.POST("/payment/validate", p.handleValidatePayment)
	}
}

// --------------------------------------------------------------------------
// Route handlers
// --------------------------------------------------------------------------

// baseView is what a Base IS to a caller: the org it belongs to, whether one
// has been opened yet, and how big it has grown.
//
// There is no verb to create one. A Base is opened the first time a request
// arrives carrying its org, so "exists" reports what happened rather than what
// somebody remembered to provision — which is why this reports a fact from the
// filesystem and not a row from a table.
type baseView struct {
	Org    string `json:"org"`
	Exists bool   `json:"exists"`
	Bytes  int64  `json:"bytes,omitempty"`
}

// base reports one org's Base. An org the caller belongs to that has never been
// used answers exists:false, which is the honest answer and not an error.
func (p *plugin) base(org string) baseView {
	v := baseView{Org: org}
	path, ok := p.orgDB.GetOrgDBPath(org)
	if !ok {
		return v
	}
	v.Exists = true
	if st, err := os.Stat(path); err == nil {
		v.Bytes = st.Size()
	}
	return v
}

// handleListBases answers which Bases the caller can reach.
//
// The orgs come from the validated token — IAM puts the membership on it, so
// this needs no local table and no call back to IAM. The local membership rows
// this used to read were a copy that could disagree with the token a request
// actually arrived on.
func (p *plugin) handleListBases(e *core.RequestEvent) error {
	user, err := p.requireAuth(e)
	if err != nil {
		return err
	}

	out := make([]baseView, 0, len(user.OrgIDs))
	for _, org := range user.OrgIDs {
		if org == "" {
			continue
		}
		out = append(out, p.base(org))
	}

	return e.JSON(http.StatusOK, out)
}

// handleGetBase answers for one Base.
//
// Who may ask is settled on the subtree this route hangs off, beside the six
// other routes that name an org — see actsInNamedOrg. It used to say so here in
// its own words, which was the fourth statement of a rule three of its
// neighbours had forgotten to make.
func (p *plugin) handleGetBase(e *core.RequestEvent) error {
	return e.JSON(http.StatusOK, p.base(e.Request.PathValue("orgId")))
}

func (p *plugin) requireAuth(e *core.RequestEvent) (*IAMUser, error) {
	// If Base already authenticated the user, use that.
	//
	// The memberships come off the request rather than off the record, because a
	// record holds fields and membership is not one of Base's — IAM signs it onto
	// the token and resolveJWKSToken puts it here. Omitting it left OrgIDs empty,
	// so every Base was refused and the list of them came back empty: not "you
	// have no orgs" but "nobody thought to bring them along".
	if e.Auth != nil {
		orgs, _ := e.Get(apis.RequestEventKeyOrgs).([]string)
		return &IAMUser{
			ID:     e.Auth.Id,
			Email:  e.Auth.GetString("email"),
			Name:   e.Auth.GetString("name"),
			OrgIDs: orgs,
		}, nil
	}

	// Extract bearer token.
	authHeader := e.Request.Header.Get("Authorization")
	if !strings.HasPrefix(authHeader, "Bearer ") {
		return nil, e.UnauthorizedError("missing or invalid authorization", nil)
	}
	token := strings.TrimPrefix(authHeader, "Bearer ")

	user, err := p.iam.ValidateToken(token)
	if err != nil {
		return nil, e.UnauthorizedError("invalid or expired token", err)
	}

	return user, nil
}

// --------------------------------------------------------------------------
// Compliance handlers
// --------------------------------------------------------------------------

// requireOrg is what every compliance route needs before anything else: an org
// the credential actually acts in.
//
// These routes name no org in their address, so the rules on /v1/bases do not
// reach them and they have to ask. requireAuth alone answers "is there a
// caller", which is not the question — a KYC record belongs to somebody.
func requireOrg(e *core.RequestEvent) (string, error) {
	org := actingOrg(e)
	if org == "" {
		return "", e.ForbiddenError("The credential acts in no organization.", nil)
	}

	return org, nil
}

// ownsComplianceApp refuses an application the credential does not hold.
//
// The application id is the vendor's and arrives in the path, so on its own it
// is an unowned name: taking it and calling requireAuth answered "is there a
// caller" and never "is this caller's", and one tenant read another tenant's
// KYC status — provider, verification state and all — and started verifications
// against it. What makes it answerable is the row written when the application
// was created.
//
// The refusal for an application this org does not hold is 403 rather than 404,
// for the reason actsInNamedOrg gives: a 404 is an answer about the data, and
// it would separate "someone else's application" from "no such application".
func (p *plugin) ownsComplianceApp(e *core.RequestEvent, applicationID string) error {
	org, err := requireOrg(e)
	if err != nil {
		return err
	}

	owner, ok := p.org.ComplianceApp(org, applicationID)
	if !ok || !actsForUser(e, owner) {
		return e.ForbiddenError("The credential does not hold that application.", nil)
	}

	return nil
}

func (p *plugin) handleCreateComplianceApp(e *core.RequestEvent) error {
	user, err := p.requireAuth(e)
	if err != nil {
		return err
	}
	org, err := requireOrg(e)
	if err != nil {
		return err
	}

	var body struct {
		GivenName  string `json:"given_name"`
		FamilyName string `json:"family_name"`
		Country    string `json:"country"`
	}
	if err := e.BindBody(&body); err != nil {
		return e.BadRequestError("invalid request body", err)
	}

	appID, err := p.compliance.CreateApplication(body.GivenName, body.FamilyName, user.Email, body.Country)
	if err != nil {
		return e.InternalServerError("compliance: create application failed", err)
	}

	// An application nobody is recorded as holding is one nobody can read back,
	// so failing to record it fails the call rather than returning an id into
	// the void.
	if err := p.org.BindComplianceApp(org, caller(e), appID); err != nil {
		return e.InternalServerError("compliance: record who holds the application", err)
	}

	return e.JSON(http.StatusCreated, map[string]string{
		"application_id": appID,
		"user_id":        user.ID,
	})
}

func (p *plugin) handleInitiateKYC(e *core.RequestEvent) error {
	if _, err := p.requireAuth(e); err != nil {
		return err
	}

	applicationID := e.Request.PathValue("applicationId")
	if applicationID == "" {
		return e.BadRequestError("missing applicationId", nil)
	}
	if err := p.ownsComplianceApp(e, applicationID); err != nil {
		return err
	}

	var body struct {
		Provider string `json:"provider,omitempty"`
	}
	e.BindBody(&body)

	verID, redirectURL, err := p.compliance.InitiateKYC(applicationID, body.Provider)
	if err != nil {
		return e.InternalServerError("compliance: initiate KYC failed", err)
	}

	return e.JSON(http.StatusOK, map[string]string{
		"verification_id": verID,
		"redirect_url":    redirectURL,
	})
}

func (p *plugin) handleGetKYCStatus(e *core.RequestEvent) error {
	if _, err := p.requireAuth(e); err != nil {
		return err
	}

	applicationID := e.Request.PathValue("applicationId")
	if applicationID == "" {
		return e.BadRequestError("missing applicationId", nil)
	}
	if err := p.ownsComplianceApp(e, applicationID); err != nil {
		return err
	}

	status, err := p.compliance.GetKYCStatus(applicationID)
	if err != nil {
		return e.InternalServerError("compliance: get KYC status failed", err)
	}

	return e.JSON(http.StatusOK, status)
}

// handleScreenIndividual screens a name the caller supplies.
//
// There is no owner to check here — the subject is a person the org is
// considering, not a record anyone holds — so what this can be scoped to is the
// org doing the asking. Each call spends a screening on the deployment's
// vendor account, and the control for that is the rate limit and the org on the
// log line, not an ownership question that has no subject.
func (p *plugin) handleScreenIndividual(e *core.RequestEvent) error {
	if _, err := p.requireAuth(e); err != nil {
		return err
	}
	if _, err := requireOrg(e); err != nil {
		return err
	}

	var body struct {
		GivenName  string `json:"given_name"`
		FamilyName string `json:"family_name"`
		Country    string `json:"country"`
	}
	if err := e.BindBody(&body); err != nil {
		return e.BadRequestError("invalid request body", err)
	}

	result, err := p.compliance.ScreenIndividual(body.GivenName, body.FamilyName, body.Country)
	if err != nil {
		return e.InternalServerError("compliance: screening failed", err)
	}

	return e.JSON(http.StatusOK, result)
}

func (p *plugin) handleValidatePayment(e *core.RequestEvent) error {
	if _, err := p.requireAuth(e); err != nil {
		return err
	}
	if _, err := requireOrg(e); err != nil {
		return err
	}

	var body struct {
		FromAccountID string  `json:"from_account_id"`
		ToAccountID   string  `json:"to_account_id"`
		Amount        float64 `json:"amount"`
		Currency      string  `json:"currency"`
		Jurisdiction  string  `json:"jurisdiction"`
	}
	if err := e.BindBody(&body); err != nil {
		return e.BadRequestError("invalid request body", err)
	}

	approved, reason, err := p.compliance.ValidatePayment(
		body.FromAccountID, body.ToAccountID, body.Amount, body.Currency, body.Jurisdiction,
	)
	if err != nil {
		return e.InternalServerError("compliance: payment validation failed", err)
	}

	return e.JSON(http.StatusOK, map[string]interface{}{
		"approved": approved,
		"reason":   reason,
	})
}

// --------------------------------------------------------------------------
// Slug validation
// --------------------------------------------------------------------------

// isValidSlug checks that s contains only lowercase alphanumeric chars and
// hyphens, is non-empty, and does not start/end with a hyphen.
func isValidSlug(s string) bool {
	if s == "" || s[0] == '-' || s[len(s)-1] == '-' {
		return false
	}
	for _, ch := range s {
		if !((ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9') || ch == '-') {
			return false
		}
	}
	return true
}

// stampOrgOwnership force-sets owner+org on record create from the VALIDATED
// principal (IAM JWT auth record, or resolved IAM API key) — never from the
// request body or headers, so base attribution cannot be forged. Collections
// without owner/org fields (non-base) are left untouched. An org-scoped record
// with no trusted org (anonymous, no key) is refused — this is what makes the
// publishable key mandatory even when a collection's createRule is "".
func stampOrgOwnership(e *core.RecordRequestEvent) error {
	col := e.Collection
	hasOwner := col.Fields.GetByName("owner") != nil
	hasOrg := col.Fields.GetByName("org") != nil
	if col.System || col.IsAuth() || (!hasOwner && !hasOrg) {
		return e.Next()
	}

	// Derive identity from the validated principal ONLY.
	//
	// The org is the one the request acts in, resolved once from the credential
	// that authenticated it. This used to read the record's own org_id, which
	// was mirrored from the `owner` claim — the org of the APPLICATION the token
	// came through — so a record created by a member of alpha was stamped with
	// whichever org owned the sign-in app.
	owner := ""
	if e.Auth != nil { // IAM JWT session
		owner = e.Auth.Id
	} else if sub, _ := e.Get("authSub").(string); sub != "" { // IAM API key (pk-/sk-)
		owner = sub
	}
	org, _ := e.Get(apis.RequestEventKeyOrg).(string)

	if hasOrg {
		if org == "" {
			return e.ForbiddenError("Organization context required to create this record.", nil)
		}
		e.Record.Set("org", org)
	}
	if hasOwner {
		e.Record.Set("owner", owner) // may be "" for a pure-anon public submit
	}
	return e.Next()
}

// createsRecordIn names the collection a request creates a record in, and "" for
// a request that creates none.
//
// It reads the address the ROUTER matched, the way the rules on /v1/bases do
// and for the same reason: the router's answer is the one the handler serves,
// so an exception granted on any other reading is granted at an address that is
// not the one about to run. The create route is the records group's own POST,
// wherever the deployment mounts it, and the collection is the segment the
// router filled — which is also the segment the handler reads.
func createsRecordIn(r *http.Request) string {
	if !strings.HasSuffix(patternPath(r.Pattern), "/collections/{collection}/records") {
		return ""
	}

	return r.PathValue("collection")
}
