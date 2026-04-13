package cli

import (
	"strings"
	"testing"
)

func TestOperatorNetworkYAML(t *testing.T) {
	t.Parallel()

	tests := []struct {
		env      Env
		wantFile string
	}{
		{EnvMainnet, "mainnet-sfo.yaml"},
		{EnvTestnet, "testnet.yaml"},
		{EnvDevnet, "devnet.yaml"},
	}

	for _, tt := range tests {
		t.Run(string(tt.env), func(t *testing.T) {
			t.Parallel()
			got := operatorNetworkYAML("/tmp/operator", tt.env)
			if !strings.HasSuffix(got, tt.wantFile) {
				t.Fatalf("expected suffix %s, got %s", tt.wantFile, got)
			}
			if !strings.Contains(got, "k8s/networks") {
				t.Fatalf("expected k8s/networks in path, got %s", got)
			}
		})
	}
}
