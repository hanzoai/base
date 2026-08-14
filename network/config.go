package network

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// Config is the parsed env surface for a Base network member. All fields are
// immutable after construction; reparsing requires a restart (standard for
// pod-lifetime config).
type Config struct {
	// Enabled is true iff BASE_NETWORK=quasar.
	Enabled bool

	// ShardKey names the shard a request belongs to, and names its source
	// with it: "header:<Name>" reads that request header, anything else
	// reads that field on the verified identity. Required when Enabled.
	ShardKey string

	// Replication is the number of members holding each shard. 1 = standalone
	// DAG (durability via archive), 2 = pair, 3+ = Byzantine-safe quorum.
	Replication int

	// Peers is the static seed list. In k8s these are pod-ordinal DNS names
	// emitted by the operator; in compose they are static service names.
	// host:port form, p2p port not HTTP port.
	//
	// It is also the trust set. The transport establishes no identity of its
	// own, so membership of this list is what a peer has, and every entry may
	// submit frames for any shard this node owns.
	Peers []string

	// NodeID is the local member identity. Defaults to $HOSTNAME; overridable
	// via BASE_NODE_ID for tests and compose.
	NodeID string

	// Role is "validator" (default) or "archive". Archive nodes don't vote;
	// they subscribe to finalized frames and append to cold storage.
	Role NodeRole

	// Archive is the cold-storage URL or "off". s3://bucket/prefix is
	// the only scheme; see NewArchive in archive.go.
	Archive string

	// ListenHTTP is the Base HTTP listen address. Used only for the
	// /-/base/members endpoint by the Gateway; main HTTP comes from core.
	ListenHTTP string

	// ListenP2P is the Quasar peer-to-peer port.
	ListenP2P string
}

// NodeRole distinguishes voters from witnesses.
type NodeRole string

const (
	RoleValidator NodeRole = "validator"
	RoleArchive   NodeRole = "archive"
)

// ShardKeyHeader is the BASE_SHARD_KEY prefix that names a request header as
// the shard's source, rather than a field on the verified identity. A header
// is whatever the caller sends, so it says the caller picks its own shard —
// which is what compose dev needs, having no token to derive one from. The
// prefix is how an operator says it on purpose.
const ShardKeyHeader = "header:"

// tlsNames are the names this package refuses. They described an mTLS surface
// that was never reached by a request: the transport is luxfi/zap, whose node
// takes one *tls.Config for both the listener it binds and every peer it dials,
// and a config that serves both directions cannot also name the peer it is
// dialling. Certificates had no issuer either. Both halves would have to land
// together — a config threaded through while nothing fills it reads as mTLS and
// is not — so the surface is gone and the names say so.
var tlsNames = []string{
	"BASE_TLS_CA",
	"BASE_TLS_SERVER_CERT",
	"BASE_TLS_SERVER_KEY",
	"BASE_TLS_ALLOWED_SANS",
}

// ConfigFromEnv reads BASE_NETWORK, BASE_SHARD_KEY, BASE_REPLICATION,
// BASE_PEERS, BASE_NODE_ROLE, BASE_ARCHIVE, BASE_LISTEN_HTTP, BASE_LISTEN_P2P,
// and BASE_SHARD_BACKLOG_MAX / BASE_SHARD_BACKLOG_SEGMENTS (R6 per-shard
// backlog caps — the archive config is built separately from these by
// base/core's startup path).
// Standalone defaults are safe: no error, Enabled==false.
func ConfigFromEnv() (Config, error) {
	mode := strings.ToLower(strings.TrimSpace(os.Getenv("BASE_NETWORK")))
	cfg := Config{
		Enabled:    mode == "quasar",
		ShardKey:   os.Getenv("BASE_SHARD_KEY"),
		NodeID:     envOr("BASE_NODE_ID", os.Getenv("HOSTNAME")),
		Role:       NodeRole(envOr("BASE_NODE_ROLE", string(RoleValidator))),
		Archive:    envOr("BASE_ARCHIVE", "off"),
		ListenHTTP: envOr("BASE_LISTEN_HTTP", ":8090"),
		ListenP2P:  envOr("BASE_LISTEN_P2P", ":9999"),
	}

	if v := strings.TrimSpace(os.Getenv("BASE_REPLICATION")); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return Config{}, fmt.Errorf("BASE_REPLICATION=%q: %w", v, err)
		}
		cfg.Replication = n
	} else if cfg.Enabled {
		cfg.Replication = 1
	}

	if v := strings.TrimSpace(os.Getenv("BASE_PEERS")); v != "" {
		for _, p := range strings.Split(v, ",") {
			p = strings.TrimSpace(p)
			if p != "" {
				cfg.Peers = append(cfg.Peers, p)
			}
		}
	}

	if mode != "" && mode != "quasar" && mode != "standalone" {
		return Config{}, fmt.Errorf("BASE_NETWORK=%q: must be 'quasar' or 'standalone'", mode)
	}

	// Encryption at rest is decided by how a database is opened, not here: a
	// per-org shard is opened with a key derived from KMS, and the process
	// database takes its protection from the volume it sits on. Nothing in this
	// package changes either, so a name that reads like a switch for them is
	// refused. An operator who sets it has an expectation about data at rest,
	// and the only honest answers are to meet it or to say so.
	if v := strings.TrimSpace(os.Getenv("BASE_ENCRYPT")); v != "" {
		return Config{}, fmt.Errorf("BASE_ENCRYPT=%q: not a setting. A per-org shard is opened with a key derived from KMS. Settings are encrypted by naming an env var with --encryptionEnv. The process database takes its protection from the volume it is on", v)
	}

	// The peer transport presents no certificate, so these name a property
	// this package does not deliver and are refused for the same reason
	// BASE_ENCRYPT is: an operator who sets one has an expectation about the
	// peer plane, and the only honest answers are to meet it or to say so.
	for _, k := range tlsNames {
		if v := strings.TrimSpace(os.Getenv(k)); v != "" {
			return Config{}, fmt.Errorf("%s=%q: not a setting. Peers authenticate by reachability, so BASE_PEERS is the whole trust set: every host in it may submit frames for any shard this node owns. Keep it to peers of the same service in the same namespace, and decide what reaches BASE_LISTEN_P2P with a NetworkPolicy", k, v)
		}
	}

	return cfg, cfg.validate()
}

func (c Config) validate() error {
	if !c.Enabled {
		return nil
	}
	if strings.TrimSpace(c.ShardKey) == "" {
		return fmt.Errorf("BASE_SHARD_KEY required when BASE_NETWORK=quasar")
	}
	if name, ok := strings.CutPrefix(c.ShardKey, ShardKeyHeader); ok && strings.TrimSpace(name) == "" {
		return fmt.Errorf("BASE_SHARD_KEY=%q: names a header as the shard source without saying which header", c.ShardKey)
	}
	if c.Replication < 1 {
		return fmt.Errorf("BASE_REPLICATION=%d: must be >= 1", c.Replication)
	}
	switch c.Role {
	case RoleValidator, RoleArchive:
	default:
		return fmt.Errorf("BASE_NODE_ROLE=%q: must be 'validator' or 'archive'", c.Role)
	}
	if strings.TrimSpace(c.NodeID) == "" {
		return fmt.Errorf("BASE_NODE_ID or $HOSTNAME must be set when BASE_NETWORK=quasar")
	}
	return nil
}

func envOr(k, def string) string {
	if v := strings.TrimSpace(os.Getenv(k)); v != "" {
		return v
	}
	return def
}
