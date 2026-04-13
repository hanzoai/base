package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"github.com/spf13/cobra"
)

// NewSelfCommand returns the `self` subcommand tree.
// Matches `lux self` shape: manages the binary install.
func NewSelfCommand(version string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "self",
		Short: "Manage the CLI binary installation",
		Long: `Commands for managing the CLI binary.

Similar to lux self / nvm, this lets you check version,
run diagnostics, and update.`,
	}

	cmd.AddCommand(selfVersionCmd(version))
	cmd.AddCommand(selfDoctorCmd())

	return cmd
}

func selfVersionCmd(version string) *cobra.Command {
	return &cobra.Command{
		Use:          "version",
		Short:        "Print version and build info",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			bin := filepath.Base(os.Args[0])
			fmt.Fprintf(os.Stdout, "%s %s (%s/%s)\n", bin, version, runtime.GOOS, runtime.GOARCH)
			return nil
		},
	}
}

func selfDoctorCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "doctor",
		Short:        "Check system dependencies and configuration",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			checks := []struct {
				name string
				bin  string
			}{
				{"kubectl", "kubectl"},
				{"base-ha", "base-ha"},
				{"docker", "docker"},
			}

			allOK := true
			for _, check := range checks {
				path, err := exec.LookPath(check.bin)
				if err != nil {
					fmt.Fprintf(os.Stdout, "  [MISS] %s: not found in PATH\n", check.name)
					allOK = false
				} else {
					fmt.Fprintf(os.Stdout, "  [OK]   %s: %s\n", check.name, path)
				}
			}

			// Check config file.
			cfgPath := configFilePath()
			if _, err := os.Stat(cfgPath); err != nil {
				fmt.Fprintf(os.Stdout, "  [MISS] config: %s (run `config init` to create)\n", cfgPath)
			} else {
				fmt.Fprintf(os.Stdout, "  [OK]   config: %s\n", cfgPath)
			}

			// Check token.
			tok := LoadToken()
			if tok == "" {
				fmt.Fprintf(os.Stdout, "  [MISS] token: not logged in (run `login`)\n")
			} else {
				fmt.Fprintf(os.Stdout, "  [OK]   token: present (%d chars)\n", len(tok))
			}

			if !allOK {
				fmt.Fprintln(os.Stdout, "\nSome dependencies are missing. Install them before using cluster/operator commands.")
			} else {
				fmt.Fprintln(os.Stdout, "\nAll checks passed.")
			}

			return nil
		},
	}
}
