package network

import "context"

// Envelope is what the transport ships between nodes: a shardID header +
// the serialisable frame. Signing and transport security live at the
// transport layer; this struct is the minimal contract.
type Envelope struct {
	ShardID string
	Frame   Frame
}

// Transport is the peer-to-peer plane. Production implementations carry
// envelopes over QUIC/gRPC with mTLS + PQ-identity. The in-process test
// transport ships envelopes through Go channels so the integration test
// runs with no sockets and no Docker.
type Transport interface {
	// Start begins delivering inbound envelopes to recv. It must be
	// idempotent-on-stop (Stop may be called after Start errors).
	Start(ctx context.Context, recv func(Envelope)) error

	// Publish fans out env to every peer in the local node's router view.
	// Best-effort: peers discover missed frames via Quasar's DAG sync on
	// reconnect. Implementations should not block for slow peers.
	Publish(env Envelope) error

	// Stop releases transport resources. Safe to call multiple times.
	Stop(ctx context.Context) error
}

// nopTransport is the default transport when none is injected. It neither
// publishes nor receives — useful for single-node dev runs where Quasar is
// still wanted for the WAL/apply path but no cross-pod traffic happens.
type nopTransport struct{}

func (nopTransport) Start(context.Context, func(Envelope)) error { return nil }
func (nopTransport) Publish(Envelope) error                      { return nil }
func (nopTransport) Stop(context.Context) error                  { return nil }
