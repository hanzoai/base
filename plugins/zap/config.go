// Package zap provides a ZAP binary protocol transport for Hanzo Base.
//
// Instead of HTTP/JSON, clients can communicate with Base using the
// ZAP zero-copy binary protocol for significantly lower latency and
// memory usage.
package zap

import (
	"fmt"
	"os"

	"github.com/hanzoai/base/tools/osutils"
)

// Config for the ZAP transport plugin.
type Config struct {
	// Port to listen on for ZAP connections (default 9999).
	Port int

	// Address is the host:port to listen on, overriding Port (default
	// ZAP_ADDR). Empty means the HTTP server's host on Port, so a Base
	// serving HTTP on loopback serves ZAP on loopback too. ":9999" is every
	// interface, which is only what an operator gets by writing it.
	Address string

	// MDNS turns on mDNS: the node advertises itself on the LAN and dials the
	// peers it finds there (default ZAP_MDNS, else off). Hanzo nodes reach each
	// other through explicit peers, so discovery runs only for an operator who
	// asks for it, and never on a loopback address, where no peer that learned
	// of the node could reach it.
	MDNS bool

	// NoMDNS keeps mDNS off even when MDNS asks for it. It predates MDNS, from
	// when discovery was on by default, and stays so that configs written for
	// v1.5.93 keep meaning what they say.
	NoMDNS bool

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

	port := 9999
	if p := os.Getenv("ZAP_PORT"); p != "" {
		fmt.Sscanf(p, "%d", &port)
	}

	return Config{
		Port:        port,
		Address:     os.Getenv("ZAP_ADDR"),
		MDNS:        osutils.Bool("ZAP_MDNS", false),
		ServiceType: "_hanzo-base._tcp",
		NodeID:      nodeID,
		Enabled:     !osutils.Bool("ZAP_DISABLED", false),
	}
}
