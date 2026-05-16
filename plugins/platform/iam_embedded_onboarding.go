package platform

// Onboarding + pluggable IDV (Identity Verification) for the embedded
// IAM. Mirrors the shape of /id's 5-step BD pipeline so
// frontends written for that contract drop into a Hanzo deployment
// without rework — but here it's all native to @hanzo/base.
//
// State machine
// =============
//
// A user record progresses through these phases, tracked on the
// `_iam_users` row (status field):
//
//     unverified  →  identity_pending  →  documents_pending  →
//     biometric_pending  →  screening  →  approved | rejected
//
// Each phase has one or more endpoints. Calling /submit on the final
// phase fans out to any configured downstream sinks (BD, ATS, KMS).
//
// Endpoints
// =========
//
//   POST /v1/iam/onboarding                       step 1 — create application
//   POST /v1/iam/onboarding/{id}/identity         step 2 — PII (DOB, address)
//   POST /v1/iam/onboarding/{id}/documents        step 3 — multipart upload
//   POST /v1/iam/onboarding/{id}/biometric        step 4 — IDV provider handoff
//   POST /v1/iam/onboarding/{id}/screen           step 5a — AML/sanctions
//   POST /v1/iam/onboarding/{id}/submit           step 5b — fan-out
//   GET  /v1/iam/onboarding/{id}                  status
//
// All POSTs accept `Idempotency-Key` — repeating the same step with
// the same key is a no-op.
//
// Pluggable IDV
// =============
//
// An IDVProvider adapter handles the identity-verification handoff:
// biometric capture (liveness selfie, document scan, passkey
// enrollment) plus the eventual attestation pull. Choose via env —
// one knob, one provider per Base instance:
//
//   IDV_PROVIDER=securegate | onyxplus | persona | jumio | hyperverge | custom | none
//   IDV_PROVIDER_URL=https://api.<provider>.com
//   IDV_PROVIDER_API_KEY=<provider-issued key>
//
// Disable entirely with IDV_PROVIDER=none (or simply don't set the
// env — the default is auto-approve, which is fine for dev + non-
// regulated workloads but MUST be replaced before any regulated
// production deployment).
//
// SPAs probe `GET /v1/iam/idv/status` to discover which adapter is
// active and route the biometric step accordingly.
//
// Add a new adapter by implementing the IDVProvider interface and
// adding a case to IDVProviderFromEnv. No PII passes through Base
// for any provider — Base only ferries thin pointers (session URL,
// enrollment id, attestation digest).

import (
	"context"
	"errors"
	"net/http"
	"os"
	"strings"

	"github.com/hanzoai/base/core"
	"github.com/hanzoai/base/tools/router"
)

// IDVProvider is the thin contract every identity-verification
// integration implements. Three calls, async-friendly. No PII passes
// through Base for any provider — Base only ferries thin pointers
// (session URL, enrollment id, attestation digest).
type IDVProvider interface {
	// Name returns the provider's brand label for logs + UI hints.
	Name() string

	// StartSession opens a verification session for the given user.
	// Returns a redirect URL the frontend opens (hosted UI or the
	// provider's SDK init payload). The provider holds all biometric
	// PII; Base receives only the session id.
	StartSession(ctx context.Context, req IDVStartReq) (*IDVSession, error)

	// FetchAttestation pulls the signed attestation once the user
	// completes the provider-hosted flow. Returns null if the session
	// is still pending. Idempotent — safe to call repeatedly.
	FetchAttestation(ctx context.Context, sessionID string) (*IDVAttestation, error)
}

type IDVStartReq struct {
	UserID    string
	Email     string
	Name      string
	IPAddress string
	UserAgent string
}

type IDVSession struct {
	ID         string `json:"session_id"`
	RedirectTo string `json:"redirect_to"`
	ExpiresAt  string `json:"expires_at"`
}

type IDVAttestation struct {
	SessionID         string            `json:"session_id"`
	Subject           string            `json:"subject"`
	VerifiedAt        string            `json:"verified_at"`
	ChecksPassed      []string          `json:"checks_passed"`
	ChecksFailed      []string          `json:"checks_failed"`
	Digest            string            `json:"digest"`            // hash of full attestation
	ProviderSignature string            `json:"provider_signature"` // for on-chain anchoring
	Metadata          map[string]string `json:"metadata"`
}

// idvNoop is the zero-config default — auto-approves every session.
// Intended for dev + non-regulated workloads. Production deployments
// MUST set IDV_PROVIDER to something else.
type idvNoop struct{}

func (idvNoop) Name() string { return "none (auto-approve)" }
func (idvNoop) StartSession(ctx context.Context, _ IDVStartReq) (*IDVSession, error) {
	return &IDVSession{
		ID:         "noop-session",
		RedirectTo: "",
		ExpiresAt:  "",
	}, nil
}
func (idvNoop) FetchAttestation(ctx context.Context, _ string) (*IDVAttestation, error) {
	return &IDVAttestation{
		Subject:      "auto-approved",
		ChecksPassed: []string{"identity_auto"},
	}, nil
}

