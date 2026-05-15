// Package cmd — iam-user subcommand.
//
// Only useful when the platform plugin is running in embedded IAM
// mode (IAM_MODE=embedded). Lets operators seed a user from the same
// shell that runs the daemon, without going through HTTP.
//
// Usage:
//
//	./base iam-user create z@example.com
//	(prompted for password on stdin)
//
//	# scripted (less safe — leaks via /proc/<pid>/environ):
//	IAM_USER_PASSWORD=...  ./base iam-user create z@example.com

package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/hanzoai/base/core"
	"github.com/hanzoai/base/plugins/platform"
	"github.com/spf13/cobra"
)

// NewIAMUserCommand returns the `iam-user` cobra subtree. Currently
// only `create` is implemented; `list` and `delete` can be layered
// on later if operators ask for them.
func NewIAMUserCommand(app core.App) *cobra.Command {
	root := &cobra.Command{
		Use:   "iam-user",
		Short: "Manage embedded-IAM users (only when IAM_MODE=embedded)",
	}
	root.AddCommand(newIAMUserCreateCmd(app))
	return root
}

func newIAMUserCreateCmd(app core.App) *cobra.Command {
	var name string
	cmd := &cobra.Command{
		Use:   "create <email>",
		Short: "Create a new user in _iam_users (bcrypt cost 12)",
		Args:  cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			email := strings.TrimSpace(args[0])
			if email == "" {
				return fmt.Errorf("email is required")
			}

			password, err := readPassword()
			if err != nil {
				return fmt.Errorf("read password: %w", err)
			}
			if password == "" {
				return fmt.Errorf("password is required")
			}

			rec, err := platform.CreateEmbeddedIAMUser(app, email, password, name)
			if err != nil {
				return err
			}
			fmt.Fprintf(c.OutOrStdout(), "created user id=%s email=%s\n", rec.Id, rec.GetString("email"))
			return nil
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "display name (optional)")
	return cmd
}

// readPassword reads the password from $IAM_USER_PASSWORD if set, or
// from stdin otherwise. Stdin input is NOT echo-suppressed — operators
// running this on a shared tty should set IAM_USER_PASSWORD instead.
// Keeping the dependency surface minimal (stdlib only) outweighs the
// nice-to-have of *-masking; this command runs once during bootstrap.
func readPassword() (string, error) {
	if v := os.Getenv("IAM_USER_PASSWORD"); v != "" {
		return v, nil
	}
	fmt.Fprint(os.Stderr, "Password (visible — set IAM_USER_PASSWORD to suppress prompt): ")
	scanner := bufio.NewScanner(os.Stdin)
	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return "", err
		}
		return "", fmt.Errorf("no password read")
	}
	return strings.TrimRight(scanner.Text(), "\r\n"), nil
}
