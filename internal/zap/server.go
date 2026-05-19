// Copyright © 2026 Hanzo AI. MIT License.

// Package zap adds the HIP-0110 ZAP server surface to Hanzo Base.
//
// Base already speaks ZAP via plugins/zap (Collections=100, Records=101,
// Auth=102, Realtime=103). HIP-0110 adds upper-half handlers that share
// the same socket:
//
//   MsgTypeForward (0x1010)   — gateway → base request. Decoded and
//                               delegated to a pluggable Dispatcher.
//   MsgTypeSubscribe (0x1030) — gateway → base subscription registration.
//                               Future record changes for the collection
//                               produce MsgTypePush envelopes routed to
//                               the right gateway peer.
//
// No new pubsub; reuses base's realtime subscription pattern.
package zap

import (
	"context"
	"errors"
	"sync"

	luxlog "github.com/luxfi/log"
	zaplib "github.com/luxfi/zap"
)

// HIP-0110 message-type IDs. Mirror github.com/hanzoai/gateway/zap_wire.go.
// zap.FinishWithFlags expects the type in the upper byte of a uint16
// (`type << 8`), so types must fit in uint8. Picked in the 0x80+ range
// so they don't collide with base/plugins/zap's lower-byte IDs
// (Collections=100, Records=101, Auth=102, Realtime=103).
const (
	MsgTypeForward   uint16 = 0x80
	MsgTypePush      uint16 = 0x81
	MsgTypeSubscribe uint16 = 0x82
)

// Field offsets — same layout as the gateway side.
const (
	fwdIsAdmin, fwdPermissions           = 0, 4
	fwdTenantID, fwdUserID               = 12, 24
	fwdMethod, fwdPath                   = 36, 48
	fwdConnID                            = 60
	fwdHeaders, fwdBody                  = 72, 84
	fwdSlotSize                          = 96
	pushConnID, pushFrame, pushEncoding  = 0, 12, 24
	pushSlotSize                         = 36
	subscribeConnID, subscribeCollection = 0, 12
	subscribeSlotSize                    = 24
)

// Forward is the parsed envelope handed to the per-path dispatcher.
type Forward struct {
	TenantID, UserID     string
	IsAdmin              bool
	Permissions          int64
	Method, Path, ConnID string
	Headers, Body        []byte
}

// Dispatcher routes a Forward envelope to a matching base handler.
type Dispatcher interface {
	Dispatch(ctx context.Context, f Forward) (status uint32, headers, body []byte, err error)
}

// Server adds the HIP-0110 surface to a base instance.
type Server struct {
	logger     luxlog.Logger
	node       *zaplib.Node
	dispatcher Dispatcher

	subsMu    sync.RWMutex
	subs      map[string]map[string]struct{} // collection → set of ConnIDs
	gatewayOf map[string]string              // ConnID → gateway peer ID
}

// NewServer constructs the HIP-0110 add-on. `node` is reused from base's
// plugins/zap so legacy and HIP-0110 IDs share one socket.
func NewServer(logger luxlog.Logger, node *zaplib.Node, d Dispatcher) (*Server, error) {
	if node == nil {
		return nil, errors.New("base/internal/zap: nil node")
	}
	if d == nil {
		return nil, errors.New("base/internal/zap: nil dispatcher")
	}
	return &Server{
		logger: logger, node: node, dispatcher: d,
		subs:      make(map[string]map[string]struct{}),
		gatewayOf: make(map[string]string),
	}, nil
}

// Register installs the HIP-0110 handlers. Call once during base
// startup, after plugins/zap has registered its lower-half IDs.
func (s *Server) Register() {
	s.node.Handle(MsgTypeForward, s.handleForward)
	s.node.Handle(MsgTypeSubscribe, s.handleSubscribe)
}

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
		s.logger.Warn("forward dispatch error", "path", f.Path, "err", err)
		return s.buildResponse(500, nil, []byte(err.Error()))
	}
	return s.buildResponse(status, headers, body)
}

func (s *Server) handleSubscribe(ctx context.Context, from string, msg *zaplib.Message) (*zaplib.Message, error) {
	r := msg.Root()
	connID, collection := r.Text(subscribeConnID), r.Text(subscribeCollection)
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
	s.logger.Debug("subscribe", "conn_id", connID, "collection", collection, "gateway", from)
	return s.buildResponse(200, nil, []byte("ok"))
}

// BroadcastRecord pushes a pre-encoded SSE / WS frame to every gateway
// holding a conn subscribed to `collection`. Base does NOT re-marshal
// JSON for the push path.
func (s *Server) BroadcastRecord(ctx context.Context, collection string, frame []byte, encoding string) {
	s.subsMu.RLock()
	conns := s.subs[collection]
	if len(conns) == 0 {
		s.subsMu.RUnlock()
		return
	}
	type target struct{ connID, gatewayID string }
	targets := make([]target, 0, len(conns))
	for cid := range conns {
		if gw := s.gatewayOf[cid]; gw != "" {
			targets = append(targets, target{cid, gw})
		}
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
				"conn_id", t.connID, "gateway", t.gatewayID, "err", err)
			s.dropConn(t.connID, collection)
		}
	}
}

func (s *Server) dropConn(connID, collection string) {
	s.subsMu.Lock()
	defer s.subsMu.Unlock()
	if conns := s.subs[collection]; conns != nil {
		delete(conns, connID)
		if len(conns) == 0 {
			delete(s.subs, collection)
		}
	}
	for _, conns := range s.subs {
		if _, ok := conns[connID]; ok {
			return // still subscribed elsewhere; keep gatewayOf
		}
	}
	delete(s.gatewayOf, connID)
}

// buildResponse mirrors plugins/zap response shape: status uint32 at
// offset 0, body Bytes at 4, headers Bytes at 12.
func (s *Server) buildResponse(status uint32, headers, body []byte) (*zaplib.Message, error) {
	b := zaplib.NewBuilder(64 + len(headers) + len(body))
	ob := b.StartObject(20)
	ob.SetUint32(0, status)
	ob.SetBytes(4, body)
	ob.SetBytes(12, headers)
	ob.FinishAsRoot()
	return zaplib.Parse(b.Finish())
}

func (s *Server) buildPush(connID string, frame []byte, encoding string) (*zaplib.Message, error) {
	b := zaplib.NewBuilder(64 + len(frame))
	ob := b.StartObject(pushSlotSize)
	ob.SetText(pushConnID, connID)
	ob.SetBytes(pushFrame, frame)
	ob.SetText(pushEncoding, encoding)
	ob.FinishAsRoot()
	return zaplib.Parse(b.FinishWithFlags(MsgTypePush << 8))
}
