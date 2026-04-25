// Copyright (c) 2025, Hanzo Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.
//
// Per-user write routing middleware. Consumes app.Network().WriterFor()
// to 307 mutating requests to the pod that currently owns the user's
// shard. Reads stay local (any replica converges via WAL frames).
//
// Shard key extraction: reads BASE_SHARD_KEY (env), looks for a
// matching JWT claim on the authenticated record, falls back to a
// request header named X-{SHARD_KEY}. No match = no routing (the
// request runs local, which is safe for anonymous / unscoped paths).
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
	"log/slog"
	"net/http"
	"os"
	"strings"
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

// shardResolver looks up shardKey on the request and stashes it on
// RequestEvent.Get. Order of precedence:
//  1. Authenticated record field named exactly shardKey (for
//     shardKey=user_id and an auth record on the `users` collection,
//     this is just Auth.Id).
//  2. JWT claim named shardKey on e.Auth.Claims — not all records
//     expose claims directly, so we also check a request-scoped store.
//  3. Header `X-{Shard-Key}` (X-User-Id, X-Org-Id, etc.).
//  4. Query parameter `shard=<id>` — last resort, only honoured in
//     non-production environments (controlled by BASE_SHARD_QUERY_OK).
func shardResolver(shardKey string) func(*RequestEvent) error {
	headerName := "X-" + toHTTPHeader(shardKey)
	allowQuery := strings.ToLower(os.Getenv("BASE_SHARD_QUERY_OK")) == "true"

	return func(e *RequestEvent) error {
		var shardID string

		if e.Auth != nil {
			// user_id → Auth.Id is the canonical resolution for the
			// most common shard key. Other keys (org_id) may come
			// from a non-id field on the record.
			if strings.EqualFold(shardKey, "user_id") || strings.EqualFold(shardKey, "id") {
				shardID = e.Auth.Id
			} else if v := e.Auth.GetString(shardKey); v != "" {
				shardID = v
			}
		}

		if shardID == "" {
			if v := e.Request.Header.Get(headerName); v != "" {
				shardID = v
			}
		}

		if shardID == "" && allowQuery {
			if v := e.Request.URL.Query().Get("shard"); v != "" {
				shardID = v
			}
		}

		if shardID != "" {
			e.Set(RequestEventKeyShardID, shardID)
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
			slog.Warn("write-forward: no HTTP endpoint for writer",
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

// toHTTPHeader converts a snake_case key (user_id, org_id) to the
// PascalCase form expected in HTTP headers (User-Id, Org-Id).
func toHTTPHeader(key string) string {
	parts := strings.Split(key, "_")
	for i, p := range parts {
		if p == "" {
			continue
		}
		parts[i] = strings.ToUpper(p[:1]) + p[1:]
	}
	return strings.Join(parts, "-")
}
