// Copyright (c) 2025, Hanzo Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.
//
// Per-user write routing middleware. Consumes app.Network().WriterFor()
// to 307 mutating requests to the pod that currently owns the user's
// shard. Reads stay local (any replica converges via WAL frames).
//
// Shard key extraction: BASE_SHARD_KEY names one source and names it
// explicitly — a field on the verified identity, or `header:<Name>` for
// a header the caller writes. No match = no routing (the request runs
// local, which is safe for anonymous / unscoped paths).
//
// Writer endpoint resolution: BASE_PEER_HTTP_ENDPOINTS is a
// comma-separated list of `nodeID=url` pairs. Without it we derive
// the HTTP address from BASE_PEERS by swapping the P2P port for
// the HTTP port (convention: operator-emitted BASE_PEERS carries
// pod-FQDN:9999, we rewrite to pod-FQDN:8090).
//
// Safe defaults: middleware is a no-op when app.Network() is nil
// or reports Enabled() == false (singleton). The same app code
// runs in 1-pod and N-pod mode unmodified.

package core

import (
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/hanzoai/base/network"
	luxlog "github.com/luxfi/log"
)

// RequestEventKeyShardID stores the resolved shard ID on the
// RequestEvent so downstream handlers (OrgDB resolution, logging)
// can read it without re-parsing the JWT.
const RequestEventKeyShardID = "baseShardID"

// installNetworkMiddleware is called once at OnServe bind time.
// Mounts two chained handlers: shard resolution (read-only), then
// write-forward (may 307). Adds an idempotent /-/base/members
// debug endpoint useful for health probes.
func (app *BaseApp) installNetworkMiddleware(se *ServeEvent) error {
	if app.network == nil || !app.network.Enabled() {
		return nil
	}

	shardKey := os.Getenv("BASE_SHARD_KEY")
	if shardKey == "" {
		// Enabled without a shard key shouldn't happen — validate()
		// rejects it — but be defensive. Without a key we can't route.
		return nil
	}
	endpoints := parsePeerHTTPEndpoints(os.Getenv("BASE_PEER_HTTP_ENDPOINTS"))

	se.Router.BindFunc(shardResolver(shardKey))
	se.Router.BindFunc(writeForward(app.network, endpoints))

	se.Router.GET("/-/base/members", func(e *RequestEvent) error {
		// Cheap health / membership probe; monitoring dashboards
		// hit this to see scale events.
		members := app.network.MembersFor("")
		return e.JSON(http.StatusOK, map[string]any{
			"shardKey": shardKey,
			"members":  members,
			"nodeID":   os.Getenv("BASE_NODE_ID"),
		})
	})
	return nil
}

// shardResolver stashes the shard a request belongs to, from the ONE source
// BASE_SHARD_KEY names. The source is part of the name:
//
//	user_id, id      the authenticated record's id
//	<field>          that field on the authenticated record
//	header:<Name>    the named request header
//
// A header is whatever the caller sends, so naming one is the operator saying
// callers pick their own shard — which compose dev needs, having no token to
// derive one from. Every other form reads the verified identity and nothing
// else: a request that carries no identity resolves no shard, and writeForward
// runs it local.
func shardResolver(shardKey string) func(*RequestEvent) error {
	if name, ok := strings.CutPrefix(shardKey, network.ShardKeyHeader); ok {
		header := http.CanonicalHeaderKey(strings.TrimSpace(name))
		return func(e *RequestEvent) error {
			if v := e.Request.Header.Get(header); v != "" {
				e.Set(RequestEventKeyShardID, v)
			}
			return e.Next()
		}
	}

	return func(e *RequestEvent) error {
		if e.Auth != nil {
			// user_id → Auth.Id is the canonical resolution for the
			// most common shard key. Other keys (org_id) may come
			// from a non-id field on the record.
			var shardID string
			if strings.EqualFold(shardKey, "user_id") || strings.EqualFold(shardKey, "id") {
				shardID = e.Auth.Id
			} else {
				shardID = e.Auth.GetString(shardKey)
			}
			if shardID != "" {
				e.Set(RequestEventKeyShardID, shardID)
			}
		}
		return e.Next()
	}
}

// writeForward is the HTTP-level writer pin. If the current pod is
// not the owner of the resolved shard, mutating methods get a 307
// to the owner's HTTP endpoint. Reads pass through unchanged.
//
// The owner endpoint comes from the endpoints map when present.
// Missing entries fall back to a convention-derived URL (swap
// :9999 → :8090 on the P2P NodeID). Anything else → 503 (we know
// the request is misrouted but can't point anywhere useful).
func writeForward(net baseNetwork, endpoints map[string]string) func(*RequestEvent) error {
	return func(e *RequestEvent) error {
		if !isMutating(e.Request.Method) {
			return e.Next()
		}
		shardID, _ := e.Get(RequestEventKeyShardID).(string)
		if shardID == "" {
			// Anonymous / unscoped write — no routing possible; run
			// local. Caller gets best-effort consistency. Fine for
			// admin / health / unauthenticated public endpoints.
			return e.Next()
		}

		owner, local := net.WriterFor(shardID)
		if local {
			return e.Next()
		}

		target := resolveWriterURL(owner, endpoints)
		if target == "" {
			luxlog.Warn("write-forward: no HTTP endpoint for writer",
				"owner", owner, "shardID", shardID)
			return e.Error(http.StatusServiceUnavailable, "write-forward: writer not reachable", nil)
		}
		url := target + e.Request.URL.RequestURI()
		http.Redirect(e.Response, e.Request, url, http.StatusTemporaryRedirect)
		return nil
	}
}

func isMutating(m string) bool {
	switch m {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	}
	return false
}

// parsePeerHTTPEndpoints reads the `a=http://a:8090,b=http://b:8090`
// form into a map. Invalid entries are silently dropped; the write
// path returns 503 for unknown owners.
func parsePeerHTTPEndpoints(env string) map[string]string {
	out := map[string]string{}
	if env == "" {
		return out
	}
	for _, pair := range strings.Split(env, ",") {
		pair = strings.TrimSpace(pair)
		k, v, ok := strings.Cut(pair, "=")
		if !ok {
			continue
		}
		k = strings.TrimSpace(k)
		v = strings.TrimSpace(v)
		if k != "" && v != "" {
			out[k] = v
		}
	}
	return out
}

// resolveWriterURL returns the HTTP URL for `owner`.
//
// Explicit map wins. Otherwise we derive: owner is "host:9999" (P2P
// port from BASE_PEERS); we swap to the HTTP port. Default HTTP
// port is 8090 (BASE_LISTEN_HTTP default) but can be overridden
// per-process via BASE_PEER_HTTP_PORT.
func resolveWriterURL(owner string, endpoints map[string]string) string {
	if owner == "" {
		return ""
	}
	if v, ok := endpoints[owner]; ok {
		return strings.TrimRight(v, "/")
	}
	httpPort := os.Getenv("BASE_PEER_HTTP_PORT")
	if httpPort == "" {
		httpPort = "8090"
	}
	host := owner
	if i := strings.LastIndex(owner, ":"); i >= 0 {
		host = owner[:i]
	}
	return fmt.Sprintf("http://%s:%s", host, httpPort)
}
