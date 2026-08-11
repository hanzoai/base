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

	"github.com/hanzoai/base/core"
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
		upstream := *upstreamBase
		upstream.Path = upstreamFor(upstreamBase.Path, e.Request.URL.Path)
		upstream.RawQuery = e.Request.URL.RawQuery

		req, err := http.NewRequestWithContext(
			e.Request.Context(), e.Request.Method, upstream.String(), e.Request.Body)
		if err != nil {
			return e.InternalServerError("iam proxy build failed", err)
		}
		// Forward all headers except hop-by-hop. Strip client-supplied
		// identity headers — those are minted from validated auth, not
		// from request input.
		for k, v := range e.Request.Header {
			switch k {
			case "Connection", "Keep-Alive", "Proxy-Authenticate",
				"Proxy-Authorization", "Te", "Trailer", "Transfer-Encoding",
				"Upgrade", "Host",
				"X-User-Id", "X-Org-Id", "X-User-Email":
				continue
			}
			req.Header[k] = v
		}
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

	r.GET("/v1/iam/{path...}", handler)
	r.POST("/v1/iam/{path...}", handler)
	r.PUT("/v1/iam/{path...}", handler)
	r.PATCH("/v1/iam/{path...}", handler)
	r.DELETE("/v1/iam/{path...}", handler)
}

func isLikelyStreaming(path string) bool {
	return strings.Contains(path, "/stream") ||
		strings.Contains(path, "/sse") ||
		strings.Contains(path, "/events")
}
