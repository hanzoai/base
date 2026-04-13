package cli

import (
	"testing"
)

func TestNewDaemonCommand(t *testing.T) {
	t.Parallel()

	cmd := NewDaemonCommand()
	if cmd.Use != "daemon" {
		t.Fatalf("expected Use=daemon, got %s", cmd.Use)
	}

	// Verify subcommands exist.
	want := map[string]bool{
		"start":   false,
		"stop":    false,
		"status":  false,
		"logs":    false,
		"restart": false,
	}

	for _, sub := range cmd.Commands() {
		if _, ok := want[sub.Use]; ok {
			want[sub.Use] = true
		}
	}

	for name, found := range want {
		if !found {
			t.Errorf("missing subcommand: %s", name)
		}
	}
}

func TestK8sContext(t *testing.T) {
	t.Parallel()

	tests := []struct {
		env  string
		want string
	}{
		{"dev", "gke_liquidity-devnet_us-central1_dev"},
		{"test", "gke_liquidity-testnet_us-central1_test"},
		{"main", "gke_liquidity-mainnet_us-central1_main"},
		{"unknown", ""},
	}

	for _, tt := range tests {
		got := k8sContext(tt.env)
		if got != tt.want {
			t.Errorf("k8sContext(%q) = %q, want %q", tt.env, got, tt.want)
		}
	}
}
