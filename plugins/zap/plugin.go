package zap

import (
	"net/http"

	"github.com/hanzoai/base/core"
	"github.com/hanzoai/base/tools/hook"
	luxlog "github.com/luxfi/log"
	zaplib "github.com/luxfi/zap"
)

// MustRegister registers the ZAP transport plugin with a Base app.
// Call this before app.Start().
func MustRegister(app core.App) {
	MustRegisterWithConfig(app, DefaultConfig())
}

// MustRegisterWithConfig registers the ZAP transport plugin with custom config.
func MustRegisterWithConfig(app core.App, config Config) {
	if !config.Enabled {
		return
	}

	p := &plugin{
		app:    app,
		config: config,
	}

	app.OnServe().Bind(&hook.Handler[*core.ServeEvent]{
		Id: "__zapTransport__",
		Func: func(e *core.ServeEvent) error {
			if err := e.Next(); err != nil {
				return err
			}
			// e.Next() drove the terminal OnServe handler that runs
			// Router.BuildMux and assigns e.Server.Handler, so the
			// fully-wrapped HTTP handler (all middleware + routes) is now
			// populated and can be bridged onto the ZAP node.
			p.start(e.Server.Handler)
			return nil
		},
	})

	app.OnTerminate().Bind(&hook.Handler[*core.TerminateEvent]{
		Id: "__zapTransportCleanup__",
		Func: func(e *core.TerminateEvent) error {
			p.stop()
			return e.Next()
		},
	})
}

type plugin struct {
	app    core.App
	config Config
	node   *zaplib.Node
	logger luxlog.Logger
}

// start brings up the ZAP node and puts base's HTTP surface on it.
//
// httpHandler is base's fully-wrapped handler (e.Server.Handler — the
// Router.BuildMux output carrying every middleware and route). Bridging it via
// the canonical luxfi/zap/forward terminal is what lets the ZAP gateway route
// /v1/base/* here, and it is the ONE way in: a request over ZAP passes the same
// chain as one over HTTP, so it is stripped of its identity headers, verified
// against IAM, resolved to an org, pointed at that org's Base, and answered
// under that collection's rules.
//
// There were four other message types on this node once — Collections=100,
// Records=101, Auth=102, Realtime=103 — each calling app.Save and app.Delete
// straight through. They read no credential, so they were reachable by any peer
// that could open a socket; they resolved no org, so they served the process's
// own Base whatever the caller was; and going around the request path meant
// going around the create hook that stamps owner and org, so what they wrote
// belonged to nobody. Realtime was the same hole pointed outward: a peer
// subscribed by name and every record change was pushed to it.
//
// Resolving an org from a ZAP envelope instead would have meant verifying a
// credential in a second place, on a second transport, in this package — which
// is authentication, and there is one of those in the estate. So they are gone
// rather than mended. The transport keeps its speed and stops being a door.
func (p *plugin) start(httpHandler http.Handler) {
	p.logger = luxlog.New("component", "zap")
	p.logger.Info("starting ZAP transport", "port", p.config.Port, "nodeID", p.config.NodeID)

	p.node = zaplib.NewNode(zaplib.NodeConfig{
		NodeID:      p.config.NodeID,
		Port:        p.config.Port,
		ServiceType: p.config.ServiceType,
	})

	p.bridgeForward(httpHandler)

	if err := p.node.Start(); err != nil {
		// What is lost here is the inbound surface — the gateway's route to
		// /v1/base/* on this instance. Nothing in this process goes OUT
		// through this node: the KMS bridge and every other client dial
		// their own. HTTP still serves, so this is a degradation and not a
		// reason to take the process down with it.
		p.logger.Error("ZAP transport unavailable; this instance serves HTTP only and the gateway cannot route to it over ZAP",
			"port", p.config.Port, "error", err)
		p.node = nil // never hold a node that is not running
		return
	}

	p.logger.Info("ZAP transport listening", "port", p.config.Port, "discovery", p.config.ServiceType)
}

func (p *plugin) stop() {
	if p.node != nil {
		p.logger.Info("stopping ZAP transport")
		p.node.Stop()
	}
}
