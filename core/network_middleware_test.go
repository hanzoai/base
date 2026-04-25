// Copyright (c) 2025, Hanzo Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.
//
// Tests for per-user writer-pin middleware. Exercises:
//   - shard resolver: JWT claim → header → query fallback
//   - write-forward: local / 307 / 503 branches
//   - URL derivation for writer endpoints (explicit map + convention)
//   - header name conversion
//   - PeerHTTPEndpoints env parsing edge cases

package core

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hanzoai/base/network"
	"github.com/hanzoai/base/tools/router"
)

// ── parsePeerHTTPEndpoints ─────────────────────────────────────────

func TestParsePeerHTTPEndpoints(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want map[string]string
	}{
		{"empty", "", map[string]string{}},
		{"single", "a=http://a:8090", map[string]string{"a": "http://a:8090"}},
		{"multi", "a=http://a:8090,b=http://b:8090", map[string]string{
			"a": "http://a:8090",
			"b": "http://b:8090",
		}},
		{"whitespace", "  a = http://a:8090 , b = http://b:8090  ", map[string]string{
			"a": "http://a:8090",
			"b": "http://b:8090",
		}},
		{"malformed-pair", "a=http://a:8090,bad,c=http://c", map[string]string{
			"a": "http://a:8090",
			"c": "http://c",
		}},
		{"empty-value", "a=,b=http://b", map[string]string{"b": "http://b"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := parsePeerHTTPEndpoints(tc.in)
			if len(got) != len(tc.want) {
				t.Errorf("size: got %d, want %d (%v)", len(got), len(tc.want), got)
			}
			for k, v := range tc.want {
				if got[k] != v {
					t.Errorf("[%q]: got %q, want %q", k, got[k], v)
				}
			}
		})
	}
}

// ── resolveWriterURL ───────────────────────────────────────────────

func TestResolveWriterURL(t *testing.T) {
	eps := map[string]string{
		"liquid-bd-0.liquid-bd-network.liquidity.svc:9999": "http://bd-0.internal:8090",
	}

	cases := []struct {
		name    string
		owner   string
		want    string
		envPort string
	}{
		{"empty-owner", "", "", ""},
		{"explicit-map", "liquid-bd-0.liquid-bd-network.liquidity.svc:9999", "http://bd-0.internal:8090", ""},
		{"derived-default-port", "liquid-bd-1.liquid-bd-network.liquidity.svc:9999", "http://liquid-bd-1.liquid-bd-network.liquidity.svc:8090", ""},
		{"derived-no-port-in-owner", "liquid-bd-2", "http://liquid-bd-2:8090", ""},
		{"derived-custom-http-port", "peer-a:9999", "http://peer-a:9443", "9443"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.envPort != "" {
				t.Setenv("BASE_PEER_HTTP_PORT", tc.envPort)
			}
			got := resolveWriterURL(tc.owner, eps)
			if got != tc.want {
				t.Errorf("resolveWriterURL(%q): got %q, want %q", tc.owner, got, tc.want)
			}
		})
	}
}

// ── toHTTPHeader ───────────────────────────────────────────────────

func TestToHTTPHeader(t *testing.T) {
	cases := map[string]string{
		"user_id":   "User-Id",
		"org_id":    "Org-Id",
		"tenant_id": "Tenant-Id",
		"x":         "X",
		"":          "",
		"a_b_c_d":   "A-B-C-D",
	}
	for in, want := range cases {
		if got := toHTTPHeader(in); got != want {
			t.Errorf("toHTTPHeader(%q): got %q, want %q", in, got, want)
		}
	}
}

// ── shardResolver ──────────────────────────────────────────────────

func newTestReq(method, url string) *RequestEvent {
	req := httptest.NewRequest(method, url, nil)
	rec := httptest.NewRecorder()
	return &RequestEvent{
		Event: router.Event{
			Request:  req,
			Response: rec,
		},
	}
	// Note: hook.Event.next is unexported; we don't set a terminal
	// handler. e.Next() returns nil on the default event, which is
	// indistinguishable from "pass through". We observe writeForward
	// outcomes via the response recorder: 307 = redirected, 503 =
	// refused, default 200 (httptest.ResponseRecorder default) = the
	// middleware called Next() and no one wrote.
}

