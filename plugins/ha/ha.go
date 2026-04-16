// Package ha registers writer/replica HA for a Base app.
//
// Quasar consensus is leaderless — all nodes are equal validators.
// SQLite's single-writer constraint is satisfied by deterministic
// writer-pinning: the lowest-sorted alive NodeID holds the write lock.
// All others are replicas that 307 mutating requests to the writer.
//
//	app := base.New()
//	ha.Register(app)
//	app.Start()
//
// Config lives under the BASE_* env namespace. See the base-ha README.
//
// HA is a no-op unless BASE_LOCAL_TARGET or BASE_STATIC_WRITER is set.
package ha

import (
	"net/http"
	"os"
	"strings"

	"github.com/hanzoai/base/core"
)

// Register wires HA into the given app.
//
// Behavior depends on env:
//   - BASE_STATIC_WRITER=http://... → all writes forward to that URL.
//   - BASE_LOCAL_TARGET=http://... + BASE_PEERS=a,b → Quasar heartbeat
//     writer-pin; lowest-sorted alive NodeID is the writer.
//   - Neither set → plugin is inactive (standalone Base).
//
// The full go-ha CDC pipeline (change-set capture over NATS-compatible
// JetStream) is provisioned by the base-ha binary at the SQL driver level.
// This plugin handles the HTTP surface: write-forwarding middleware and
// the /_ha/heartbeat endpoint.
func Register(app core.App) {
	target := os.Getenv("BASE_LOCAL_TARGET")
	peers := splitCSV(os.Getenv("BASE_PEERS"))
	static := os.Getenv("BASE_STATIC_WRITER")

	if target == "" && static == "" {
		return // standalone mode
	}

	var provider WriterProvider
	switch {
	case static != "":
		provider = &StaticWriter{Target: static}
	default:
		w, err := NewQuasarWriter(QuasarConfig{
			NodeID:      nodeID(),
			LocalTarget: target,
			Peers:       peers,
		})
		if err != nil {
			app.Logger().Error("ha: quasar writer init", "error", err)
			return
		}
		provider = w
	}

	app.OnServe().BindFunc(func(se *core.ServeEvent) error {
		// Forward mutating methods to the writer when we're a replica.
		se.Router.BindFunc(forwardToWriter(provider))

		// Mount the heartbeat endpoint for Quasar writer-pin.
		if q, ok := provider.(*QuasarWriter); ok {
			se.Router.POST("/_ha/heartbeat", func(e *core.RequestEvent) error {
				q.HandleHeartbeat(e.Response, e.Request)
				return nil
			})
		}
		return se.Next()
	})
}

// forwardToWriter is middleware that 307s mutating HTTP to the pinned writer.
func forwardToWriter(p WriterProvider) func(*core.RequestEvent) error {
	return func(e *core.RequestEvent) error {
		if isMutating(e.Request.Method) && !p.IsWriter() {
			target := p.RedirectTarget()
			if target == "" {
				http.Error(e.Response, "no writer available", http.StatusServiceUnavailable)
				return nil
			}
			http.Redirect(e.Response, e.Request, target+e.Request.URL.RequestURI(), http.StatusTemporaryRedirect)
			return nil
		}
		return e.Next()
	}
}

func isMutating(m string) bool {
	switch m {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	}
	return false
}

func splitCSV(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := parts[:0]
	for _, p := range parts {
		if v := strings.TrimSpace(p); v != "" {
			out = append(out, v)
		}
	}
	return out
}

func nodeID() string {
	if id := os.Getenv("BASE_NODE_ID"); id != "" {
		return id
	}
	hn, _ := os.Hostname()
	return hn
}
