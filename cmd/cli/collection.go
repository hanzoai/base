package cli

import (
	"os"

	"github.com/spf13/cobra"
)

// NewCollectionCommand returns the `collection` subcommand tree.
func NewCollectionCommand(clientFn func() *Client, formatFn func() Format) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "collection",
		Short: "Manage collections",
	}

	cmd.AddCommand(collectionListCmd(clientFn, formatFn))
	cmd.AddCommand(collectionGetCmd(clientFn, formatFn))
	cmd.AddCommand(collectionSchemaCmd(clientFn, formatFn))

	return cmd
}

func collectionListCmd(clientFn func() *Client, formatFn func() Format) *cobra.Command {
	return &cobra.Command{
		Use:          "list",
		Short:        "List all collections",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			data, _, err := clientFn().Get("/collections")
			if err != nil {
				return err
			}
			return Print(os.Stdout, formatFn(), data)
		},
	}
}

func collectionGetCmd(clientFn func() *Client, formatFn func() Format) *cobra.Command {
	return &cobra.Command{
		Use:          "get <name>",
		Short:        "Show a collection's schema",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			data, _, err := clientFn().Get("/collections/" + args[0])
			if err != nil {
				return err
			}
			return Print(os.Stdout, formatFn(), data)
		},
	}
}

func collectionSchemaCmd(clientFn func() *Client, formatFn func() Format) *cobra.Command {
	return &cobra.Command{
		Use:          "schema <name>",
		Short:        "Export a collection's schema as JSON (pipe to file)",
		Example:      "base collection schema users > schema.json",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			data, _, err := clientFn().Get("/collections/" + args[0])
			if err != nil {
				return err
			}
			// schema always outputs JSON regardless of --format
			return Print(os.Stdout, FormatJSON, data)
		},
	}
}

