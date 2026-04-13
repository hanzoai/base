package cli

import (
	"strings"
	"testing"
)

func TestHaConfigPath(t *testing.T) {
	t.Parallel()

	got := haConfigPath("ats", EnvMainnet)
	if !strings.HasSuffix(got, "ha-ats-mainnet.yaml") {
		t.Fatalf("expected ha-ats-mainnet.yaml suffix, got %s", got)
	}

	got = haConfigPath("bd", EnvLocal)
	if !strings.HasSuffix(got, "ha-bd-local.yaml") {
		t.Fatalf("expected ha-bd-local.yaml suffix, got %s", got)
	}
}

func TestHaConfigContent(t *testing.T) {
	t.Parallel()

	content := haConfigContent("ats", EnvLocal, "lux", 3)

	if !strings.Contains(content, "daemon: ats") {
		t.Fatalf("expected daemon name in config, got:\n%s", content)
	}
	if !strings.Contains(content, "consensus: lux") {
		t.Fatalf("expected consensus in config, got:\n%s", content)
	}
	if !strings.Contains(content, "replicas: 3") {
		t.Fatalf("expected replicas in config, got:\n%s", content)
	}
	if !strings.Contains(content, "http://127.0.0.1:8090") {
		t.Fatalf("expected first peer in config, got:\n%s", content)
	}
	if !strings.Contains(content, "http://127.0.0.1:8092") {
		t.Fatalf("expected third peer in config, got:\n%s", content)
	}
}

func TestHaConfigContentPubsub(t *testing.T) {
	t.Parallel()

	content := haConfigContent("bd", EnvTestnet, "pubsub", 5)

	if !strings.Contains(content, "consensus: pubsub") {
		t.Fatalf("expected pubsub consensus, got:\n%s", content)
	}
	if !strings.Contains(content, "replicas: 5") {
		t.Fatalf("expected 5 replicas, got:\n%s", content)
	}
	if !strings.Contains(content, "http://127.0.0.1:8094") {
		t.Fatalf("expected fifth peer in config, got:\n%s", content)
	}
}
