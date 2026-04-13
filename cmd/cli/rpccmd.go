package cli

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// NewRPCCommand returns the `rpc` subcommand.
// Matches `lux rpc` shape: direct API call passthrough.
func NewRPCCommand(clientFn func() *Client, formatFn func() Format) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "rpc",
		Short: "Direct API call passthrough to the daemon",
		Long: `Make direct API calls to the running Base daemon.

Examples:
  # GET a path
  ats rpc get /health

  # POST with JSON body
  ats rpc post /collections/orders/records '{"symbol":"AAPL","qty":100}'

  # DELETE
  ats rpc delete /collections/orders/records/abc123`,
	}

	cmd.AddCommand(rpcGetCmd(clientFn, formatFn))
	cmd.AddCommand(rpcPostCmd(clientFn, formatFn))
	cmd.AddCommand(rpcPatchCmd(clientFn, formatFn))
	cmd.AddCommand(rpcDeleteCmd(clientFn, formatFn))

	return cmd
}

func rpcGetCmd(clientFn func() *Client, formatFn func() Format) *cobra.Command {
	return &cobra.Command{
		Use:          "get <path>",
		Short:        "GET request",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			data, _, err := clientFn().Get(args[0])
			if err != nil {
				return err
			}
			return Print(os.Stdout, formatFn(), data)
		},
	}
}

func rpcPostCmd(clientFn func() *Client, formatFn func() Format) *cobra.Command {
	return &cobra.Command{
		Use:          "post <path> [json-body]",
		Short:        "POST request",
		Args:         cobra.RangeArgs(1, 2),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			var body any
			if len(args) > 1 {
				if err := json.Unmarshal([]byte(args[1]), &body); err != nil {
					return fmt.Errorf("invalid JSON body: %w", err)
				}
			}
			data, _, err := clientFn().Post(args[0], body)
			if err != nil {
				return err
			}
			return Print(os.Stdout, formatFn(), data)
		},
	}
}

func rpcPatchCmd(clientFn func() *Client, formatFn func() Format) *cobra.Command {
	return &cobra.Command{
		Use:          "patch <path> <json-body>",
		Short:        "PATCH request",
		Args:         cobra.ExactArgs(2),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			var body any
			if err := json.Unmarshal([]byte(args[1]), &body); err != nil {
				return fmt.Errorf("invalid JSON body: %w", err)
			}
			data, _, err := clientFn().Patch(args[0], body)
			if err != nil {
				return err
			}
			return Print(os.Stdout, formatFn(), data)
		},
	}
}

func rpcDeleteCmd(clientFn func() *Client, formatFn func() Format) *cobra.Command {
	return &cobra.Command{
		Use:          "delete <path>",
		Short:        "DELETE request",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			_, _, err := clientFn().Delete(args[0])
			if err != nil {
				return err
			}
			fmt.Fprintln(os.Stdout, "Deleted.")
			return nil
		},
	}
}
