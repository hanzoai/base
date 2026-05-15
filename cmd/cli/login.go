package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// NewLoginCommand returns the `login` subcommand.
//
// The legacy `base login --email --password` flow is gone — Hanzo IAM
// is the only auth source for Base, and it issues tokens via the OIDC
// PKCE flow at /api/iam/oauth/authorize (proxied by the platform
// plugin to IAM_ENDPOINT). The CLI consumes those tokens via the
// `--token` persistent flag or the BASE_TOKEN env var (see config.go).
//
// This command stays bound so `base login --help` returns a useful
// message instead of "unknown command". Running it without args
// prints the IAM-handoff instructions; running with --email/--password
// returns a non-zero exit code with the same instructions.
func NewLoginCommand(clientFn func() *Client, formatFn func() Format) *cobra.Command {
	var email string
	var password string
	var superuser bool

	cmd := &cobra.Command{
		Use:   "login",
		Short: "Obtain an auth token from Hanzo IAM (out-of-band) and pass it via --token",
		Long: "Base no longer accepts a local password. Run the IAM PKCE flow " +
			"(e.g. through hanzo.id or your org's IAM endpoint) to obtain a JWT, " +
			"then pass it to subsequent commands via --token, $BASE_TOKEN, or " +
			"~/.config/base/token.",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Fprintln(os.Stderr,
				"base login: local password auth has been removed. "+
					"Obtain an IAM-issued JWT and pass it via --token or BASE_TOKEN.")
			if email != "" || password != "" {
				return fmt.Errorf("local login is not supported; use IAM and pass --token")
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&email, "email", "", "(removed) use IAM and --token instead")
	cmd.Flags().StringVar(&password, "password", "", "(removed) use IAM and --token instead")
	cmd.Flags().BoolVar(&superuser, "superuser", false, "(removed) use IAM and --token instead")
	_ = cmd.Flags().MarkHidden("email")
	_ = cmd.Flags().MarkHidden("password")
	_ = cmd.Flags().MarkHidden("superuser")

	return cmd
}

// NewWhoamiCommand returns the `whoami` subcommand.
func NewWhoamiCommand(clientFn func() *Client, formatFn func() Format) *cobra.Command {
	return &cobra.Command{
		Use:          "whoami",
		Short:        "Show the current authenticated identity",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			c := clientFn()
			if c.Token == "" {
				fmt.Fprintln(os.Stderr, "Not logged in. Obtain an IAM token and pass --token or set BASE_TOKEN.")
				return nil
			}

			// Try superuser health endpoint (returns extra data when authenticated)
			data, _, err := c.Get("/health")
			if err != nil {
				return err
			}
			return Print(os.Stdout, formatFn(), data)
		},
	}
}
