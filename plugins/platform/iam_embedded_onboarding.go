package platform

// Onboarding for the embedded IAM.
//
// Decomplected from IDV: the embedded IAM owns the onboarding STATE
// (which applications exist, which step each is on, where the
// attestation digest lives) and proxies the biometric step to a
// separately-deployed `hanzoai/idv` service over HTTP. IAM never
// imports a provider SDK; it knows nothing about Jumio/Onfido/Plaid/
// //Persona/Intellicheck/etc. — that knowledge
// belongs entirely to the IDV service.
//
// Stacked services
// ================
//
//   IAM   (`hanzoai/base`)          → /v1/iam/*               identity, sessions
//   IDV   (`hanzoai/idv`)           → /v1/idv/*               identity verification
//
// Each runs as its own binary, each configured independently. The
// only link is one env on IAM:
//
//   IDV_ENDPOINT=https://idv.hanzo.ai
//
// Unset (or empty) → IDV is disabled; the biometric step auto-
// approves (dev only). For regulated production deployments, set
// IDV_ENDPOINT to your IDV service and configure providers there.
//
// Provider selection lives in the IDV service, not here:
//
//   # On the IDV service (hanzoai/idv binary), not on IAM:
//   IDV_PROVIDER=jumio | onfido | plaid | lexisnexis | intellicheck |
//                 idmerit | berbix |  |  | persona
//   <PROVIDER>_API_TOKEN=…           (or per-provider key var)
//   <PROVIDER>_BASE_URL=…             (optional region override)
//
// State machine (IAM-owned)
// =========================
//
//     unverified  →  identity_pending  →  documents_pending  →
//     biometric_pending  →  screening  →  approved | rejected
//
// Endpoints (IAM-owned)
// =====================
//
//   POST /v1/iam/onboarding                       step 1 — create application
//   POST /v1/iam/onboarding/{id}/identity         step 2 — PII (DOB, address)
//   POST /v1/iam/onboarding/{id}/documents        step 3 — multipart upload
//   POST /v1/iam/onboarding/{id}/biometric        step 4 — proxy to IDV service
//   POST /v1/iam/onboarding/{id}/screen           step 5a — AML/sanctions
//   POST /v1/iam/onboarding/{id}/submit           step 5b — fan-out
//   GET  /v1/iam/onboarding/{id}                  status
//
// Discovery (sibling mount — NOT under /v1/iam)
// =============================================
//
//   GET  /v1/idv/status
//   POST /v1/idv/sessions
//   GET  /v1/idv/sessions/{id}
//   POST /v1/idv/webhook/{provider}
//
// IDV lives at /v1/idv/* — its own service domain, NOT a child of
// /v1/iam. base transparently proxies the whole /v1/idv/* surface
// to ${IDV_ENDPOINT}/v1/idv/* so SPAs talk to one host but the
// upstream is decomplected. With IDV_ENDPOINT unset, the proxy
// returns 200 {enabled:false} on /v1/idv/status and 503 on
// everything else.

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/hanzoai/base/core"
	"github.com/hanzoai/base/tools/router"
)

// IDVEndpoint returns the configured upstream IDV service URL, or
// "" when IDV is disabled. The trailing slash is stripped so
// callers can always concatenate "/v1/idv/...".
func IDVEndpoint() string {
	return strings.TrimRight(os.Getenv("IDV_ENDPOINT"), "/")
}

// --------------------------------------------------------------------------
// Discovery + transparent proxy: /v1/idv/*
// --------------------------------------------------------------------------

// idvMount is the mount path for the IDV proxy. Sibling of /v1/iam,
// NOT a child — IDV is its own service domain.
const idvMount = "/v1/idv"

type idvStatusResp struct {
	Enabled  bool   `json:"enabled"`
	Endpoint string `json:"endpoint"`
	Provider string `json:"provider"`
	Label    string `json:"label"`
}

func (p *plugin) registerEmbeddedIDVRoutes(r *router.Router[*core.RequestEvent]) {
	// IDV mount is unconditional — even with IDV_ENDPOINT unset the
	// /v1/idv/status endpoint must respond {enabled:false} so SPAs
	// can render the "IDV not configured" state. Other paths return
	// 503 in that case.
	r.GET(idvMount+"/status", p.handleIDVStatus)
	// Generic proxy for the rest of /v1/idv/*. One handler bound per
	// method (the router rejects `Any` when other patterns share the
	// prefix).
	r.GET(idvMount+"/{path...}", p.handleIDVProxy)
	r.POST(idvMount+"/{path...}", p.handleIDVProxy)
	r.PUT(idvMount+"/{path...}", p.handleIDVProxy)
	r.PATCH(idvMount+"/{path...}", p.handleIDVProxy)
	r.DELETE(idvMount+"/{path...}", p.handleIDVProxy)
}

func (p *plugin) handleIDVStatus(e *core.RequestEvent) error {
	endpoint := IDVEndpoint()
	if endpoint == "" {
		return e.JSON(http.StatusOK, idvStatusResp{Enabled: false})
	}

	ctx, cancel := context.WithTimeout(e.Request.Context(), 4*time.Second)
	defer cancel()

	req, _ := http.NewRequestWithContext(ctx, "GET", endpoint+"/v1/idv/status", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		// IDV unreachable — surface "enabled but degraded" rather
		// than masking. SPAs can show a friendly error.
		return e.JSON(http.StatusOK, idvStatusResp{
			Enabled:  true,
			Endpoint: endpoint,
			Label:    "unreachable",
		})
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	var upstream idvStatusResp
	if err := json.Unmarshal(body, &upstream); err != nil {
		return e.JSON(http.StatusOK, idvStatusResp{
			Enabled:  true,
			Endpoint: endpoint,
			Label:    "unknown",
		})
	}
	upstream.Enabled = true
	if upstream.Endpoint == "" {
		upstream.Endpoint = endpoint
	}
	return e.JSON(http.StatusOK, upstream)
}

// handleIDVProxy is a thin reverse proxy for everything under
// /v1/idv/* (other than /status which is handled separately for the
// "disabled" short-circuit). One round-trip, no body buffering, no
// header rewriting beyond Host.
func (p *plugin) handleIDVProxy(e *core.RequestEvent) error {
	endpoint := IDVEndpoint()
	if endpoint == "" {
		return e.Error(http.StatusServiceUnavailable, "IDV not configured (set IDV_ENDPOINT)", nil)
	}

	upstreamPath := strings.TrimPrefix(e.Request.URL.Path, "")
	if upstreamPath == "" {
		upstreamPath = "/v1/idv/"
	}
	upstreamURL := endpoint + upstreamPath
	if e.Request.URL.RawQuery != "" {
		upstreamURL += "?" + e.Request.URL.RawQuery
	}

	req, err := http.NewRequestWithContext(e.Request.Context(), e.Request.Method, upstreamURL, e.Request.Body)
	if err != nil {
		return e.Error(http.StatusInternalServerError, "proxy req: "+err.Error(), nil)
	}
	// Forward client headers verbatim. The IDV service handles its
	// own auth (API key / signature on inbound webhooks).
	for k, vv := range e.Request.Header {
		for _, v := range vv {
			req.Header.Add(k, v)
		}
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return e.Error(http.StatusBadGateway, "upstream IDV unreachable: "+err.Error(), nil)
	}
	defer resp.Body.Close()

	for k, vv := range resp.Header {
		for _, v := range vv {
			e.Response.Header().Add(k, v)
		}
	}
	e.Response.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(e.Response, resp.Body)
	return nil
}
