package zap

import (
	"net"
	"strconv"
	"testing"
	"time"
)

// nodeConfig is where the listen address and mDNS are decided, so every way an
// operator can arrive at one is pinned here.
func TestNodeConfig(t *testing.T) {
	scenarios := []struct {
		name     string
		address  string // Config.Address: --zap, else ZAP_ADDR
		mdns     bool   // Config.MDNS: --mdns, else ZAP_MDNS
		noMDNS   bool   // Config.NoMDNS: --no-mdns
		httpAddr string
		wantAddr string
		wantMDNS bool
	}{
		// With no address given, the node takes the HTTP host. mDNS is off
		// unless asked for, wherever the node listens.
		{"loopback HTTP keeps ZAP on loopback", "", false, false, "127.0.0.1:8090", "127.0.0.1:9999", false},
		{"IPv6 loopback HTTP", "", false, false, "[::1]:8090", "[::1]:9999", false},
		{"localhost HTTP", "", false, false, "localhost:8090", "localhost:9999", false},
		{"wildcard HTTP keeps the wildcard, without mDNS", "", false, false, "0.0.0.0:8090", "0.0.0.0:9999", false},
		{"empty-host HTTP is every interface, without mDNS", "", false, false, ":8090", ":9999", false},
		{"LAN HTTP, without mDNS", "", false, false, "192.168.1.5:8090", "192.168.1.5:9999", false},
		{"unreadable HTTP address falls back to loopback", "", false, false, "", "127.0.0.1:9999", false},

		// An address given wins over the HTTP host, in both directions.
		{"address narrower than HTTP", "127.0.0.1:19652", false, false, "0.0.0.0:8090", "127.0.0.1:19652", false},
		{"an explicit wildcard is honoured", ":19652", false, false, "127.0.0.1:8090", ":19652", false},
		{"address is trimmed", " 127.0.0.1:19652 ", false, false, "0.0.0.0:8090", "127.0.0.1:19652", false},

		// --mdns turns discovery on only where a peer could reach the node, and
		// --no-mdns beats it.
		{"mdns on a wildcard", "", true, false, "0.0.0.0:8090", "0.0.0.0:9999", true},
		{"mdns on a LAN address", "192.168.1.5:19652", true, false, "127.0.0.1:8090", "192.168.1.5:19652", true},
		{"mdns never on loopback", "", true, false, "127.0.0.1:8090", "127.0.0.1:9999", false},
		{"no-mdns beats mdns", ":19652", true, true, "127.0.0.1:8090", ":19652", false},
		{"no-mdns alone on a wildcard", "", false, true, "0.0.0.0:8090", "0.0.0.0:9999", false},
	}

	for _, s := range scenarios {
		t.Run(s.name, func(t *testing.T) {
			nc, err := nodeConfig(Config{Port: 9999, Address: s.address, MDNS: s.mdns, NoMDNS: s.noMDNS, NodeID: "n"}, s.httpAddr)
			if err != nil {
				t.Fatalf("nodeConfig: %v", err)
			}
			if nc.Address != s.wantAddr {
				t.Fatalf("Address = %q, want %q", nc.Address, s.wantAddr)
			}
			if mdns := !nc.NoDiscovery; mdns != s.wantMDNS {
				t.Fatalf("mDNS = %v, want %v", mdns, s.wantMDNS)
			}
			_, port, _ := net.SplitHostPort(s.wantAddr)
			if strconv.Itoa(nc.Port) != port {
				t.Fatalf("Port = %d, want %s — mDNS advertises Port, so it must be the port bound", nc.Port, port)
			}
		})
	}

	t.Run("a unix socket is never advertised", func(t *testing.T) {
		nc, err := nodeConfig(Config{Port: 9999, Address: "/tmp/base.zap.sock", MDNS: true}, "0.0.0.0:8090")
		if err != nil {
			t.Fatalf("nodeConfig: %v", err)
		}
		if nc.Address != "/tmp/base.zap.sock" || !nc.NoDiscovery {
			t.Fatalf("Address = %q, NoDiscovery = %v", nc.Address, nc.NoDiscovery)
		}
	})

	// A typo must fail to bind, not bind somewhere else.
	for _, bad := range []string{"19652", "localhost", "127.0.0.1:", "127.0.0.1:zap"} {
		t.Run("rejects "+bad, func(t *testing.T) {
			if nc, err := nodeConfig(Config{Port: 9999, Address: bad}, "0.0.0.0:8090"); err == nil {
				t.Fatalf("accepted %q as %q", bad, nc.Address)
			}
		})
	}
}

// TestStartListensWhereHTTPDoes starts the node the way OnServe does and dials
// it as a LAN peer would, on this machine's non-loopback address. With HTTP on
// loopback nothing answers there; with an explicit wildcard something does,
// which shows the probe can see an exposure at all.
func TestStartListensWhereHTTPDoes(t *testing.T) {
	lan := lanIPv4(t)

	start := func(t *testing.T, cfg Config, httpAddr string) {
		t.Helper()
		p := &plugin{config: cfg}
		p.start(healthHandler(), httpAddr)
		if p.node == nil {
			t.Fatal("ZAP node did not start")
		}
		t.Cleanup(p.stop)
	}

	t.Run("loopback HTTP", func(t *testing.T) {
		port := freePort(t)
		start(t, Config{Port: port, NodeID: "bind-loopback", ServiceType: "_hanzo-base._tcp"}, "127.0.0.1:8090")
		if !dials("127.0.0.1", port) {
			t.Fatal("nothing answers on loopback")
		}
		if dials(lan, port) {
			t.Fatalf("ZAP answers on %s:%d with HTTP on loopback", lan, port)
		}
	})

	t.Run("explicit wildcard", func(t *testing.T) {
		port := freePort(t)
		start(t, Config{Address: ":" + strconv.Itoa(port), NoMDNS: true, NodeID: "bind-wildcard", ServiceType: "_hanzo-base._tcp"}, "127.0.0.1:8090")
		if !dials(lan, port) {
			t.Fatalf("nothing answers on %s:%d with ZAP asked for every interface", lan, port)
		}
	})
}

func dials(host string, port int) bool {
	c, err := net.DialTimeout("tcp", net.JoinHostPort(host, strconv.Itoa(port)), time.Second)
	if err != nil {
		return false
	}
	c.Close()
	return true
}

// lanIPv4 is an address of this machine that is not loopback — the one a LAN
// peer would dial.
func lanIPv4(t *testing.T) string {
	t.Helper()
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		t.Skipf("interface addresses: %v", err)
	}
	for _, a := range addrs {
		if ipn, ok := a.(*net.IPNet); ok {
			if ip := ipn.IP.To4(); ip != nil && !ip.IsLoopback() && !ip.IsLinkLocalUnicast() {
				return ip.String()
			}
		}
	}
	t.Skip("no non-loopback IPv4 address to dial")
	return ""
}
