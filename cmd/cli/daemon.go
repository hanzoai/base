package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/spf13/cobra"
)

// NewDaemonCommand returns the `daemon` subcommand tree for managing the
// daemon process lifecycle. Works in two modes:
//
//   - Local: spawns/kills the process directly (default when no --env flag).
//   - K8s: delegates to kubectl for rollout management (when --env is set).
//
// The daemon name is derived from os.Args[0] base name.
func NewDaemonCommand() *cobra.Command {
	var env string
	var yes bool

	daemonName := filepath.Base(os.Args[0])

	cmd := &cobra.Command{
		Use:   "daemon",
		Short: "Manage the " + daemonName + " daemon process",
	}

	cmd.PersistentFlags().StringVar(&env, "env", "", "target environment: dev, test, main (enables K8s mode)")
	cmd.PersistentFlags().BoolVar(&yes, "yes", false, "execute destructive actions (default: dry-run)")

	cmd.AddCommand(daemonStartCmd(daemonName, &env, &yes))
	cmd.AddCommand(daemonStopCmd(daemonName, &env, &yes))
	cmd.AddCommand(daemonStatusCmd(daemonName, &env))
	cmd.AddCommand(daemonLogsCmd(daemonName, &env))
	cmd.AddCommand(daemonRestartCmd(daemonName, &env, &yes))

	return cmd
}

func daemonStartCmd(name string, env *string, yes *bool) *cobra.Command {
	return &cobra.Command{
		Use:          "start",
		Short:        "Start the daemon (local: nohup, K8s: rollout restart)",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if *env != "" {
				return k8sRolloutRestart(name, *env, *yes)
			}
			return localStart(name)
		},
	}
}

func daemonStopCmd(name string, env *string, yes *bool) *cobra.Command {
	return &cobra.Command{
		Use:          "stop",
		Short:        "Stop the daemon (local: kill, K8s: scale to 0)",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if *env != "" {
				return k8sScale(name, *env, 0, *yes)
			}
			return localStop(name)
		},
	}
}

func daemonStatusCmd(name string, env *string) *cobra.Command {
	return &cobra.Command{
		Use:          "status",
		Short:        "Show daemon status (local: pgrep, K8s: get pods)",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if *env != "" {
				return k8sStatus(name, *env)
			}
			return localStatus(name)
		},
	}
}

func daemonLogsCmd(name string, env *string) *cobra.Command {
	var follow bool

	cmd := &cobra.Command{
		Use:          "logs",
		Short:        "Show daemon logs (local: log file, K8s: kubectl logs)",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if *env != "" {
				return k8sLogs(name, *env, follow)
			}
			return localLogs(name, follow)
		},
	}

	cmd.Flags().BoolVarP(&follow, "follow", "f", false, "follow log output")
	return cmd
}

func daemonRestartCmd(name string, env *string, yes *bool) *cobra.Command {
	return &cobra.Command{
		Use:          "restart",
		Short:        "Restart the daemon (local: stop+start, K8s: rollout restart)",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if *env != "" {
				return k8sRolloutRestart(name, *env, *yes)
			}
			if err := localStop(name); err != nil {
				// Not running is fine for restart.
				fmt.Fprintf(os.Stderr, "stop: %v (continuing with start)\n", err)
			}
			return localStart(name)
		},
	}
}

// --- Local mode ---

func localStart(name string) error {
	// Find the binary.
	bin, err := exec.LookPath(name)
	if err != nil {
		// Try current directory.
		bin = "./" + name
		if _, err := os.Stat(bin); err != nil {
			return fmt.Errorf("binary %q not found in PATH or current directory", name)
		}
	}

	logFile := filepath.Join(os.TempDir(), name+".log")
	f, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return fmt.Errorf("open log file %s: %w", logFile, err)
	}

	proc := exec.Command(bin, "serve")
	proc.Stdout = f
	proc.Stderr = f
	proc.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	if err := proc.Start(); err != nil {
		f.Close()
		return fmt.Errorf("start %s: %w", name, err)
	}

	fmt.Fprintf(os.Stdout, "%s started (pid %d), logs: %s\n", name, proc.Process.Pid, logFile)
	return nil
}

func localStop(name string) error {
	out, err := exec.Command("pgrep", "-f", name+" serve").Output()
	if err != nil {
		return fmt.Errorf("%s is not running", name)
	}

	pids := strings.Fields(strings.TrimSpace(string(out)))
	for _, pid := range pids {
		if err := exec.Command("kill", pid).Run(); err != nil {
			return fmt.Errorf("kill pid %s: %w", pid, err)
		}
		fmt.Fprintf(os.Stdout, "killed %s (pid %s)\n", name, pid)
	}
	return nil
}

func localStatus(name string) error {
	out, err := exec.Command("pgrep", "-af", name+" serve").Output()
	if err != nil {
		fmt.Fprintln(os.Stdout, name+": not running")
		return nil
	}
	fmt.Fprint(os.Stdout, string(out))
	return nil
}

func localLogs(name string, follow bool) error {
	logFile := filepath.Join(os.TempDir(), name+".log")
	if _, err := os.Stat(logFile); err != nil {
		return fmt.Errorf("no log file at %s", logFile)
	}

	args := []string{logFile}
	bin := "cat"
	if follow {
		bin = "tail"
		args = []string{"-f", logFile}
	}

	c := exec.Command(bin, args...)
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	return c.Run()
}

// --- K8s mode ---

func k8sContext(env string) string {
	switch env {
	case "dev":
		return "gke_devnet"
	case "test":
		return "gke_testnet"
	case "main":
		return "gke_mainnet"
	default:
		return ""
	}
}

func k8sNamespace(env string) string {
	e, err := parseEnv(env)
	if err != nil {
		return "default"
	}
	return e.K8sNamespace()
}

func k8sRolloutRestart(name, env string, execute bool) error {
	ctx := k8sContext(env)
	if ctx == "" {
		return fmt.Errorf("unknown environment: %s", env)
	}

	args := []string{"--context", ctx, "-n", k8sNamespace(env), "rollout", "restart", "deployment/" + name}

	if !execute {
		fmt.Fprintf(os.Stdout, "[dry-run] kubectl %s\n", strings.Join(args, " "))
		return nil
	}

	c := exec.Command("kubectl", args...)
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	return c.Run()
}

func k8sScale(name, env string, replicas int, execute bool) error {
	ctx := k8sContext(env)
	if ctx == "" {
		return fmt.Errorf("unknown environment: %s", env)
	}

	args := []string{"--context", ctx, "-n", k8sNamespace(env), "scale", "deployment/" + name, fmt.Sprintf("--replicas=%d", replicas)}

	if !execute {
		fmt.Fprintf(os.Stdout, "[dry-run] kubectl %s\n", strings.Join(args, " "))
		return nil
	}

	c := exec.Command("kubectl", args...)
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	return c.Run()
}

func k8sStatus(name, env string) error {
	ctx := k8sContext(env)
	if ctx == "" {
		return fmt.Errorf("unknown environment: %s", env)
	}

	c := exec.Command("kubectl", "--context", ctx, "-n", k8sNamespace(env), "get", "pods", "-l", "app="+name)
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	return c.Run()
}

func k8sLogs(name, env string, follow bool) error {
	ctx := k8sContext(env)
	if ctx == "" {
		return fmt.Errorf("unknown environment: %s", env)
	}

	args := []string{"--context", ctx, "-n", k8sNamespace(env), "logs", "deployment/" + name}
	if follow {
		args = append(args, "-f")
	}

	c := exec.Command("kubectl", args...)
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	return c.Run()
}
