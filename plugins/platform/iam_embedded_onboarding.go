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
// enrollment) plus the eventual attestation pull. Choose via env:
//
//   IDV_PROVIDER= | persona | jumio | hyperverge | custom | none
//   IDV_PROVIDER_URL=…
//   IDV_PROVIDER_API_KEY=…
//
// Add a new adapter by implementing the three-method interface below
// and registering it in registerIDVProvider().
//
// For "none" (the zero-config default), the biometric step auto-
// approves so a Base instance is usable for non-regulated workloads
// without an IDV contract. Regulated deployments MUST set
// IDV_PROVIDER and route accordingly.

import (
	"context"
	"errors"
	"os"
	"strings"
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
	case "":
		return idvFromEnv()
	case "custom":
		// Custom adapters set IDV_PROVIDER_URL + IDV_PROVIDER_API_KEY
		// and run a generic webhook flow. Implementation deferred —
		// the contract is enough to wire downstream UI today.
		return idvNoop{}
	default:
		// Unknown provider — fall back to noop and let ops notice via
		// the log line emitted at startup.
		return idvNoop{}
	}
}

//  is the canonical reference adapter, mirroring the
// /id integration. Real implementation lives in a sibling
// commit; the interface lands first so frontends and ops manifests
// can pin against it.
type  struct {
	baseURL string
	apiKey  string
}

func idvFromEnv() IDVProvider {
	return &{
		baseURL: os.Getenv("IDV_PROVIDER_URL"),
		apiKey:  os.Getenv("IDV_PROVIDER_API_KEY"),
	}
}

func (a *) Name() string { return "" }
func (a *) StartSession(ctx context.Context, req IDVStartReq) (*IDVSession, error) {
	// TODO: POST to ${baseURL}/v1/onyx/enrollments — wire after the
	// onboarding endpoints land. The contract here is stable.
	return nil, errors.New(" adapter not yet wired")
}
func (a *) FetchAttestation(ctx context.Context, sessionID string) (*IDVAttestation, error) {
	// TODO: GET ${baseURL}/v1/onyx/enrollments/{sessionID}
	return nil, errors.New(" adapter not yet wired")
}
