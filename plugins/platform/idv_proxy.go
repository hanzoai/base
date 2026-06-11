// IDV (identity verification) transparent proxy for the platform plugin.
//
// IDV lives at /v1/idv/* — its own service domain, a sibling of /v1/iam,
// NOT a child. Base proxies the whole /v1/idv/* surface to
// ${IDV_ENDPOINT}/v1/idv/* so SPAs talk to one host while the upstream
// `hanzoai/idv` service stays decomplected. With IDV_ENDPOINT unset the
// proxy returns 200 {enabled:false} on /v1/idv/status and 503 elsewhere.
//
// This is independent of where IAM lives — Base is a pure IAM client and
// proxies to whatever IDV service IDV_ENDPOINT points at.

package platform

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

// idvMount is the mount path for the IDV proxy. Sibling of /v1/iam,
// NOT a child — IDV is its own service domain.
const idvMount = "/v1/idv"

type idvStatusResp struct {
	Enabled  bool   `json:"enabled"`
	Endpoint string `json:"endpoint"`
	Provider string `json:"provider"`
	Label    string `json:"label"`
}

// registerIDVProxy mounts /v1/idv/* — sibling of /v1/iam, unconditional.
// With IDV_ENDPOINT unset, /v1/idv/status returns {enabled:false} and the
// rest return 503.
func (p *plugin) registerIDVProxy(r *router.Router[*core.RequestEvent]) {
	r.GET(idvMount+"/status", p.handleIDVStatus)
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

	upstreamPath := e.Request.URL.Path
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
