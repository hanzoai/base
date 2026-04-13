// Package ha registers leader/follower HA for a Base app.
//
// Import it for side-effect-free registration:
//
//	import _ "github.com/hanzoai/base/plugins/ha"
//
// Then call [Register] from your main:
//
//	app := base.New()
//	ha.Register(app)
//	app.Start()
//
// Config lives under the BASE_* env namespace. See the base-ha README
// for the full variable reference.
//
// HA is a no-op unless at least one of BASE_LOCAL_TARGET, BASE_STATIC_LEADER,
// or BASE_PEERS is set. That way you can import this plugin unconditionally
// and enable clustering per-deployment.
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
//   - BASE_STATIC_LEADER=http://... → all writes forward to that URL.
//   - BASE_LOCAL_TARGET=http://... + BASE_PEERS=a,b → lightweight lux
//     heartbeat-lease election; local node redirects to elected leader.
//   - Neither set → plugin is inactive (standalone Base).
//
// The full go-ha CDC pipeline (change-set capture over NATS-compatible
// JetStream) is provisioned by the base-ha binary at the SQL driver level.
// This plugin only handles the HTTP surface: leader-forwarding middleware
// and the /_ha/heartbeat endpoint.
func Register(app core.App) {
	target := os.Getenv("BASE_LOCAL_TARGET")
	peers := splitCSV(os.Getenv("BASE_PEERS"))
	static := os.Getenv("BASE_STATIC_LEADER")

	if target == "" && static == "" {
		return // standalone mode
	}

	var provider LeaderProvider
	switch {
	case static != "":
		provider = &StaticLeader{Target: static}
	default:
		p, err := NewLuxLeader(LuxConfig{
			NodeID:      nodeID(),
			LocalTarget: target,
			Peers:       peers,
		})
		if err != nil {
			app.Logger().Error("ha: lux leader init", "error", err)
			return
		}
		provider = p
	}

	app.OnServe().BindFunc(func(se *core.ServeEvent) error {
		// Forward write methods to the leader when we're a follower.
		se.Router.BindFunc(forwardToLeader(provider))

		// Mount the heartbeat endpoint when dynamic.
		if lux, ok := provider.(*LuxLeader); ok {
			se.Router.POST("/_ha/heartbeat", func(e *core.RequestEvent) error {
				lux.HandleHeartbeat(e.Response, e.Request)
				return nil
			})
		}
		return se.Next()
	})
}

// LeaderProvider abstracts election strategies.
type LeaderProvider interface {
	IsLeader() bool
	RedirectTarget() string
}

// forwardToLeader is a RequestEvent middleware that reverse-proxies
// mutating HTTP methods to the current leader when we're a follower.
func forwardToLeader(p LeaderProvider) func(*core.RequestEvent) error {
	return func(e *core.RequestEvent) error {
		if isMutating(e.Request.Method) && !p.IsLeader() {
			target := p.RedirectTarget()
			if target == "" {
				http.Error(e.Response, "no leader available", http.StatusServiceUnavailable)
				return nil
			}
			// 307 preserves the method and body; clients will resend.
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
