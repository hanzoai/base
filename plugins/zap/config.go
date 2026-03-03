// Package zap provides a ZAP binary protocol transport for Hanzo Base.
//
// Instead of HTTP/JSON, clients can communicate with Base using the
// ZAP zero-copy binary protocol for significantly lower latency and
// memory usage.
package zap

import (
	"fmt"
	"os"
)

// Config for the ZAP transport plugin.
type Config struct {
	// Port to listen on for ZAP connections (default 9652).
	Port int

	// ServiceType for mDNS discovery (default "_hanzo-base._tcp").
	ServiceType string

	// NodeID for ZAP peer identification (default hostname-based).
	NodeID string

	// Enabled controls whether the ZAP listener starts (default true).
	Enabled bool
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() Config {
	nodeID, _ := os.Hostname()
	if nodeID == "" {
		nodeID = "base-node"
	}

	port := 9652
	if p := os.Getenv("ZAP_PORT"); p != "" {
		fmt.Sscanf(p, "%d", &port)
	}

	return Config{
		Port:        port,
		ServiceType: "_hanzo-base._tcp",
		NodeID:      nodeID,
		Enabled:     os.Getenv("ZAP_DISABLED") != "true",
	}
}
