package cli

import (
	"testing"
)

func TestNetworkFlagsResolveSingle(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		nf   NetworkFlags
		want Env
	}{
		{"mainnet", NetworkFlags{Mainnet: true}, EnvMainnet},
		{"testnet", NetworkFlags{Testnet: true}, EnvTestnet},
		{"devnet", NetworkFlags{Devnet: true}, EnvDevnet},
		{"dev", NetworkFlags{Dev: true}, EnvLocal},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := tt.nf.Resolve()
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("expected %s, got %s", tt.want, got)
			}
		})
	}
}

func TestNetworkFlagsResolveMultipleError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		nf   NetworkFlags
	}{
		{"mainnet+testnet", NetworkFlags{Mainnet: true, Testnet: true}},
		{"mainnet+devnet", NetworkFlags{Mainnet: true, Devnet: true}},
		{"testnet+dev", NetworkFlags{Testnet: true, Dev: true}},
		{"all", NetworkFlags{Mainnet: true, Testnet: true, Devnet: true, Dev: true}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := tt.nf.Resolve()
			if err == nil {
				t.Fatal("expected error for multiple flags")
			}
		})
	}
}

func TestNetworkFlagsResolveEnvVar(t *testing.T) {
	// LIQUIDITY_ENV takes precedence over BASE_ENV.
	t.Setenv("LIQUIDITY_ENV", "testnet")
	t.Setenv("BASE_ENV", "mainnet")

	nf := NetworkFlags{}
	got, err := nf.Resolve()
	if err != nil {
		t.Fatal(err)
	}
	if got != EnvTestnet {
		t.Fatalf("expected testnet from LIQUIDITY_ENV, got %s", got)
	}
}

func TestNetworkFlagsResolveBASEEnv(t *testing.T) {
	t.Setenv("LIQUIDITY_ENV", "")
	t.Setenv("BASE_ENV", "main")

	nf := NetworkFlags{}
	got, err := nf.Resolve()
	if err != nil {
		t.Fatal(err)
	}
	if got != EnvMainnet {
		t.Fatalf("expected mainnet from BASE_ENV, got %s", got)
	}
}

func TestNetworkFlagsResolveDefault(t *testing.T) {
	t.Setenv("LIQUIDITY_ENV", "")
	t.Setenv("BASE_ENV", "")

	nf := NetworkFlags{}
	got, err := nf.Resolve()
	if err != nil {
		t.Fatal(err)
	}
	if got != EnvLocal {
		t.Fatalf("expected local default, got %s", got)
	}
}

func TestNetworkFlagsResolveFlagOverridesEnv(t *testing.T) {
	t.Setenv("LIQUIDITY_ENV", "mainnet")

	nf := NetworkFlags{Devnet: true}
	got, err := nf.Resolve()
	if err != nil {
		t.Fatal(err)
	}
	if got != EnvDevnet {
		t.Fatalf("expected devnet from flag, got %s", got)
	}
}

func TestEnvIsRemote(t *testing.T) {
	t.Parallel()

	if !EnvMainnet.IsRemote() {
		t.Fatal("mainnet should be remote")
	}
	if !EnvTestnet.IsRemote() {
		t.Fatal("testnet should be remote")
	}
	if !EnvDevnet.IsRemote() {
		t.Fatal("devnet should be remote")
	}
	if EnvLocal.IsRemote() {
		t.Fatal("local should not be remote")
	}
}

func TestEnvK8sContext(t *testing.T) {
	t.Parallel()

	tests := []struct {
		env  Env
		want string
	}{
		{EnvMainnet, "gke_liquidity-mainnet_us-central1_main"},
		{EnvTestnet, "gke_liquidity-testnet_us-central1_test"},
		{EnvDevnet, "gke_liquidity-devnet_us-central1_dev"},
		{EnvLocal, ""},
	}

	for _, tt := range tests {
		if got := tt.env.K8sContext(); got != tt.want {
			t.Fatalf("%s: expected %q, got %q", tt.env, tt.want, got)
		}
	}
}

func TestEnvK8sNamespace(t *testing.T) {
	t.Parallel()

	if got := EnvMainnet.K8sNamespace(); got != "liquid-mainnet" {
		t.Fatalf("expected liquid-mainnet, got %s", got)
	}
	if got := EnvDevnet.K8sNamespace(); got != "liquid-devnet" {
		t.Fatalf("expected liquid-devnet, got %s", got)
	}
}

func TestParseEnv(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input string
		want  Env
		err   bool
	}{
		{"main", EnvMainnet, false},
		{"mainnet", EnvMainnet, false},
		{"production", EnvMainnet, false},
		{"prod", EnvMainnet, false},
		{"test", EnvTestnet, false},
		{"testnet", EnvTestnet, false},
		{"staging", EnvTestnet, false},
		{"dev", EnvDevnet, false},
		{"devnet", EnvDevnet, false},
		{"development", EnvDevnet, false},
		{"local", EnvLocal, false},
		{"garbage", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			t.Parallel()
			got, err := parseEnv(tt.input)
			if tt.err && err == nil {
				t.Fatal("expected error")
			}
			if !tt.err && err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Fatalf("expected %s, got %s", tt.want, got)
			}
		})
	}
}

func TestEnvURLs(t *testing.T) {
	t.Parallel()

	got := EnvURLs(EnvLocal, "ats", 8090)
	if got != "http://localhost:8090" {
		t.Fatalf("expected localhost URL, got %s", got)
	}

	got = EnvURLs(EnvMainnet, "ats", 8090)
	if got != "https://ats.satschel.com" {
		t.Fatalf("expected mainnet URL, got %s", got)
	}

	got = EnvURLs(EnvTestnet, "ats", 8090)
	if got != "https://ats.test.satschel.com" {
		t.Fatalf("expected testnet URL, got %s", got)
	}

	got = EnvURLs(EnvDevnet, "bd", 8091)
	if got != "https://bd.dev.satschel.com" {
		t.Fatalf("expected devnet URL, got %s", got)
	}
}
