// Copyright (c) 2025, Hanzo Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.
//
// ZAP-backed transport for the base/network Quasar plane.
//
// This is the production Transport. It wires base/network Envelopes onto a
// `github.com/luxfi/zap` Node: one TCP listener on BASE_LISTEN_P2P (default
// :9651), explicit peer dial to every entry in BASE_PEERS (no mDNS in K8s),
// and a single registered handler for envelope delivery.
//
// The envelope payload is the output of `Frame.encode()` — the shardID is
// already embedded in the frame, so the wire carries the frame bytes and
// nothing else. Decode is the inverse. One Envelope in, one ZAP message
// out (no fan-out inside the transport; the Quasar shard engine dedupes).
//
// Scale 1 → N:
//   - N=1 (singleton): cfg.Peers is just self. Broadcast is a no-op (no peers
//     connected); WAL hook still installs. Zero cross-pod traffic, but the
//     path is identical — no "standalone vs network" branch for callers.
//   - N≥2: every peer dials every other peer on Start. Quasar writer-pin
//     sorts NodeID, lowest wins the write lock; followers relay through
//     the router; replication factor caps quorum at ⌈k/2⌉+1 ≤ len(Peers)+1.

package network

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/luxfi/zap"
)

// zapEnvelopeMsgType is the ZAP handler key for envelope delivery. ZAP
// dispatches on `msg.Flags() >> 8` — the high 8 bits of the 16-bit flags
// field — so the effective space is uint8. 0xBE = "Base Envelope".
const zapEnvelopeMsgType uint8 = 0xBE

// zapServiceType is the mDNS service identifier. mDNS is disabled on the
// production Node (K8s pods rely on BASE_PEERS, not link-local), but the
// type string is still required by luxfi/zap's NodeConfig.
const zapServiceType = "_hanzo-base._tcp"

// dialRetryInterval is how often Start's background reconnector tries to
// dial peers that weren't reachable at Start time. K8s pod DNS may resolve
// slowly during a StatefulSet bring-up, so we keep reattempting rather
// than failing the whole Start path.
const dialRetryInterval = 3 * time.Second

// zapTransport implements Transport using luxfi/zap's Node as the peer
// plane. Constructed once per base/network.Node and owned for the lifetime
// of the process.
type zapTransport struct {
	node  *zap.Node
	cfg   Config
	self  string
	peers []string

	logger *slog.Logger

	recv   atomic.Value // func(Envelope) — set once by Start, read by handler
	ctx    context.Context
	cancel context.CancelFunc

	// reconnectOnce ensures only one reconnect loop runs even if Start is
	// called and then Stop and then Start again on the same transport
	// instance (defensive — base/network today constructs + starts once).
	reconnectOnce sync.Once
}

// newZapTransport builds a zapTransport from a validated Config. It does
// NOT bind the listener — Start does. Construction is cheap so the base
// node can embed the transport before the base app has finished booting.
func newZapTransport(cfg Config) *zapTransport {
	port := portFromListen(cfg.ListenP2P)

	logger := slog.Default().With("component", "base-network", "transport", "zap", "nodeID", cfg.NodeID)

	// luxfi/zap uses mDNS by default for peer discovery; K8s pods don't
	// get link-local multicast so we disable it and rely on the explicit
	// BASE_PEERS list below.
	node := zap.NewNode(zap.NodeConfig{
		NodeID:      cfg.NodeID,
		ServiceType: zapServiceType,
		Port:        port,
		NoDiscovery: true,
		Logger:      logger,
	})

	return &zapTransport{
		node:   node,
		cfg:    cfg,
		self:   cfg.NodeID,
		peers:  append([]string(nil), cfg.Peers...),
		logger: logger,
	}
}

// Start binds the TCP listener, installs the envelope handler, and dials
// every peer. Dial failures are logged and retried in the background; they
// do not fail Start. Returns an error only when the listener itself can't
// bind — that's a fatal misconfiguration (port in use, permission denied).
func (z *zapTransport) Start(ctx context.Context, recv func(Envelope)) error {
	if recv == nil {
		return errors.New("zap-transport: recv callback is required")
	}
	z.recv.Store(recv)

	z.node.Handle(uint16(zapEnvelopeMsgType), z.handle)

	if err := z.node.Start(); err != nil {
		return fmt.Errorf("zap-transport: start node: %w", err)
	}

	z.ctx, z.cancel = context.WithCancel(ctx)

	// Initial peer dial — best-effort, then hand off to the reconnect loop.
	z.dialAllOnce()
	z.reconnectOnce.Do(func() {
		go z.reconnectLoop()
	})

	z.logger.Info("zap-transport started",
		"listen", z.cfg.ListenP2P,
		"peers", len(z.peers),
	)
	return nil
}

