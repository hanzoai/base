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

	// ShardKey names the identity carried by requests (JWT claim or header).
	// Required when Enabled.
	ShardKey string

	// Replication is the number of members holding each shard. 1 = standalone
	// DAG (durability via archive), 2 = pair, 3+ = Byzantine-safe quorum.
	Replication int

	// Peers is the static seed list. In k8s these are pod-ordinal DNS names
	// emitted by the operator; in compose they are static service names.
	// host:port form, p2p port not HTTP port.
	Peers []string

	// NodeID is the local member identity. Defaults to $HOSTNAME; overridable
	// via BASE_NODE_ID for tests and compose.
	NodeID string

	// Role is "validator" (default) or "archive". Archive nodes don't vote;
	// they subscribe to finalized frames and append to cold storage.
	Role NodeRole

	// Archive is the cold-storage URL or "off". s3://bucket/prefix,
	// gs://bucket/prefix, file://path (dev), off.
	Archive string

	// ListenHTTP is the Base HTTP listen address. Used only for the
	// /-/base/members endpoint by the Gateway; main HTTP comes from core.
	ListenHTTP string

	// ListenP2P is the Quasar peer-to-peer port.
	ListenP2P string

	// TLS is the mTLS config for the Quasar p2p transport (R5). When
	// unset the transport runs without authentication — only acceptable
	// in single-node dev and tests. Production callers inject certs via
	// the KMS plugin (base/plugins/kms) or the operator-emitted dev CA.
	// See transport_tls.go for the pinning rules.
	TLS *TLSConfig
}

// NodeRole distinguishes voters from witnesses.
type NodeRole string

const (
	RoleValidator NodeRole = "validator"
	RoleArchive   NodeRole = "archive"
)

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

	return cfg, cfg.validate()
}

func (c Config) validate() error {
	if !c.Enabled {
		return nil
	}
	if strings.TrimSpace(c.ShardKey) == "" {
		return fmt.Errorf("BASE_SHARD_KEY required when BASE_NETWORK=quasar")
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
