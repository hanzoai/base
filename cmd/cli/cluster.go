package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

// NewClusterCommand returns the `cluster` subcommand tree for managing
// Base HA groups. Uses the BASE_* env namespace per base-ha conventions.
func NewClusterCommand(nf *NetworkFlags) *cobra.Command {
	var consensus string
	var replicas int

	cmd := &cobra.Command{
		Use:   "cluster",
		Short: "Manage Base HA cluster",
		Long: `Manage Hanzo Base high-availability cluster.

Local mode (--dev): spawns multiple base-ha processes on localhost.
K8s mode (--mainnet/--testnet/--devnet): scales deployment via kubectl.

Uses the BASE_* env namespace (BASE_NODE_ID, BASE_PEERS, BASE_CONSENSUS).`,
	}

	cmd.PersistentFlags().StringVar(&consensus, "consensus", "lux", "consensus mode: lux (default) or pubsub")
	cmd.PersistentFlags().IntVar(&replicas, "replicas", 3, "number of replicas")

	daemonName := filepath.Base(os.Args[0])

	cmd.AddCommand(clusterInitCmd(nf, daemonName, &consensus, &replicas))
	cmd.AddCommand(clusterStartCmd(nf, daemonName, &consensus, &replicas))
	cmd.AddCommand(clusterStopCmd(nf, daemonName))
	cmd.AddCommand(clusterStatusCmd(nf, daemonName))
	cmd.AddCommand(clusterLeaderCmd(nf, daemonName))
	cmd.AddCommand(clusterReplicateCmd(nf, daemonName))
	cmd.AddCommand(clusterFailoverCmd(nf, daemonName))

	return cmd
}

func clusterInitCmd(nf *NetworkFlags, daemonName string, consensus *string, replicas *int) *cobra.Command {
	return &cobra.Command{
		Use:          "init",
		Short:        "Initialize HA cluster config",
		Example:      daemonName + " cluster init --dev --replicas 3 --consensus lux",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			env, err := nf.Resolve()
			if err != nil {
				return err
			}

			cfg := haConfigPath(daemonName, env)
			dir := filepath.Dir(cfg)
			if err := os.MkdirAll(dir, 0700); err != nil {
				return fmt.Errorf("create config dir: %w", err)
			}

			content := haConfigContent(daemonName, env, *consensus, *replicas)
			if err := os.WriteFile(cfg, []byte(content), 0600); err != nil {
				return fmt.Errorf("write config: %w", err)
			}

			fmt.Fprintf(os.Stdout, "Wrote HA config: %s\n", cfg)
			return nil
		},
	}
}

func clusterStartCmd(nf *NetworkFlags, daemonName string, consensus *string, replicas *int) *cobra.Command {
	var yes bool

	cmd := &cobra.Command{
		Use:          "start",
		Short:        "Start HA cluster (local: spawn N processes, K8s: scale deployment)",
		Example:      daemonName + " cluster start --dev --replicas 3",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			env, err := nf.Resolve()
			if err != nil {
				return err
			}

			if env.IsRemote() {
				return k8sClusterScale(daemonName, env, *replicas, yes)
			}
			return localClusterStart(daemonName, *consensus, *replicas, yes)
		},
	}

	cmd.Flags().BoolVar(&yes, "yes", false, "execute (default: dry-run)")
	return cmd
}

func clusterStopCmd(nf *NetworkFlags, daemonName string) *cobra.Command {
	var yes bool

	cmd := &cobra.Command{
		Use:          "stop",
		Short:        "Stop HA cluster",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			env, err := nf.Resolve()
			if err != nil {
				return err
			}

			if env.IsRemote() {
				return k8sClusterScale(daemonName, env, 0, yes)
			}
			return localClusterStop(daemonName, yes)
		},
	}

	cmd.Flags().BoolVar(&yes, "yes", false, "execute (default: dry-run)")
	return cmd
}