// Publish fans out the envelope to every currently-connected peer. Peers
// that aren't yet connected are skipped silently — the reconnect loop will
// catch them up, and Quasar's DAG sync replays missed frames on reconnect.
func (z *zapTransport) Publish(env Envelope) error {
	payload := env.Frame.encode()
	msg, err := buildZapEnvelopeMessage(payload)
	if err != nil {
		return fmt.Errorf("zap-transport: build message: %w", err)
	}

	if z.ctx == nil {
		return errors.New("zap-transport: not started")
	}

	results := z.node.Broadcast(z.ctx, msg)
	if len(results) == 0 {
		// Singleton topology or all peers disconnected — neither is an
		// error condition; local WAL already applied before Publish runs.
		return nil
	}
	// Record per-peer failures at debug; transient errors are normal and
	// the reconnect loop handles recovery.
	for peer, perr := range results {
		if perr != nil {
			z.logger.Debug("publish peer error", "peer", peer, "err", perr)
		}
	}
	return nil
}

// Stop cancels the reconnect loop and tears down the ZAP node. Safe to
// call multiple times.
func (z *zapTransport) Stop(ctx context.Context) error {
	if z.cancel != nil {
		z.cancel()
	}
	z.node.Stop()
	return nil
}

// handle is registered with zap.Node. It decodes the envelope bytes and
// delivers the frame to the shard engine via the recv callback.
func (z *zapTransport) handle(_ context.Context, from string, msg *zap.Message) (*zap.Message, error) {
	payload := extractZapEnvelopePayload(msg)
	if len(payload) == 0 {
		return nil, errors.New("zap-transport: empty envelope payload")
	}
	frame, err := decodeFrame(payload)
	if err != nil {
		z.logger.Debug("decode failed", "peer", from, "err", err)
		return nil, nil // don't propagate decode errors to the peer
	}
	if err := frame.Valid(); err != nil {
		z.logger.Debug("invalid frame dropped", "peer", from, "err", err)
		return nil, nil
	}
	if cb, ok := z.recv.Load().(func(Envelope)); ok && cb != nil {
		cb(Envelope{ShardID: frame.ShardID, Frame: frame})
	}
	return nil, nil
}

// dialAllOnce attempts to ConnectDirect to every peer except self. Errors
// are logged at debug; the reconnect loop will retry.
func (z *zapTransport) dialAllOnce() {
	for _, p := range z.peers {
		if p == z.self {
			continue
		}
		if err := z.node.ConnectDirect(p); err != nil {
			z.logger.Debug("initial peer dial failed (will retry)", "peer", p, "err", err)
		}
	}
}

// reconnectLoop reattempts ConnectDirect for peers that aren't in the
// current connection set. Called once per transport lifetime. Exits when
// ctx is cancelled (Stop).
func (z *zapTransport) reconnectLoop() {
	ticker := time.NewTicker(dialRetryInterval)
	defer ticker.Stop()
	for {
		select {
		case <-z.ctx.Done():
			return
		case <-ticker.C:
			connected := make(map[string]struct{})
			for _, id := range z.node.Peers() {
				connected[id] = struct{}{}
			}
			for _, p := range z.peers {
				if p == z.self {
					continue
				}
				if _, ok := connected[p]; ok {
					continue
				}
				if err := z.node.ConnectDirect(p); err != nil {
					z.logger.Debug("reconnect dial failed", "peer", p, "err", err)
				}
			}
		}
	}
}

// portFromListen parses ":9651" or "0.0.0.0:9651" → 9651. Falls back to
// 9651 on any parse error; validate() already guarantees ListenP2P is a
// well-formed host:port.
func portFromListen(listen string) int {
	listen = strings.TrimSpace(listen)
	_, p, err := net.SplitHostPort(listen)
	if err != nil {
		return 9651
	}
	port, err := strconv.Atoi(p)
	if err != nil || port <= 0 || port > 65535 {
		return 9651
	}
	return port
}

// buildZapEnvelopeMessage wraps the raw frame bytes in a ZAP message with
// Flags = msgType << 8 so the receiving Node dispatches to the envelope
// handler. The body is a single ZAP object with one `bytes` field at
// offset 0; this is the smallest valid ZAP message that carries a byte
// slice without borrowing a full struct schema.
func buildZapEnvelopeMessage(payload []byte) (*zap.Message, error) {
	b := zap.NewBuilder(len(payload) + 64)
	obj := b.StartObject(8) // one bytes field = 4 bytes offset + 4 bytes length
	obj.SetBytes(0, payload)
	obj.FinishAsRoot()
	raw := b.FinishWithFlags(uint16(zapEnvelopeMsgType) << 8)
	msg, err := zap.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("parse self-built message: %w", err)
	}
	return msg, nil
}

// extractZapEnvelopePayload pulls the bytes field at offset 0 out of the
// message root object. Returns nil if the message is malformed.
func extractZapEnvelopePayload(msg *zap.Message) []byte {
	if msg == nil {
		return nil
	}
	root := msg.Root()
	if root.IsNull() {
		return nil
	}
	return root.Bytes(0)
}

// Compile-time assertion: zapTransport satisfies Transport.
var _ Transport = (*zapTransport)(nil)

// Unused but imported — keep binary.LittleEndian reachable for future
// frame-header extensions without re-adding the import.
var _ = binary.LittleEndian
