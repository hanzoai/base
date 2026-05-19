// Copyright © 2026 Hanzo AI. MIT License.

// Package zap adds the HIP-0110 ZAP server surface to Hanzo Base.
//
// The base process already speaks ZAP through plugins/zap (collections,
// records, auth, realtime). HIP-0110 adds the reverse-push direction:
// when a base collection changes for an actively-subscribed client, the
// gateway needs the SSE / WebSocket frame to flow directly back through
// the existing gateway↔base ZAP socket — no JSON re-marshaling, no
// extra hop through cloud.
//
// This server registers two new handlers on base's existing zap node:
//
//   MsgTypeForward — a HIP-0110 Forward envelope from the gateway. The
//                    server decodes it, calls the matching collection
//                    or auth handler, returns the response envelope.
//
//   MsgTypeSubscribe — a HIP-0110 Subscribe envelope from the gateway.
//                      Records (ConnID → collection set) in the local
//                      subs table; future record changes addressed to
//                      those collections produce MsgTypePush envelopes
//                      that the gateway routes to the matching conn.
//
// Reuses the existing realtime subs map from plugins/zap and the
// existing zaplib.Node — no new pubsub, no new transport.
package zap

import (
	"context"
	"errors"
	"sync"

	luxlog "github.com/luxfi/log"
	zaplib "github.com/luxfi/zap"
)

// HIP-0110 message type IDs. Must match
// github.com/hanzoai/gateway/zap_wire.go (the gateway-side source of
// truth) — copied here as constants so base does not take a build dep
// on the gateway repo.
const (
	MsgTypeForward   uint16 = 0x1010
	MsgTypePush      uint16 = 0x1020
	MsgTypeSubscribe uint16 = 0x1030
)

// Forward / Push field offsets — same layout as gateway/zap_wire.go.
const (
	fwdIsAdmin     = 0
	fwdPermissions = 4
	fwdTenantID    = 12
	fwdUserID      = 24
	fwdMethod      = 36
	fwdPath        = 48
	fwdConnID      = 60
	fwdHeaders     = 72
	fwdBody        = 84
	fwdSlotSize    = 96

	pushConnID   = 0
	pushFrame    = 12
	pushEncoding = 24
	pushSlotSize = 36

	// Subscribe envelope:
	//   slot[ 0..11] ConnID     Text
	//   slot[12..23] Collection Text   (single collection per subscribe;
	//                                   multi-collection = multi-subscribe)
	subscribeConnID      = 0
	subscribeCollection  = 12
	subscribeSlotSize    = 24
)

// PushSink is the seam the gateway implements on its side: gateway's
// HandleReversePush is the canonical implementation. Inside base it's
// just a slot the Server holds and uses to send Push envelopes when a
// record changes.
//
// The seam exists so the same Server can be unit-tested with an
// in-memory sink instead of a real ZAP node.
type PushSink interface {
	Push(ctx context.Context, connID string, frame []byte, encoding string) error
}

// Forward is the parsed envelope handed to the per-path dispatcher.
type Forward struct {
	TenantID    string
	UserID      string
	IsAdmin     bool
	Permissions int64
	Method      string
	Path        string
	ConnID      string
	Headers     []byte
	Body        []byte
}

// Dispatcher routes a Forward envelope to the matching base handler.
// Implementations live with the per-domain handlers (collections,
// records, auth) so this package stays a transport seam, not a router.
type Dispatcher interface {
	Dispatch(ctx context.Context, f Forward) (status uint32, headers, body []byte, err error)
}

// Server adds the HIP-0110 surface to a base instance.
type Server struct {
	logger     luxlog.Logger
	node       *zaplib.Node
	dispatcher Dispatcher

	subsMu sync.RWMutex
	// subs maps collection → set of ConnIDs subscribed to it. Lookup
	// is collection-driven so a record change in collection X only
	// iterates the conns that care about X.
	subs map[string]map[string]struct{}
	// gatewayOf maps ConnID → peer ID of the gateway that owns the
	// conn, so Push frames are routed to the right gateway replica.
	gatewayOf map[string]string
}

// NewServer constructs the HIP-0110 add-on. Caller wires `node` from
// the existing plugins/zap initialization so HIP-0110 and the legacy
// MsgTypeCollections/Records/Auth/Realtime handlers share one socket.
func NewServer(logger luxlog.Logger, node *zaplib.Node, d Dispatcher) (*Server, error) {
	if node == nil {
		return nil, errors.New("base/internal/zap: nil node")
	}
	if d == nil {
		return nil, errors.New("base/internal/zap: nil dispatcher")
	}
	return &Server{
		logger:     logger,
		node:       node,
		dispatcher: d,
		subs:       make(map[string]map[string]struct{}),
		gatewayOf:  make(map[string]string),
	}, nil
}

// Register installs the HIP-0110 handlers on the underlying node.
// Call once during base startup, after the legacy plugins/zap
// handlers have registered their own MsgTypeCollections/Records/Auth/
// Realtime IDs (these IDs do not overlap — HIP-0110 uses 0x1010+).
func (s *Server) Register() {
	s.node.Handle(MsgTypeForward, s.handleForward)
	s.node.Handle(MsgTypeSubscribe, s.handleSubscribe)
}

