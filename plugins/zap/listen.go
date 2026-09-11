package zap

import (
	"fmt"
	"net"
	"strconv"
	"strings"

	zaplib "github.com/luxfi/zap"
)

// nodeConfig decides where the node listens and whether it runs mDNS, given
// the address the HTTP server listens on.
//
// The node answers what the HTTP port answers — plugin.start bridges the same
// handler onto it — so unless Address says otherwise it listens on the HTTP
// host, and a Base kept on loopback keeps ZAP on loopback. It used to listen on
// every interface and announce itself over mDNS whatever --http said, which put
// that surface on the LAN of every Base whose operator had kept HTTP off it.
//
// An address that cannot be read is an error, never a fallback, so a typo can
// only fail to bind; it cannot bind wider than was asked.
func nodeConfig(cfg Config, httpAddr string) (zaplib.NodeConfig, error) {
	addr := strings.TrimSpace(cfg.Address)
	if addr == "" {
		addr = net.JoinHostPort(httpHost(httpAddr), strconv.Itoa(cfg.Port))
	}

	nc := zaplib.NodeConfig{
		NodeID:      cfg.NodeID,
		ServiceType: cfg.ServiceType,
		Address:     addr,
		NoDiscovery: true,
	}

	// A unix socket is unreachable off-host; luxfi/zap never advertises one.
	if zaplib.Network(addr) == "unix" {
		return nc, nil
	}

	host, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		return zaplib.NodeConfig{}, fmt.Errorf("ZAP listen address %q: %w", addr, err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return zaplib.NodeConfig{}, fmt.Errorf("ZAP listen address %q: the port must be a number", addr)
	}

	nc.Port = port // what mDNS advertises, so it must be the port bound
	nc.NoDiscovery = cfg.NoMDNS || isLoopback(host)

	return nc, nil
}

// httpHost is the host part of the HTTP listen address. An address it cannot
// read gives loopback: not knowing where HTTP listens must not open ZAP wider.
func httpHost(httpAddr string) string {
	host, _, err := net.SplitHostPort(httpAddr)
	if err != nil {
		return "127.0.0.1"
	}
	return host
}

// isLoopback reports whether host names this machine's loopback interface.
func isLoopback(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
