package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// Env is a canonical environment name.
type Env string

const (
	EnvMainnet Env = "mainnet"
	EnvTestnet Env = "testnet"
	EnvDevnet  Env = "devnet"
	EnvLocal   Env = "local"
)

// NetworkFlags holds the mutually-exclusive --mainnet/--testnet/--devnet/--dev
// flags. Mirrors the lux/cli pattern: exactly one may be set.
type NetworkFlags struct {
	Mainnet bool
	Testnet bool
	Devnet  bool
	Dev     bool
}

// AddNetworkFlags registers --mainnet, --testnet, --devnet, --dev on the
// given command's persistent flags so they propagate to all subcommands.
// Skips any flag already registered on the command (e.g. PocketBase's
// built-in --dev) so it's safe to call after app.RootCmd is constructed.
func AddNetworkFlags(cmd *cobra.Command, nf *NetworkFlags) {
	if cmd.PersistentFlags().Lookup("mainnet") == nil {
		cmd.PersistentFlags().BoolVarP(&nf.Mainnet, "mainnet", "m", false, "target mainnet")
	}
	if cmd.PersistentFlags().Lookup("testnet") == nil {
		cmd.PersistentFlags().BoolVarP(&nf.Testnet, "testnet", "t", false, "target testnet")
	}
	if cmd.PersistentFlags().Lookup("devnet") == nil {
		cmd.PersistentFlags().BoolVarP(&nf.Devnet, "devnet", "d", false, "target devnet")
	}
	if cmd.PersistentFlags().Lookup("dev") == nil {
		cmd.PersistentFlags().BoolVar(&nf.Dev, "dev", false, "target local dev mode")
	}
}

// Resolve returns the canonical Env. Rules:
//  1. Exactly one flag set -> that env.
//  2. No flags -> $APP_ENV, then $BASE_ENV, then default "local".
//  3. More than one flag -> error.
func (nf *NetworkFlags) Resolve() (Env, error) {
	set := 0
	var resolved Env

	if nf.Mainnet {
		set++
		resolved = EnvMainnet
	}
	if nf.Testnet {
		set++
		resolved = EnvTestnet
	}
	if nf.Devnet {
		set++
		resolved = EnvDevnet
	}
	if nf.Dev {
		set++
		resolved = EnvLocal
	}

	if set > 1 {
		return "", fmt.Errorf("at most one of --mainnet, --testnet, --devnet, --dev may be set")
	}
	if set == 1 {
		return resolved, nil
	}

	// Fallback: env vars.
	if v := os.Getenv("APP_ENV"); v != "" {
		return parseEnv(v)
	}
	if v := os.Getenv("BASE_ENV"); v != "" {
		return parseEnv(v)
	}
	return EnvLocal, nil
}

// IsRemote returns true when the resolved env targets a K8s cluster.
func (e Env) IsRemote() bool {
	return e == EnvMainnet || e == EnvTestnet || e == EnvDevnet
}

// K8sContext returns the GKE kubectl context for this env.
func (e Env) K8sContext() string {
	switch e {
	case EnvMainnet:
		return "gke_mainnet"
	case EnvTestnet:
		return "gke_testnet"
	case EnvDevnet:
		return "gke_devnet"
	default:
		return ""
	}
}

// K8sNamespace returns the K8s namespace for this env.
func (e Env) K8sNamespace() string {
	switch e {
	case EnvMainnet:
		return "liquid-mainnet"
	case EnvTestnet:
		return "liquid-testnet"
	case EnvDevnet:
		return "liquid-devnet"
	default:
		return "default"
	}
}

// DomainSuffix returns the DNS zone suffix for service URLs.
func (e Env) DomainSuffix() string {
	switch e {
	case EnvMainnet:
		return "example.com"
	case EnvTestnet:
		return "test.example.com"
	case EnvDevnet:
		return "dev.example.com"
	default:
		return ""
	}
}

func parseEnv(s string) (Env, error) {
	switch s {
	case "main", "mainnet", "production", "prod":
		return EnvMainnet, nil
	case "test", "testnet", "staging":
		return EnvTestnet, nil
	case "dev", "devnet", "development":
		return EnvDevnet, nil
	case "local":
		return EnvLocal, nil
	default:
		return "", fmt.Errorf("unknown environment: %q (expected mainnet, testnet, devnet, or local)", s)
	}
}

// EnvURLs returns service URLs for the resolved environment.
func EnvURLs(env Env, serviceName string, localPort int) string {
	if env == EnvLocal {
		return fmt.Sprintf("http://localhost:%d", localPort)
	}
	return fmt.Sprintf("https://%s.%s", serviceName, env.DomainSuffix())
}
