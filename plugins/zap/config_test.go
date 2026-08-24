package zap

import "testing"

// DefaultConfig is the operator's whole interface to this transport: a port and
// an off-switch. Both are read from the environment, so both are pinned here.
func TestDefaultConfig(t *testing.T) {
	t.Run("defaults when nothing is set", func(t *testing.T) {
		t.Setenv("ZAP_PORT", "")
		t.Setenv("ZAP_DISABLED", "")
		c := DefaultConfig()
		if c.Port != 9999 {
			t.Fatalf("Port = %d, want 9999", c.Port)
		}
		if c.ServiceType != "_hanzo-base._tcp" {
			t.Fatalf("ServiceType = %q", c.ServiceType)
		}
		if !c.Enabled {
			t.Fatal("Enabled = false — the transport is on unless disabled")
		}
		if c.NodeID == "" {
			t.Fatal("NodeID is empty — a node with no id identifies no peer")
		}
	})

	t.Run("ZAP_PORT is honoured", func(t *testing.T) {
		t.Setenv("ZAP_PORT", "4242")
		if got := DefaultConfig().Port; got != 4242 {
			t.Fatalf("Port = %d, want 4242", got)
		}
	})

	t.Run("an unreadable ZAP_PORT keeps the default", func(t *testing.T) {
		t.Setenv("ZAP_PORT", "not-a-port")
		if got := DefaultConfig().Port; got != 9999 {
			t.Fatalf("Port = %d, want 9999 — garbage must not silently bind elsewhere", got)
		}
	})

	// The off-switch takes every spelling of true, which is the point of routing
	// it through osutils.Bool: `ZAP_DISABLED=1` used to leave the listener up.
	for _, raw := range []string{"true", "TRUE", "True", "1", "t"} {
		t.Run("ZAP_DISABLED="+raw+" disables", func(t *testing.T) {
			t.Setenv("ZAP_DISABLED", raw)
			if DefaultConfig().Enabled {
				t.Fatalf("ZAP_DISABLED=%q left the transport enabled", raw)
			}
		})
	}
	for _, raw := range []string{"false", "0", "f", "", "nonsense"} {
		t.Run("ZAP_DISABLED="+raw+" leaves it on", func(t *testing.T) {
			t.Setenv("ZAP_DISABLED", raw)
			if !DefaultConfig().Enabled {
				t.Fatalf("ZAP_DISABLED=%q disabled the transport", raw)
			}
		})
	}
}