func clusterStatusCmd(nf *NetworkFlags, daemonName string) *cobra.Command {
	return &cobra.Command{
		Use:          "status",
		Short:        "Show cluster leader, followers, and txseq lag",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			env, err := nf.Resolve()
			if err != nil {
				return err
			}

			if env.IsRemote() {
				return k8sClusterStatus(daemonName, env)
			}
			return localClusterStatus(daemonName)
		},
	}
}

func clusterLeaderCmd(nf *NetworkFlags, daemonName string) *cobra.Command {
	return &cobra.Command{
		Use:          "leader",
		Short:        "Print the current leader URL",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			env, err := nf.Resolve()
			if err != nil {
				return err
			}

			if env.IsRemote() {
				ctx := env.K8sContext()
				ns := env.K8sNamespace()
				fmt.Fprintf(os.Stdout, "[dry-run] kubectl --context %s -n %s exec deployment/%s -- wget -qO- http://localhost:8090/api/health\n",
					ctx, ns, daemonName)
				return nil
			}

			fmt.Fprintf(os.Stdout, "[local] curl http://localhost:8090/api/health\n")
			return nil
		},
	}
}

func clusterReplicateCmd(nf *NetworkFlags, daemonName string) *cobra.Command {
	var yes bool

	cmd := &cobra.Command{
		Use:          "replicate",
		Short:        "Force resync on followers",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			env, err := nf.Resolve()
			if err != nil {
				return err
			}

			if env.IsRemote() {
				ctx := env.K8sContext()
				ns := env.K8sNamespace()
				cmdStr := fmt.Sprintf("kubectl --context %s -n %s rollout restart statefulset/%s",
					ctx, ns, daemonName)
				if !yes {
					fmt.Fprintf(os.Stdout, "[dry-run] %s\n", cmdStr)
					return nil
				}
				c := exec.Command("kubectl", "--context", ctx, "-n", ns,
					"rollout", "restart", "statefulset/"+daemonName)
				c.Stdout = os.Stdout
				c.Stderr = os.Stderr
				return c.Run()
			}

			fmt.Fprintln(os.Stdout, "[local] Replication is automatic via BASE_PEERS. Restart followers to force resync.")
			return nil
		},
	}

	cmd.Flags().BoolVar(&yes, "yes", false, "execute (default: dry-run)")
	return cmd
}

func clusterFailoverCmd(nf *NetworkFlags, daemonName string) *cobra.Command {
	var yes bool

	cmd := &cobra.Command{
		Use:          "failover",
		Short:        "Cede leadership (ops drill)",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			env, err := nf.Resolve()
			if err != nil {
				return err
			}

			if env.IsRemote() {
				ctx := env.K8sContext()
				ns := env.K8sNamespace()
				// Delete the leader pod -- election promotes the next node.
				cmdStr := fmt.Sprintf("kubectl --context %s -n %s delete pod -l app=%s,role=leader",
					ctx, ns, daemonName)
				if !yes {
					fmt.Fprintf(os.Stdout, "[dry-run] %s\n", cmdStr)
					return nil
				}
				c := exec.Command("kubectl", "--context", ctx, "-n", ns,
					"delete", "pod", "-l", "app="+daemonName+",role=leader")
				c.Stdout = os.Stdout
				c.Stderr = os.Stderr
				return c.Run()
			}

			fmt.Fprintln(os.Stdout, "[local] Kill the leader process. Election will promote the next node by BASE_NODE_ID sort order.")
			return nil
		},
	}

	cmd.Flags().BoolVar(&yes, "yes", false, "execute (default: dry-run)")
	return cmd
}

// --- helpers ---

func haConfigPath(daemonName string, env Env) string {
	dir := configDir()
	return filepath.Join(dir, fmt.Sprintf("ha-%s-%s.yaml", daemonName, env))
}