// TestShardResolverFromHeader: no auth record, header carries shard.
func TestShardResolverFromHeader(t *testing.T) {
	fn := shardResolver("user_id")
	e := newTestReq(http.MethodGet, "/v1/some")
	e.Request.Header.Set("X-User-Id", "u-abc")

	if err := fn(e); err != nil {
		t.Fatalf("fn: %v", err)
	}
	if got := e.Get(RequestEventKeyShardID); got != "u-abc" {
		t.Errorf("shard stash: got %v, want u-abc", got)
	}
}

// TestShardResolverFromHeaderOrgID: different shard key.
func TestShardResolverFromHeaderOrgID(t *testing.T) {
	fn := shardResolver("org_id")
	e := newTestReq(http.MethodGet, "/v1/data")
	e.Request.Header.Set("X-Org-Id", "o-123")

	if err := fn(e); err != nil {
		t.Fatalf("fn: %v", err)
	}
	if got := e.Get(RequestEventKeyShardID); got != "o-123" {
		t.Errorf("stash: got %v, want o-123", got)
	}
}

// TestShardResolverMissing: no auth, no header, no query → no stash.
func TestShardResolverMissing(t *testing.T) {
	fn := shardResolver("user_id")
	e := newTestReq(http.MethodGet, "/v1/x")

	if err := fn(e); err != nil {
		t.Fatalf("fn: %v", err)
	}
	if got := e.Get(RequestEventKeyShardID); got != nil {
		t.Errorf("unexpected stash: %v", got)
	}
}

// TestShardResolverQueryGate: query fallback disabled by default.
func TestShardResolverQueryGate(t *testing.T) {
	fn := shardResolver("user_id")
	e := newTestReq(http.MethodGet, "/v1/x?shard=u-query")
	if err := fn(e); err != nil {
		t.Fatalf("fn: %v", err)
	}
	if got := e.Get(RequestEventKeyShardID); got != nil {
		t.Error("query fallback should be off by default")
	}
}

// TestShardResolverQueryEnabled: BASE_SHARD_QUERY_OK=true lets query
// param set the shard (dev/test convenience).
func TestShardResolverQueryEnabled(t *testing.T) {
	t.Setenv("BASE_SHARD_QUERY_OK", "true")
	fn := shardResolver("user_id")
	e := newTestReq(http.MethodGet, "/v1/x?shard=u-query")
	if err := fn(e); err != nil {
		t.Fatalf("fn: %v", err)
	}
	if got := e.Get(RequestEventKeyShardID); got != "u-query" {
		t.Errorf("query-enabled: got %v, want u-query", got)
	}
}

// ── writeForward ───────────────────────────────────────────────────

// fakeNet is a minimal network.Network for tests. Only WriterFor is
// meaningful — the rest are no-ops that satisfy the interface.
type fakeNet struct {
	owner string
	local bool
}

func (f *fakeNet) Enabled() bool                              { return true }
func (f *fakeNet) Start(context.Context) error                { return nil }
func (f *fakeNet) Stop(context.Context) error                 { return nil }
func (f *fakeNet) InstallWALHook(any, string) error           { return nil }
func (f *fakeNet) WriterFor(string) (string, bool)            { return f.owner, f.local }
func (f *fakeNet) MembersFor(string) []string                 { return nil }
func (f *fakeNet) Metrics() *network.Metrics                  { return nil }

// TestWriteForwardReadPassthrough: GET requests always run local
// even if WriterFor says remote.
func TestWriteForwardReadPassthrough(t *testing.T) {
	n := &fakeNet{owner: "other:9999", local: false}
	fn := writeForward(n, nil)

	e := newTestReq(http.MethodGet, "/v1/things")
	e.Set(RequestEventKeyShardID, "u-1")

	_ = fn(e)

	rec := e.Response.(*httptest.ResponseRecorder)
	if rec.Code == http.StatusTemporaryRedirect {
		t.Errorf("GET request redirected: body=%s", rec.Body.String())
	}
}

