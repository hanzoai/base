package cli

import (
	"testing"

	"github.com/spf13/cobra"
)

// AddNetworkFlags is called after the root command already exists, and Base
// registers its own --dev. Registering it twice is a cobra panic; replacing it
// would rebind Base's flag to this struct and silently detach it from whatever
// reads it. So the rule is: register what is missing, touch what is there.
func TestAddNetworkFlagsRegistersWhatIsMissing(t *testing.T) {
	cmd := &cobra.Command{Use: "root"}
	var nf NetworkFlags
	AddNetworkFlags(cmd, &nf)

	for name, short := range map[string]string{"mainnet": "m", "testnet": "t", "devnet": "d", "dev": ""} {
		f := cmd.PersistentFlags().Lookup(name)
		if f == nil {
			t.Fatalf("--%s was not registered", name)
		}
		if f.Shorthand != short {
			t.Fatalf("--%s shorthand = %q, want %q", name, f.Shorthand, short)
		}
	}
}

func TestAddNetworkFlagsIsIdempotent(t *testing.T) {
	cmd := &cobra.Command{Use: "root"}
	var nf NetworkFlags
	AddNetworkFlags(cmd, &nf)
	AddNetworkFlags(cmd, &nf) // must not panic on a duplicate registration
	if got := cmd.PersistentFlags().Lookup("mainnet"); got == nil {
		t.Fatal("--mainnet disappeared on the second call")
	}
}

// A --dev that already exists keeps its own binding. This is the case the skip
// was written for: Base's built-in --dev is bound to Base's own variable, and
// rebinding it here would leave that variable permanently false.
func TestAddNetworkFlagsLeavesAnExistingDevAlone(t *testing.T) {
	cmd := &cobra.Command{Use: "root"}
	theirs := false
	cmd.PersistentFlags().BoolVar(&theirs, "dev", false, "base's own dev flag")

	var nf NetworkFlags
	AddNetworkFlags(cmd, &nf)

	if err := cmd.PersistentFlags().Set("dev", "true"); err != nil {
		t.Fatalf("Set(dev): %v", err)
	}
	if !theirs {
		t.Fatal("--dev was rebound: the pre-existing variable no longer receives it")
	}
	if nf.Dev {
		t.Fatal("--dev was rebound to NetworkFlags, detaching the original")
	}
}

// Every env answers every question, including the one that is not a cluster.
// K8sContext returning "" for local is what statuscmd keys on to decide there is
// nothing remote to ask.
func TestEnvAnswersForLocal(t *testing.T) {
	if got := EnvLocal.K8sContext(); got != "" {
		t.Fatalf("EnvLocal.K8sContext() = %q, want empty — local targets no cluster", got)
	}
	if got := EnvLocal.K8sNamespace(); got != "default" {
		t.Fatalf("EnvLocal.K8sNamespace() = %q, want %q", got, "default")
	}
	if got := EnvLocal.DomainSuffix(); got != "" {
		t.Fatalf("EnvLocal.DomainSuffix() = %q, want empty", got)
	}
	if EnvLocal.IsRemote() {
		t.Fatal("EnvLocal.IsRemote() = true")
	}
	// An unknown Env falls through the same default arms rather than panicking.
	unknown := Env("nowhere")
	if got := unknown.K8sNamespace(); got != "default" {
		t.Fatalf("unknown env namespace = %q, want %q", got, "default")
	}
	if got := unknown.DomainSuffix(); got != "" {
		t.Fatalf("unknown env suffix = %q, want empty", got)
	}
}

// EnvURLs is pinned as it stands, and what it stands on is a placeholder:
// DomainSuffix is hard-coded to example.com with no override, so every remote
// URL it builds points at a domain nobody owns. Nothing calls it — the CLI names
// its hosts elsewhere — which is the only reason that has never surfaced. Read
// this test as a description, not an endorsement.
func TestEnvURLsBuildsFromTheDomainSuffix(t *testing.T) {
	if got := EnvURLs(EnvLocal, "base", 8090); got != "http://localhost:8090" {
		t.Fatalf("local = %q", got)
	}
	if got := EnvURLs(EnvMainnet, "base", 8090); got != "https://base.example.com" {
		t.Fatalf("mainnet = %q — the placeholder suffix changed; give it a real source", got)
	}
}
