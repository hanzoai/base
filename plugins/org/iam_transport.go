// The wire base reaches Hanzo IAM on.
//
// IAM is a zip app (github.com/zap-proto/zip). zip serves ONE route tree across
// every address it listens on, and the address SCHEME names the transport: a
// bare host:port is ZAP, "http://" is HTTP. IAM's deployment listens on both —
// `iam serve --zap :9653 --http http://:8000` — so /v1/iam/oauth/userinfo,
// /v1/iam/get-user and the JWKS answer identically on either wire.
//
// So this is not a second client, a second protocol, or a second set of paths.
// It is the same request, addressed the same way, carried over the transport the
// configured endpoint names — the one vocabulary zip already uses for Listen and
// Mount. An https:// endpoint keeps net/http exactly as before.
//
// What the ZAP address buys is the hop, not the encoding: https://hanzo.id
// leaves the cluster, crosses the public edge and comes back to a pod one hop
// away, and it ties base's credential path to that edge being up. zap://
// iam.hanzo.svc.cluster.local:9653 is the pod.
package org

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/valyala/fasthttp"
	zaphttp "github.com/zap-proto/http"
)

// resolveIAM turns a configured endpoint into the base URL requests are built
// against and the client that carries them.
//
// The returned base URL always carries an http:// or https:// scheme, because
// that is what http.NewRequest parses and what every call site here already
// concatenates paths onto. On the ZAP wire that scheme is ADDRESSING ONLY — the
// round tripper dials ZAP and never opens a TCP HTTP connection — which is why
// the endpoint an operator writes, not the URL a request carries, is what
// selects the transport.
func resolveIAM(endpoint string, timeout time.Duration) (baseURL string, client *http.Client) {
	ep := strings.TrimSpace(endpoint)
	if ep == "" {
		ep = "https://hanzo.id"
	}
	ep = strings.TrimRight(ep, "/")

	low := strings.ToLower(ep)
	switch {
	case strings.HasPrefix(low, "http://"), strings.HasPrefix(low, "https://"):
		return ep, &http.Client{Timeout: timeout}
	case strings.HasPrefix(low, zapScheme):
		ep = ep[len(zapScheme):]
	}
	// A bare address is ZAP, matching zip's DefaultScheme: one vocabulary for
	// where a service is, whichever direction you are going.
	client = &http.Client{
		Timeout:   timeout,
		Transport: &zapTransport{tr: zaphttp.Dial(networkOf(ep), ep)},
	}
	if networkOf(ep) == "unix" {
		// A socket path is not an authority, so it cannot go in the URL. The
		// transport already holds the path it dials; the URL only has to name
		// something http.NewRequest will parse.
		return "http://iam", client
	}
	return "http://" + ep, client
}

// networkOf reads the plumbing off the address shape the way zip's own
// (unexported) networkOf does: a filesystem path is a unix socket, anything
// else is host:port. It must agree with zip's, or a socket address would be
// dialed as tcp.
func networkOf(addr string) string {
	if strings.HasPrefix(addr, "/") || strings.HasPrefix(addr, "./") || strings.HasPrefix(addr, "@") {
		return "unix"
	}
	return "tcp"
}

// zapTransport carries a net/http request over ZAP frames.
//
// It exists because every call in this package is written against net/http and
// should stay that way: the request built, the headers set and the JSON decoded
// are identical on both wires, so there is one client and one set of call sites
// rather than a parallel implementation per transport. Only the bytes change.
type zapTransport struct{ tr *zaphttp.Transport }

func (z *zapTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	req := fasthttp.AcquireRequest()
	resp := fasthttp.AcquireResponse()
	defer fasthttp.ReleaseRequest(req)
	defer fasthttp.ReleaseResponse(resp)

	req.Header.SetMethod(r.Method)
	req.SetRequestURI(r.URL.String())
	req.SetHost(r.URL.Host)
	for name, values := range r.Header {
		for _, v := range values {
			req.Header.Add(name, v)
		}
	}
	if r.Body != nil {
		body, err := io.ReadAll(r.Body)
		r.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("iam: read request body: %w", err)
		}
		req.SetBody(body)
	}

	if err := z.tr.Do(req, resp); err != nil {
		return nil, fmt.Errorf("iam: zap %s: %w", r.URL.Host, err)
	}

	// Copy before the deferred release: both the header values and the body
	// point into buffers fasthttp reuses for the next request on this conn.
	header := make(http.Header)
	resp.Header.VisitAll(func(k, v []byte) {
		header.Add(string(k), string(v))
	})
	body := append([]byte(nil), resp.Body()...)

	return &http.Response{
		Status:        fmt.Sprintf("%d %s", resp.StatusCode(), http.StatusText(resp.StatusCode())),
		StatusCode:    resp.StatusCode(),
		Proto:         "HTTP/1.1",
		ProtoMajor:    1,
		ProtoMinor:    1,
		Header:        header,
		Body:          io.NopCloser(strings.NewReader(string(body))),
		ContentLength: int64(len(body)),
		Request:       r,
	}, nil
}
