package graph

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strings"

	"github.com/hanzoai/base/apis"
	"github.com/zap-proto/zip"
)

// callsMost is how many requests one root field may make of the Base. A record
// is one request and a page of a connection one or two, so the bound is met
// only by asking for a connection under every record of a list: 50 is a page of
// opportunities under each of a page of companies, with room left over.
const callsMost = 50

// run is one root field in flight: the caller's request, which every call it
// makes on the Base presents as its own, and how many calls it has made.
type run struct {
	g      *Graph
	m      *model
	header http.Header
	host   string
	remote string
	calls  int
}

func (g *Graph) run(ctx context.Context, m *model) *run {
	h := zip.Headers(ctx)
	if h == nil {
		h = http.Header{}
	}
	host := h.Get("Host")
	// what describes the GraphQL request itself, not the call made for it
	for _, k := range []string{"Host", "Content-Length", "Content-Type", "Transfer-Encoding", "Connection",
		"Upgrade", "Accept-Encoding", "Expect", "If-None-Match", "If-Modified-Since"} {
		h.Del(k)
	}
	h.Set("Accept", "application/json")

	remote := "127.0.0.1:0"
	if ip := zip.CallerOf(ctx).IP; ip != "" {
		remote = net.JoinHostPort(ip, "0")
	}

	return &run{g: g, m: m, header: h, host: host, remote: remote}
}

// call makes one request of the Base's own router, as the caller: the route,
// its middleware and its rules are the ones the same request over REST meets,
// so nothing here decides who may read or write what. It answers the decoded
// JSON body, or nil for an empty one.
func (r *run) call(ctx context.Context, method, path string, q url.Values, body any) (any, error) {
	r.calls++
	if r.calls > callsMost {
		return nil, bad("one field may make at most %d requests of the Base; select fewer records below a list", callsMost)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	target := apis.Prefix() + path
	if len(q) > 0 {
		target += "?" + q.Encode()
	}

	// a request a server receives always has a body, if an empty one
	var rd io.Reader = http.NoBody
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return nil, bad("the data does not encode: %v", err)
		}
		rd = bytes.NewReader(raw)
	}

	req, err := http.NewRequestWithContext(ctx, method, target, rd)
	if err != nil {
		return nil, err
	}
	for k, vs := range r.header {
		req.Header[k] = append([]string(nil), vs...)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Host = r.host
	req.RemoteAddr = r.remote

	w := &recorder{header: http.Header{}, status: http.StatusOK}
	r.g.router.ServeHTTP(w, req)

	if w.status >= 400 {
		return nil, refusal(w.status, w.body.Bytes(), method, path)
	}
	if w.body.Len() == 0 {
		return nil, nil
	}
	var out any
	if err := json.Unmarshal(w.body.Bytes(), &out); err != nil {
		return nil, fmt.Errorf("base answered %s %s with a body that is not JSON: %w", method, path, err)
	}
	return out, nil
}

// refusal is what the caller is told of a request the Base refused: its status
// and its message, with each field's own reason. A server error is not the
// caller's to read; zip logs it under the request's id.
func refusal(status int, body []byte, method, path string) error {
	var e struct {
		Message string         `json:"message"`
		Data    map[string]any `json:"data"`
	}
	_ = json.Unmarshal(body, &e)

	if status >= 500 {
		return fmt.Errorf("base answered %d for %s %s: %s", status, method, path, e.Message)
	}

	msg := strings.TrimSpace(e.Message)
	if msg == "" {
		msg = http.StatusText(status)
	}
	if reasons := reasonsOf(e.Data, ""); len(reasons) > 0 {
		msg = strings.TrimSuffix(msg, ".") + ": " + strings.Join(reasons, "; ")
	}
	return &zip.HTTPError{Status: status, Msg: msg}
}

// reasonsOf flattens a validation error's data into "field: reason" lines.
func reasonsOf(data map[string]any, at string) []string {
	var out []string
	for k, v := range data {
		e, ok := v.(map[string]any)
		if !ok {
			continue
		}
		if m, ok := e["message"].(string); ok {
			out = append(out, at+k+": "+m)
			continue
		}
		out = append(out, reasonsOf(e, at+k+".")...)
	}
	sort.Strings(out)
	return out
}

// isNotFound reports whether err is the Base answering 404.
func isNotFound(err error) bool {
	he, ok := err.(*zip.HTTPError)
	return ok && he.Status == http.StatusNotFound
}

// recorder holds one response of the Base's router.
type recorder struct {
	header http.Header
	status int
	wrote  bool
	body   bytes.Buffer
}

func (w *recorder) Header() http.Header { return w.header }

func (w *recorder) WriteHeader(status int) {
	if !w.wrote {
		w.status, w.wrote = status, true
	}
}

func (w *recorder) Write(b []byte) (int, error) {
	w.wrote = true
	return w.body.Write(b)
}
