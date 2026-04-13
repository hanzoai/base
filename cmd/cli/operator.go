package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

// DefaultOperatorNetworkDir is the path to the operator network YAMLs.
// Resolved relative to the user's checkout of ~/work/liquidity/operator.
const DefaultOperatorNetworkDir = "k8s/networks"

// NewOperatorCommand returns the `operator` subcommand tree for managing
// Liquidity K8s operator CRDs (liquid.network/v1alpha1).
func NewOperatorCommand(nf *NetworkFlags) *cobra.Command {
	var operatorDir string

	cmd := &cobra.Command{
		Use:   "operator",
		Short: "Manage Liquidity K8s operator CRDs",
		Long: `Manage the Liquidity Kubernetes operator (liquid.network/v1alpha1).

Wraps kubectl to apply, inspect, and upgrade CRDs managed by the
Liquidity operator. Respects --mainnet/--testnet/--devnet flags for
kubectl context selection.`,
	}

	home, _ := os.UserHomeDir()
	defaultDir := filepath.Join(home, "work", "liquidity", "operator")
	cmd.PersistentFlags().StringVar(&operatorDir, "operator-dir", defaultDir,
		"path to the liquidity/operator checkout")

	daemonName := filepath.Base(os.Args[0])

	cmd.AddCommand(operatorApplyCmd(nf, &operatorDir))
	cmd.AddCommand(operatorStatusCmd(nf, daemonName))
	cmd.AddCommand(operatorDescribeCmd(nf))
	cmd.AddCommand(operatorUpgradeCmd(nf, daemonName))
	cmd.AddCommand(operatorLogsCmd(nf))

	return cmd
}

func operatorApplyCmd(nf *NetworkFlags, operatorDir *string) *cobra.Command {
	var yes bool

	cmd := &cobra.Command{
		Use:          "apply",
		Short:        "Apply operator network YAML for the target environment",
		Example:      "ats operator apply --testnet --yes",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			env, err := nf.Resolve()
			if err != nil {
				return err
			}
			if !env.IsRemote() {
				return fmt.Errorf("operator apply requires --mainnet, --testnet, or --devnet")
			}

			yamlFile := operatorNetworkYAML(*operatorDir, env)
			if _, err := os.Stat(yamlFile); err != nil {
				return fmt.Errorf("network YAML not found: %s", yamlFile)
			}

			ctx := env.K8sContext()
			kubectlArgs := []string{"--context", ctx, "apply", "-f", yamlFile}

			if !yes {
				fmt.Fprintf(os.Stdout, "[dry-run] kubectl %s\n", strings.Join(kubectlArgs, " "))
				return nil
			}

			c := exec.Command("kubectl", kubectlArgs...)
			c.Stdout = os.Stdout
			c.Stderr = os.Stderr
			return c.Run()
		},
	}

	cmd.Flags().BoolVar(&yes, "yes", false, "execute (default: dry-run)")
	return cmd
}

func operatorStatusCmd(nf *NetworkFlags, daemonName string) *cobra.Command {
	return &cobra.Command{
		Use:          "status",
		Short:        "Show operator-managed CRD resources",
		Example:      "ats operator status --testnet",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			env, err := nf.Resolve()
			if err != nil {
				return err
			}
			if !env.IsRemote() {
				fmt.Fprintln(os.Stdout, "operator status is only available for remote environments (--mainnet/--testnet/--devnet)")
				return nil
			}

			ctx := env.K8sContext()
			ns := env.K8sNamespace()

			// Get all liquid.network CRDs in the namespace.
			resources := "liquidnetwork,liquidchain,liquidindexer,liquidexplorer,liquidats,liquidbd,liquidta,liquidiam,liquidkms,liquidgateway"
			kubectlArgs := []string{"--context", ctx, "-n", ns, "get", resources}

			fmt.Fprintf(os.Stdout, "kubectl %s\n\n", strings.Join(kubectlArgs, " "))

			c := exec.Command("kubectl", kubectlArgs...)
			c.Stdout = os.Stdout
			c.Stderr = os.Stderr
			return c.Run()
		},
	}
}

