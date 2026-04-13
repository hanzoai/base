package cli

import (
	"os"

	"github.com/spf13/cobra"
)

// NewCronsCommand returns the `crons` subcommand tree.
func NewCronsCommand(clientFn func() *Client, formatFn func() Format) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "crons",
		Short: "Manage cron schedules",
	}

	cmd.AddCommand(cronsListCmd(clientFn, formatFn))

	return cmd
}

func cronsListCmd(clientFn func() *Client, formatFn func() Format) *cobra.Command {
	return &cobra.Command{
		Use:          "list",
		Short:        "List registered cron schedules",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			data, _, err := clientFn().Get("/crons")
			if err != nil {
				return err
			}
			return Print(os.Stdout, formatFn(), data)
		},
	}
}
