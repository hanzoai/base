package zap

import (
	"net/http"

	"github.com/luxfi/zap/forward"
)

// bridgeForward registers the canonical HTTP-over-ZAP terminal
// (luxfi/zap/forward) on the plugin's live ZAP node, dispatching every
// MsgTypeForward envelope to h.
//
// h MUST be base's fully-wrapped HTTP handler (the *http.ServeMux produced
// by Router.BuildMux and bound to e.Server.Handler) so every middleware —
// activity log, panic recovery, rate limiting, auth-token loading, security
// headers, body limit, CSP, gzip — and every route run on bridged requests
// exactly as they do on the HTTP port. forward.Serve reconstructs the
// *http.Request, injects the edge-validated identity (X-Org-Id / X-User-Id /
// X-User-IsAdmin / X-User-Permissions), serves it through h, and returns the
// buffered response as a ZAP Response.
//
// It is the ONLY thing this node answers. Four other message types once served
// collections, records, auth and realtime by calling the store directly — see
// plugin.start for why they are gone rather than mended — so a peer reaches
// exactly the surface an HTTP caller reaches, under exactly the same chain, and
// the table wire arrives here like every other route. No-op when the node is
// absent or h is nil; base's HTTP behavior is unchanged either way.
func (p *plugin) bridgeForward(h http.Handler) {
	if p.node == nil || h == nil {
		return
	}
	forward.Serve(p.node, h)
	p.logger.Info("forward_serve: ZAP HTTP terminal registered on node "+p.node.NodeID(), "msgType", forward.MsgTypeForward)
}