func operatorDescribeCmd(nf *NetworkFlags) *cobra.Command {
	return &cobra.Command{
		Use:          "describe <resource-type> <name>",
		Short:        "Describe a specific operator-managed resource",
		Example:      "ats operator describe liquidnetwork liquidd --testnet",
		Args:         cobra.ExactArgs(2),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			env, err := nf.Resolve()
			if err != nil {
				return err
			}
			if !env.IsRemote() {
				return fmt.Errorf("operator describe requires --mainnet, --testnet, or --devnet")
			}

			ctx := env.K8sContext()
			ns := env.K8sNamespace()

			c := exec.Command("kubectl", "--context", ctx, "-n", ns,
				"describe", args[0], args[1])
			c.Stdout = os.Stdout
			c.Stderr = os.Stderr
			return c.Run()
		},
	}
}

func operatorUpgradeCmd(nf *NetworkFlags, daemonName string) *cobra.Command {
	var yes bool
	var tag string

	cmd := &cobra.Command{
		Use:          "upgrade",
		Short:        "Rolling upgrade via CRD spec bump",
		Example:      "ats operator upgrade --testnet --tag v1.4.0 --yes",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			env, err := nf.Resolve()
			if err != nil {
				return err
			}
			if !env.IsRemote() {
				return fmt.Errorf("operator upgrade requires --mainnet, --testnet, or --devnet")
			}

			ctx := env.K8sContext()
			ns := env.K8sNamespace()

			// Patch the CRD spec.image.tag field.
			patchJSON := fmt.Sprintf(`{"spec":{"image":{"tag":"%s"}}}`, tag)
			kubectlArgs := []string{
				"--context", ctx, "-n", ns,
				"patch", "liquidnetwork", "liquidd",
				"--type", "merge", "-p", patchJSON,
			}

			if !yes {
				fmt.Fprintf(os.Stdout, "[dry-run] kubectl %s\n", strings.Join(kubectlArgs, " "))
				return nil
			}

			c := exec.Command("kubectl", kubectlArgs...)
			c.Stdout = os.Stdout
			c.Stderr = os.Stderr
			return c.Run()
		},
	}

	cmd.Flags().BoolVar(&yes, "yes", false, "execute (default: dry-run)")
	cmd.Flags().StringVar(&tag, "tag", "", "image tag for upgrade (required)")
	_ = cmd.MarkFlagRequired("tag")
	return cmd
}

func operatorLogsCmd(nf *NetworkFlags) *cobra.Command {
	var follow bool

	cmd := &cobra.Command{
		Use:          "logs",
		Short:        "Tail operator controller logs",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			env, err := nf.Resolve()
			if err != nil {
				return err
			}
			if !env.IsRemote() {
				return fmt.Errorf("operator logs requires --mainnet, --testnet, or --devnet")
			}

			ctx := env.K8sContext()
			ns := env.K8sNamespace()

			kubectlArgs := []string{
				"--context", ctx, "-n", ns,
				"logs", "deployment/operator", "--tail=100",
			}
			if follow {
				kubectlArgs = append(kubectlArgs, "-f")
			}

			c := exec.Command("kubectl", kubectlArgs...)
			c.Stdout = os.Stdout
			c.Stderr = os.Stderr
			return c.Run()
		},
	}

	cmd.Flags().BoolVarP(&follow, "follow", "f", false, "follow log output")
	return cmd
}

// operatorNetworkYAML resolves the path to the network YAML for the given env.
func operatorNetworkYAML(operatorDir string, env Env) string {
	var filename string
	switch env {
	case EnvMainnet:
		filename = "mainnet-sfo.yaml"
	case EnvTestnet:
		filename = "testnet.yaml"
	case EnvDevnet:
		filename = "devnet.yaml"
	default:
		filename = "devnet.yaml"
	}
	return filepath.Join(operatorDir, DefaultOperatorNetworkDir, filename)
}
