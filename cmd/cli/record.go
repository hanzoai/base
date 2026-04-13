package cli

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// NewRecordCommand returns the `record` subcommand tree.
func NewRecordCommand(clientFn func() *Client, formatFn func() Format) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "record",
		Short: "Manage records in a collection",
	}

	cmd.AddCommand(recordListCmd(clientFn, formatFn))
	cmd.AddCommand(recordGetCmd(clientFn, formatFn))
	cmd.AddCommand(recordCreateCmd(clientFn, formatFn))
	cmd.AddCommand(recordUpdateCmd(clientFn, formatFn))
	cmd.AddCommand(recordDeleteCmd(clientFn, formatFn))

	return cmd
}

func recordListCmd(clientFn func() *Client, formatFn func() Format) *cobra.Command {
	var filter string
	var limit int
	var sortFlag string

	cmd := &cobra.Command{
		Use:          "list <collection>",
		Short:        "List records in a collection",
		Example:      `base record list users --filter "email~'test'" --limit 10 --sort "-created"`,
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			q := BuildQuery(filter, limit, sortFlag, nil)
			data, _, err := clientFn().Get("/collections/" + args[0] + "/records" + q)
			if err != nil {
				return err
			}
			return Print(os.Stdout, formatFn(), data)
		},
	}

	cmd.Flags().StringVar(&filter, "filter", "", "filter expression")
	cmd.Flags().IntVar(&limit, "limit", 0, "max records per page")
	cmd.Flags().StringVar(&sortFlag, "sort", "", "sort expression (e.g. -created)")

	return cmd
}

func recordGetCmd(clientFn func() *Client, formatFn func() Format) *cobra.Command {
	return &cobra.Command{
		Use:          "get <collection> <id>",
		Short:        "Get a single record by ID",
		Args:         cobra.ExactArgs(2),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			data, _, err := clientFn().Get("/collections/" + args[0] + "/records/" + args[1])
			if err != nil {
				return err
			}
			return Print(os.Stdout, formatFn(), data)
		},
	}
}

func recordCreateCmd(clientFn func() *Client, formatFn func() Format) *cobra.Command {
	return &cobra.Command{
		Use:          "create <collection> '<json>'",
		Short:        "Create a new record",
		Example:      `base record create posts '{"title":"Hello","body":"World"}'`,
		Args:         cobra.ExactArgs(2),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			var body any
			if err := json.Unmarshal([]byte(args[1]), &body); err != nil {
				return fmt.Errorf("invalid JSON body: %w", err)
			}
			data, _, err := clientFn().Post("/collections/"+args[0]+"/records", body)
			if err != nil {
				return err
			}
			return Print(os.Stdout, formatFn(), data)
		},
	}
}

func recordUpdateCmd(clientFn func() *Client, formatFn func() Format) *cobra.Command {
	return &cobra.Command{
		Use:          "update <collection> <id> '<json>'",
		Short:        "Update an existing record",
		Example:      `base record update posts abc123 '{"title":"Updated"}'`,
		Args:         cobra.ExactArgs(3),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			var body any
			if err := json.Unmarshal([]byte(args[2]), &body); err != nil {
				return fmt.Errorf("invalid JSON body: %w", err)
			}
			data, _, err := clientFn().Patch("/collections/"+args[0]+"/records/"+args[1], body)
			if err != nil {
				return err
			}
			return Print(os.Stdout, formatFn(), data)
		},
	}
}

func recordDeleteCmd(clientFn func() *Client, formatFn func() Format) *cobra.Command {
	return &cobra.Command{
		Use:          "delete <collection> <id>",
		Short:        "Delete a record",
		Args:         cobra.ExactArgs(2),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			_, _, err := clientFn().Delete("/collections/" + args[0] + "/records/" + args[1])
			if err != nil {
				return err
			}
			fmt.Fprintln(os.Stdout, "Deleted.")
			return nil
		},
	}
}