func haConfigContent(daemonName string, env Env, consensus string, replicas int) string {
	var peers []string
	basePort := 8090
	for i := 0; i < replicas; i++ {
		peers = append(peers, fmt.Sprintf("http://127.0.0.1:%d", basePort+i))
	}

	return fmt.Sprintf(`# Base HA config for %s (%s)
# Generated by %s cluster init
daemon: %s
env: %s
consensus: %s
replicas: %d
base_port: %d
peers:
%s
`, daemonName, env, daemonName, daemonName, env, consensus, replicas, basePort,
		func() string {
			var lines []string
			for _, p := range peers {
				lines = append(lines, "  - "+p)
			}
			return strings.Join(lines, "\n")
		}())
}

func localClusterStart(daemonName string, consensus string, replicas int, execute bool) error {
	basePort := 8090
	var peers []string
	for i := 0; i < replicas; i++ {
		peers = append(peers, fmt.Sprintf("http://127.0.0.1:%d", basePort+i))
	}
	peersCSV := strings.Join(peers, ",")

	for i := 0; i < replicas; i++ {
		port := basePort + i
		nodeID := fmt.Sprintf("%s-node%d", daemonName, i)
		local := fmt.Sprintf("http://127.0.0.1:%d", port)

		envVars := []string{
			fmt.Sprintf("BASE_NODE_ID=%s", nodeID),
			fmt.Sprintf("BASE_LOCAL_TARGET=%s", local),
			fmt.Sprintf("BASE_PEERS=%s", peersCSV),
			fmt.Sprintf("BASE_CONSENSUS=%s", consensus),
		}

		if !execute {
			fmt.Fprintf(os.Stdout, "[dry-run] %s base-ha serve --http 127.0.0.1:%d\n",
				strings.Join(envVars, " "), port)
			continue
		}

		bin, err := exec.LookPath("base-ha")
		if err != nil {
			return fmt.Errorf("base-ha not found in PATH: %w", err)
		}

		logFile := filepath.Join(os.TempDir(), fmt.Sprintf("%s-node%d.log", daemonName, i))
		f, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err != nil {
			return fmt.Errorf("open log file: %w", err)
		}

		proc := exec.Command(bin, "serve", "--http", fmt.Sprintf("127.0.0.1:%d", port))
		proc.Env = append(os.Environ(), envVars...)
		proc.Stdout = f
		proc.Stderr = f

		if err := proc.Start(); err != nil {
			f.Close()
			return fmt.Errorf("start node%d: %w", i, err)
		}
		fmt.Fprintf(os.Stdout, "Started %s (pid %d) on :%d, log: %s\n", nodeID, proc.Process.Pid, port, logFile)
	}

	return nil
}

func localClusterStop(daemonName string, execute bool) error {
	cmdStr := fmt.Sprintf("pkill -f 'base-ha serve'")
	if !execute {
		fmt.Fprintf(os.Stdout, "[dry-run] %s\n", cmdStr)
		return nil
	}

	c := exec.Command("pkill", "-f", "base-ha serve")
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	_ = c.Run() // pkill returns non-zero if no processes found; that's fine
	fmt.Fprintln(os.Stdout, "Stopped all local base-ha processes.")
	return nil
}

func localClusterStatus(daemonName string) error {
	c := exec.Command("pgrep", "-af", "base-ha serve")
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	if err := c.Run(); err != nil {
		fmt.Fprintln(os.Stdout, "No local base-ha processes running.")
	}
	return nil
}

func k8sClusterScale(daemonName string, env Env, replicas int, execute bool) error {
	ctx := env.K8sContext()
	ns := env.K8sNamespace()

	args := []string{
		"--context", ctx, "-n", ns,
		"scale", "deployment/" + daemonName,
		fmt.Sprintf("--replicas=%d", replicas),
	}

	if !execute {
		fmt.Fprintf(os.Stdout, "[dry-run] kubectl %s\n", strings.Join(args, " "))
		return nil
	}

	c := exec.Command("kubectl", args...)
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	return c.Run()
}

func k8sClusterStatus(daemonName string, env Env) error {
	ctx := env.K8sContext()
	ns := env.K8sNamespace()

	c := exec.Command("kubectl", "--context", ctx, "-n", ns,
		"get", "pods", "-l", "app="+daemonName, "-o", "wide")
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	return c.Run()
}
