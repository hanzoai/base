package cmd

import (
	"github.com/hanzoai/base/cmd/cli"
	"github.com/spf13/cobra"
)

// Version is set at build time via ldflags.
var Version = "0.1.0"

// NewCLICommand returns the top-level Cobra command tree for the Base CLI
// client. These subcommands hit the running server's HTTP API, so they work
// against any Base-backed daemon (base, atsd, brokerd, etc.) by URL.
//
// Usage from a downstream daemon:
//
//	rootCmd.AddCommand(cmd.NewCLICommand())
func NewCLICommand() *cobra.Command {
	var flagURL string
	var flagToken string
	var flagTenant string
	var flagFormat string
	var nf cli.NetworkFlags

	// lazy constructors — resolved after flags are parsed
	clientFn := func() *cli.Client {
		return cli.NewClient(
			cli.ResolveURL(flagURL),
			cli.ResolveToken(flagToken),
			flagTenant,
		)
	}
	formatFn := func() cli.Format {
		return cli.DetectFormat(flagFormat)
	}

	root := &cobra.Command{
		Use:   "cli",
		Short: "HTTP client commands against a running Base server",
		Long: `CLI commands that hit the Base HTTP API.

Works against any Base-backed daemon (ats, bd, ta, etc.)
by pointing --url at the running instance.`,
	}

	root.PersistentFlags().StringVar(&flagURL, "url", "", "server URL (default $BASE_URL or http://127.0.0.1:8090)")
	root.PersistentFlags().StringVar(&flagToken, "token", "", "auth token (default $BASE_TOKEN or ~/.config/base/token)")
	root.PersistentFlags().StringVar(&flagTenant, "tenant", "", "tenant org ID (sets X-Org-Id header)")
	root.PersistentFlags().StringVar(&flagFormat, "format", "", "output format: table, json, yaml (default: table on tty, json otherwise)")

	// Network flags: --mainnet, --testnet, --devnet, --dev
	// Safe on the `cli` subtree (no collision with Base root --dev flag).
	cli.AddNetworkFlags(root, &nf)

	// Base CRUD commands
	root.AddCommand(cli.NewCollectionCommand(clientFn, formatFn))
	root.AddCommand(cli.NewRecordCommand(clientFn, formatFn))
	root.AddCommand(cli.NewLoginCommand(clientFn, formatFn))
	root.AddCommand(cli.NewWhoamiCommand(clientFn, formatFn))
	root.AddCommand(cli.NewCronsCommand(clientFn, formatFn))
	root.AddCommand(cli.NewDaemonCommand())

	// Infrastructure commands (cluster, operator, config, status, self, rpc)
	root.AddCommand(cli.NewClusterCommand(&nf))
	root.AddCommand(cli.NewOperatorCommand(&nf))
	root.AddCommand(cli.NewConfigCommand())
	root.AddCommand(cli.NewStatusCommand(clientFn, formatFn, &nf))
	root.AddCommand(cli.NewSelfCommand(Version))
	root.AddCommand(cli.NewRPCCommand(clientFn, formatFn))

	return root
}

// AddCLISubcommands registers the core Base CLI subcommands (collection,
// record, login, whoami, crons, daemon) directly onto the given parent
// command. Use this instead of NewCLICommand when you want the commands
// at root level (e.g. `ats collection list` instead of `ats cli collection list`).
//
// Does NOT add network flags (--mainnet, --testnet, --devnet, --dev) or
// infrastructure commands (cluster, operator, config, status, self, rpc)
// because the parent's root typically already has a --dev flag from Base.
func AddCLISubcommands(parent *cobra.Command) {
	var flagURL string
	var flagToken string
	var flagTenant string
	var flagFormat string

	parent.PersistentFlags().StringVar(&flagURL, "url", "", "server URL (default $BASE_URL or http://127.0.0.1:8090)")
	parent.PersistentFlags().StringVar(&flagToken, "token", "", "auth token (default $BASE_TOKEN or ~/.config/base/token)")
	parent.PersistentFlags().StringVar(&flagTenant, "tenant", "", "tenant org ID (sets X-Org-Id header)")
	parent.PersistentFlags().StringVar(&flagFormat, "format", "", "output format: table, json, yaml (default: table on tty, json otherwise)")

	clientFn := func() *cli.Client {
		return cli.NewClient(
			cli.ResolveURL(flagURL),
			cli.ResolveToken(flagToken),
			flagTenant,
		)
	}
	formatFn := func() cli.Format {
		return cli.DetectFormat(flagFormat)
	}

	parent.AddCommand(cli.NewCollectionCommand(clientFn, formatFn))
	parent.AddCommand(cli.NewRecordCommand(clientFn, formatFn))
	parent.AddCommand(cli.NewLoginCommand(clientFn, formatFn))
	parent.AddCommand(cli.NewWhoamiCommand(clientFn, formatFn))
	parent.AddCommand(cli.NewCronsCommand(clientFn, formatFn))
	parent.AddCommand(cli.NewDaemonCommand())
}