// IDVProviderFromEnv returns the configured provider based on
// IDV_PROVIDER. Returns idvNoop when unset or set to "none".
//
// Add an adapter by implementing IDVProvider and adding a case here.
func IDVProviderFromEnv() IDVProvider {
	switch strings.ToLower(os.Getenv("IDV_PROVIDER")) {
	case "", "none", "noop":
		return idvNoop{}
	case "securegate":
		return &httpIDVAdapter{
			label:   "Securegate",
			baseURL: defaultURL("https://api.securegate.io"),
			apiKey:  os.Getenv("IDV_PROVIDER_API_KEY"),
			// Securegate's enrollment + attestation paths.
			startPath: "/v1/sessions",
			fetchPath: "/v1/sessions/{id}",
		}
	case "onyxplus":
		return &httpIDVAdapter{
			label:     "OnyxPlus",
			baseURL:   defaultURL("https://onyxplus-api.dev."),
			apiKey:    os.Getenv("IDV_PROVIDER_API_KEY"),
			startPath: "/v1/onyx/enrollments",
			fetchPath: "/v1/onyx/enrollments/{id}",
		}
	case "persona":
		return &httpIDVAdapter{
			label:     "Persona",
			baseURL:   defaultURL("https://api.withpersona.com"),
			apiKey:    os.Getenv("IDV_PROVIDER_API_KEY"),
			startPath: "/api/v1/inquiries",
			fetchPath: "/api/v1/inquiries/{id}",
		}
	case "jumio":
		return &httpIDVAdapter{
			label:     "Jumio",
			baseURL:   defaultURL("https://account.amer-1.jumio.ai"),
			apiKey:    os.Getenv("IDV_PROVIDER_API_KEY"),
			startPath: "/api/v1/accounts",
			fetchPath: "/api/v1/accounts/{id}",
		}
	case "custom":
		// Generic webhook-style adapter: operator sets URL + API key,
		// upstream conforms to the same contract.
		return &httpIDVAdapter{
			label:     "Custom",
			baseURL:   os.Getenv("IDV_PROVIDER_URL"),
			apiKey:    os.Getenv("IDV_PROVIDER_API_KEY"),
			startPath: "/v1/sessions",
			fetchPath: "/v1/sessions/{id}",
		}
	default:
		// Unknown provider — fall back to noop and let ops notice via
		// the log line emitted at startup.
		return idvNoop{}
	}
}

// defaultURL returns IDV_PROVIDER_URL when set, otherwise the
// per-provider default. Lets operators override the SaaS region
// without code changes (e.g. Jumio EU vs US, OnyxPlus dev vs prod).
func defaultURL(def string) string {
	if v := os.Getenv("IDV_PROVIDER_URL"); v != "" {
		return v
	}
	return def
}

// httpIDVAdapter is the shared HTTP shape that every provider
// implements. The endpoints differ per provider but the contract
// (start a session, poll for an attestation) is identical, so we
// parameterize the paths and keep the wire logic in one place.
//
// All providers expect API-key auth via Authorization: Bearer. If
// any provider needs a richer auth flow (Jumio uses Basic+OAuth2,
// for example), upgrade the adapter to its own struct rather than
// braiding the variant into the shared one.
type httpIDVAdapter struct {
	label              string
	baseURL            string
	apiKey             string
	startPath          string
	fetchPath          string // "{id}" placeholder substituted at call time
}

func (a *httpIDVAdapter) Name() string { return a.label }

func (a *httpIDVAdapter) StartSession(ctx context.Context, req IDVStartReq) (*IDVSession, error) {
	if a.baseURL == "" || a.apiKey == "" {
		return nil, errors.New(a.label + ": IDV_PROVIDER_URL + IDV_PROVIDER_API_KEY required")
	}
	// TODO: real HTTP POST with provider-specific body shape. The
	// contract is stable; per-provider request shapes ship as the
	// onboarding endpoints land. For now the noop session keeps
	// SPAs unblocked.
	return &IDVSession{
		ID:         a.label + "-pending",
		RedirectTo: "",
		ExpiresAt:  "",
	}, nil
}

func (a *httpIDVAdapter) FetchAttestation(ctx context.Context, sessionID string) (*IDVAttestation, error) {
	if a.baseURL == "" || a.apiKey == "" {
		return nil, errors.New(a.label + ": IDV_PROVIDER_URL + IDV_PROVIDER_API_KEY required")
	}
	// TODO: real HTTP GET with provider-specific response shape.
	return nil, errors.New(a.label + ": adapter not yet wired")
}

// --------------------------------------------------------------------------
// Discovery
// --------------------------------------------------------------------------

// registerEmbeddedIDVRoutes exposes GET /v1/iam/idv/status so SPAs
// can detect which adapter is wired and route the biometric step
// accordingly.
func (p *plugin) registerEmbeddedIDVRoutes(r *router.Router[*core.RequestEvent]) {
	if p.embeddedIAM == nil {
		return
	}
	r.GET(embeddedIAMMount+"/idv/status", p.handleIDVStatus)
}

func (p *plugin) handleIDVStatus(e *core.RequestEvent) error {
	prov := IDVProviderFromEnv()
	enabled := strings.ToLower(os.Getenv("IDV_PROVIDER"))
	if enabled == "" || enabled == "none" || enabled == "noop" {
		enabled = "" // canonical "disabled" signal for the SPA
	}
	return e.JSON(http.StatusOK, map[string]any{
		"provider": enabled,
		"label":    prov.Name(),
		"enabled":  enabled != "",
	})
}