// TestWriteForwardWriteLocal: mutating request + local writer → pass through.
func TestWriteForwardWriteLocal(t *testing.T) {
	n := &fakeNet{owner: "self:9999", local: true}
	fn := writeForward(n, nil)

	e := newTestReq(http.MethodPost, "/v1/records")
	e.Set(RequestEventKeyShardID, "u-1")

	_ = fn(e)
	rec := e.Response.(*httptest.ResponseRecorder)
	if rec.Code == http.StatusTemporaryRedirect {
		t.Errorf("local writer: unexpected 307")
	}
}

// TestWriteForwardWriteNotLocal307: mutating request + remote writer
// → 307 to the resolved URL.
func TestWriteForwardWriteNotLocal307(t *testing.T) {
	n := &fakeNet{owner: "peer-b:9999", local: false}
	eps := map[string]string{"peer-b:9999": "http://peer-b.internal:8090"}
	fn := writeForward(n, eps)

	e := newTestReq(http.MethodPost, "/v1/records?filter=x")
	e.Set(RequestEventKeyShardID, "u-1")

	_ = fn(e)
	rec := e.Response.(*httptest.ResponseRecorder)
	if rec.Code != http.StatusTemporaryRedirect {
		t.Fatalf("expected 307, got %d (body=%s)", rec.Code, rec.Body.String())
	}
	loc := rec.Header().Get("Location")
	if !strings.HasPrefix(loc, "http://peer-b.internal:8090/v1/records") {
		t.Errorf("Location: got %q, want http://peer-b.internal:8090/...", loc)
	}
	if !strings.Contains(loc, "filter=x") {
		t.Errorf("Location must carry query: got %q", loc)
	}
}

// TestWriteForwardConventionDerivedURL: no explicit map → port swap.
func TestWriteForwardConventionDerivedURL(t *testing.T) {
	n := &fakeNet{
		owner: "liquid-bd-2.liquid-bd-network.liquidity.svc:9999",
		local: false,
	}
	fn := writeForward(n, nil)

	e := newTestReq(http.MethodPost, "/v1/rec")
	e.Set(RequestEventKeyShardID, "u-1")

	_ = fn(e)
	rec := e.Response.(*httptest.ResponseRecorder)
	if rec.Code != http.StatusTemporaryRedirect {
		t.Fatalf("expected 307, got %d", rec.Code)
	}
	loc := rec.Header().Get("Location")
	wantPrefix := "http://liquid-bd-2.liquid-bd-network.liquidity.svc:8090"
	if !strings.HasPrefix(loc, wantPrefix) {
		t.Errorf("Location: got %q, want prefix %q", loc, wantPrefix)
	}
}

// TestWriteForwardNoShardIDPassthrough: mutating request without a
// resolved shard runs local. Fine for admin / auth / public endpoints.
func TestWriteForwardNoShardIDPassthrough(t *testing.T) {
	n := &fakeNet{owner: "peer:9999", local: false}
	fn := writeForward(n, nil)

	e := newTestReq(http.MethodPost, "/v1/public/signup")
	// Deliberately NOT setting shardID.

	_ = fn(e)
	rec := e.Response.(*httptest.ResponseRecorder)
	if rec.Code == http.StatusTemporaryRedirect {
		t.Error("no shardID: unexpected 307")
	}
}

// TestWriteForwardEmptyOwnerServiceUnavailable: mutating request
// where WriterFor returned no owner → 503 ApiError returned.
// The router writes the 503 response from the returned error in
// production; inside a test the error surface is what we assert.
func TestWriteForwardEmptyOwnerServiceUnavailable(t *testing.T) {
	n := &fakeNet{owner: "", local: false}
	fn := writeForward(n, nil)

	e := newTestReq(http.MethodPost, "/v1/x")
	e.Set(RequestEventKeyShardID, "u-1")

	err := fn(e)
	if err == nil {
		t.Fatal("expected ApiError, got nil")
	}
	apiErr, ok := err.(*router.ApiError)
	if !ok {
		t.Fatalf("expected *router.ApiError, got %T", err)
	}
	if apiErr.Status != http.StatusServiceUnavailable {
		t.Errorf("status: got %d, want 503", apiErr.Status)
	}
}
