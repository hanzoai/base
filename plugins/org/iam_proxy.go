// Package platform — /v1/iam/* proxy onto the configured IAM_ENDPOINT.
//
// The admin UI (apps/admin-base) and any first-party Base client
// targets a same-origin "/v1/iam" endpoint. This transparent reverse
// proxy forwards it to the configured IAM_ENDPOINT — Hanzo's hanzo.id,
// an enterprise Hanzo IAM, or an in-process iam.Embed() served by the
// fused daemon. Base is a pure IAM client; whichever IAM answers is
// opaque to the client. The mount is /v1/iam, never /api/* — /v1 is
// Base's one external prefix.

package org

import (
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/hanzoai/base/apis"
	"github.com/hanzoai/base/core"
	"github.com/hanzoai/base/tools/claims"
	"github.com/hanzoai/base/tools/router"
)

// registerIAMProxy mounts /v1/iam/{path...} forwarding to IAM_ENDPOINT.
// The proxy is opaque — every method, body, query param, and header
// (except hop-by-hop) passes through. SSE / streaming responses are
// flushed as bytes arrive.
// upstreamFor is the address rule, named once so the proxy and its test cannot
// each hold their own copy of it.
func upstreamFor(base, path string) string {
	return strings.TrimRight(base, "/") + path
}

// climbs reports a path that walks out of the mount it arrived under.
//
// A proxy forwards the DECODED path, so %2e%2e arrives here as a literal `..`
// segment and the upstream normalizes it away — /v1/iam/%2e%2e/x is asked of
// the issuer as /x, which is not under the mount and is not what the mount
// means. The refusal is here rather than a re-encoding, because whether an
// upstream normalizes before or after decoding is the upstream's business and
// a mount boundary cannot be enforced by guessing which.
func climbs(path string) bool {
	for _, segment := range strings.Split(path, "/") {
		if segment == ".." {
			return true
		}
	}

	return false
}

func (p *plugin) registerIAMProxy(r *router.Router[*core.RequestEvent]) {
	endpoint := strings.TrimRight(p.config.IAMEndpoint, "/")
	if endpoint == "" {
		return // platform.Register already errored on this
	}
	upstreamBase, err := url.Parse(endpoint)
	if err != nil {
		p.app.Logger().Error("platform: invalid IAMEndpoint for /v1/iam proxy",
			"endpoint", endpoint, "err", err)
		return
	}

	// Streaming-friendly client: no global timeout; the request's
	// context cancellation handles abandonment.
	client := &http.Client{Timeout: 0}

	handler := func(e *core.RequestEvent) error {
		// The path passes through UNCHANGED: /v1/iam/x → ${IAMEndpoint}/v1/iam/x.
		//
		// It used to strip /v1/iam and ask the issuer for /x, which is the wrong
		// address — IAM serves its whole surface UNDER /v1/iam, and every other
		// consumer in the fleet says so (IAM_KEYS_URL names the full path). The
		// strip did not fail loudly, because IAM answers an unregistered path
		// with its own single-page app at 200 text/html: the jwks call came back
		// a web page, and the studio read the 200 as success. Nothing in a status
		// code could have caught that, which is why the test beside this asserts
		// the URL the proxy BUILDS.
		if climbs(e.Request.URL.Path) {
			return e.BadRequestError("The path leaves the /v1/iam mount.", nil)
		}

		upstream := *upstreamBase
		upstream.Path = upstreamFor(upstreamBase.Path, e.Request.URL.Path)
		upstream.RawQuery = e.Request.URL.RawQuery

		req, err := http.NewRequestWithContext(
			e.Request.Context(), e.Request.Method, upstream.String(), e.Request.Body)
		if err != nil {
			return e.InternalServerError("iam proxy build failed", err)
		}
		// Forward everything except hop-by-hop, then drop every identity
		// header. Inbound they are a client saying who it is, and this mount
		// is where a caller asks IAM that very question — so what arrives
		// here must not be able to answer it. Which headers those are is
		// tools/claims's to know: naming three of them by hand let the rest
		// through, and X-User-Permissions among them is an authorization
		// bypass at whatever answers upstream.
		for k, v := range e.Request.Header {
			switch k {
			case "Connection", "Keep-Alive", "Proxy-Authenticate",
				"Proxy-Authorization", "Te", "Trailer", "Transfer-Encoding",
				"Upgrade", "Host":
				continue
			}
			req.Header[k] = v
		}
		claims.StripIdentityHeaders(req.Header)
		// Ensure Host matches upstream (some IdP impls validate it).
		req.Host = upstreamBase.Host
		// Disable proxy buffering for SSE-style endpoints.
		req.Header.Set("X-Accel-Buffering", "no")

		// SSE / streaming heuristic: use a long-lived client.
		c := client
		if !isLikelyStreaming(e.Request.URL.Path) {
			c = &http.Client{Timeout: 30 * time.Second}
		}
		resp, err := c.Do(req)
		if err != nil {
			return e.InternalServerError("iam unreachable", err)
		}
		defer resp.Body.Close()

		for k, v := range resp.Header {
			e.Response.Header()[k] = v
		}
		e.Response.WriteHeader(resp.StatusCode)

		flusher, _ := e.Response.(http.Flusher)
		buf := make([]byte, 32*1024)
		for {
			n, rerr := resp.Body.Read(buf)
			if n > 0 {
				if _, werr := e.Response.Write(buf[:n]); werr != nil {
					return nil
				}
				if flusher != nil {
					flusher.Flush()
				}
			}
			if rerr != nil {
				if rerr == io.EOF {
					return nil
				}
				return nil
			}
		}
	}

	// Base does not authenticate a request it does not serve. Everything under
	// this mount is the issuer's to answer, the caller's identity included, so
	// the two middlewares that resolve a credential are unbound from it.
	//
	// They did more than waste a JWKS round trip. A machine token carries no
	// membership, so resolving one refused the call with 403 before the proxy
	// ran — a service could no longer reach IAM through Base at all. And a token
	// that did carry an org opened that org's Base, running a full migration on
	// a fresh SQLite file, as a side effect of asking IAM for a token.
	iam := r.Group("/v1/iam").Unbind(apis.DefaultLoadAuthTokenMiddlewareId, apiKeyAuthId)

	iam.GET("/{path...}", handler)
	iam.POST("/{path...}", handler)
	iam.PUT("/{path...}", handler)
	iam.PATCH("/{path...}", handler)
	iam.DELETE("/{path...}", handler)
}

func isLikelyStreaming(path string) bool {
	return strings.Contains(path, "/stream") ||
		strings.Contains(path, "/sse") ||
		strings.Contains(path, "/events")
}
