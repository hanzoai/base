package cli

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// NewStatusCommand returns the `status` subcommand.
// Matches `lux status` shape: shows daemon health, leader info, etc.
func NewStatusCommand(clientFn func() *Client, formatFn func() Format, nf *NetworkFlags) *cobra.Command {
	return &cobra.Command{
		Use:          "status",
		Short:        "Show daemon health and cluster state",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			env, err := nf.Resolve()
			if err != nil {
				return err
			}

			c := clientFn()

			// Hit health endpoint.
			data, _, err := c.Get("/health")
			if err != nil {
				return fmt.Errorf("health check failed: %w", err)
			}

			// Enrich with env info.
			var health map[string]any
			if err := json.Unmarshal(data, &health); err != nil {
				return Print(os.Stdout, formatFn(), data)
			}

			health["env"] = string(env)
			health["remote"] = env.IsRemote()
			if ctx := env.K8sContext(); ctx != "" {
				health["k8s_context"] = ctx
			}

			enriched, err := json.Marshal(health)
			if err != nil {
				return Print(os.Stdout, formatFn(), data)
			}

			return Print(os.Stdout, formatFn(), json.RawMessage(enriched))
		},
	}
}