// handleForward decodes a Forward envelope and delegates to the
// dispatcher. The response envelope shape matches the legacy
// plugins/zap response (status uint32, body bytes) so older gateway
// builds can still call the dispatcher with the older message type.
func (s *Server) handleForward(ctx context.Context, from string, msg *zaplib.Message) (*zaplib.Message, error) {
	r := msg.Root()
	f := Forward{
		TenantID:    r.Text(fwdTenantID),
		UserID:      r.Text(fwdUserID),
		IsAdmin:     r.Bool(fwdIsAdmin),
		Permissions: r.Int64(fwdPermissions),
		Method:      r.Text(fwdMethod),
		Path:        r.Text(fwdPath),
		ConnID:      r.Text(fwdConnID),
		Headers:     r.Bytes(fwdHeaders),
		Body:        r.Bytes(fwdBody),
	}
	status, headers, body, err := s.dispatcher.Dispatch(ctx, f)
	if err != nil {
		s.logger.Warn("forward dispatch error",
			"path", f.Path,
			"err", err,
		)
		return s.buildResponse(500, nil, []byte(err.Error()))
	}
	return s.buildResponse(status, headers, body)
}

// handleSubscribe records a (ConnID, collection) subscription. The
// gateway calls this when a client opens an SSE / WS stream.
func (s *Server) handleSubscribe(ctx context.Context, from string, msg *zaplib.Message) (*zaplib.Message, error) {
	r := msg.Root()
	connID := r.Text(subscribeConnID)
	collection := r.Text(subscribeCollection)
	if connID == "" || collection == "" {
		return s.buildResponse(400, nil, []byte("connId and collection required"))
	}
	s.subsMu.Lock()
	if s.subs[collection] == nil {
		s.subs[collection] = make(map[string]struct{})
	}
	s.subs[collection][connID] = struct{}{}
	s.gatewayOf[connID] = from
	s.subsMu.Unlock()
	s.logger.Debug("subscribe",
		"conn_id", connID,
		"collection", collection,
		"gateway", from,
	)
	return s.buildResponse(200, nil, []byte("ok"))
}

// BroadcastRecord is called by base's record-change pipeline when a
// row in `collection` mutates. It walks the subs table for that
// collection and produces one MsgTypePush envelope per subscribed
// ConnID, sent to the gateway peer that registered the ConnID.
//
// `frame` is the SSE / WebSocket payload, already encoded by the
// caller — base does NOT re-marshal JSON for the push path.
func (s *Server) BroadcastRecord(ctx context.Context, collection string, frame []byte, encoding string) {
	s.subsMu.RLock()
	conns := s.subs[collection]
	if len(conns) == 0 {
		s.subsMu.RUnlock()
		return
	}
	// Snapshot under read lock so we don't hold it across Send.
	type target struct{ connID, gatewayID string }
	targets := make([]target, 0, len(conns))
	for cid := range conns {
		gw := s.gatewayOf[cid]
		if gw == "" {
			continue
		}
		targets = append(targets, target{cid, gw})
	}
	s.subsMu.RUnlock()

	for _, t := range targets {
		msg, err := s.buildPush(t.connID, frame, encoding)
		if err != nil {
			s.logger.Warn("buildPush failed", "conn_id", t.connID, "err", err)
			continue
		}
		if err := s.node.Send(ctx, t.gatewayID, msg); err != nil {
			s.logger.Debug("push send failed; dropping sub",
				"conn_id", t.connID,
				"gateway", t.gatewayID,
				"err", err,
			)
			s.dropConn(t.connID, collection)
		}
	}
}

// dropConn removes a single (connID, collection) registration after a
// push send fails. Called from the broadcast loop without holding any
// lock.
func (s *Server) dropConn(connID, collection string) {
	s.subsMu.Lock()
	if conns := s.subs[collection]; conns != nil {
		delete(conns, connID)
		if len(conns) == 0 {
			delete(s.subs, collection)
		}
	}
	// Only forget gatewayOf when no subscriptions remain anywhere.
	stillSubscribed := false
	for _, conns := range s.subs {
		if _, ok := conns[connID]; ok {
			stillSubscribed = true
			break
		}
	}
	if !stillSubscribed {
		delete(s.gatewayOf, connID)
	}
	s.subsMu.Unlock()
}

// buildResponse mirrors the legacy plugins/zap response shape: a
// single object with status uint32 at field 0 and a Bytes body at
// field 4. Headers are passed via Bytes at field 8 (canonicalized
// header map; gateway decides the wire format).
func (s *Server) buildResponse(status uint32, headers, body []byte) (*zaplib.Message, error) {
	b := zaplib.NewBuilder(64 + len(headers) + len(body))
	ob := b.StartObject(20)
	ob.SetUint32(0, status)
	ob.SetBytes(4, body)
	ob.SetBytes(12, headers)
	ob.FinishAsRoot()
	return zaplib.Parse(b.Finish())
}

// buildPush emits a MsgTypePush envelope. Matches the on-wire format
// declared in github.com/hanzoai/gateway/zap_wire.go.
func (s *Server) buildPush(connID string, frame []byte, encoding string) (*zaplib.Message, error) {
	b := zaplib.NewBuilder(64 + len(frame))
	ob := b.StartObject(pushSlotSize)
	ob.SetText(pushConnID, connID)
	ob.SetBytes(pushFrame, frame)
	ob.SetText(pushEncoding, encoding)
	ob.FinishAsRoot()
	return zaplib.Parse(b.FinishWithFlags(MsgTypePush << 8))
}
