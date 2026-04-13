package cli

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/fatih/color"
	"github.com/spf13/cobra"
)

// NewLoginCommand returns the `login` subcommand.
func NewLoginCommand(clientFn func() *Client, formatFn func() Format) *cobra.Command {
	var email string
	var password string
	var superuser bool

	cmd := &cobra.Command{
		Use:          "login",
		Short:        "Authenticate and store a token",
		Example:      "base login --email admin@example.com --password secret123",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if email == "" || password == "" {
				return fmt.Errorf("--email and --password are required")
			}

			collection := "users"
			if superuser {
				collection = "_superusers"
			}

			body := map[string]string{
				"identity": email,
				"password": password,
			}

			c := clientFn()
			data, _, err := c.Post("/collections/"+collection+"/auth-with-password", body)
			if err != nil {
				return fmt.Errorf("login failed: %w", err)
			}

			var resp struct {
				Token string `json:"token"`
			}
			if err := json.Unmarshal(data, &resp); err != nil {
				return fmt.Errorf("parse auth response: %w", err)
			}

			if resp.Token == "" {
				return fmt.Errorf("no token in response")
			}

			if err := SaveToken(resp.Token); err != nil {
				return fmt.Errorf("save token: %w", err)
			}

			color.Green("Logged in. Token saved to %s", tokenPath())
			return nil
		},
	}

	cmd.Flags().StringVar(&email, "email", "", "email / identity")
	cmd.Flags().StringVar(&password, "password", "", "password")
	cmd.Flags().BoolVar(&superuser, "superuser", false, "authenticate as superuser")

	return cmd
}

// NewWhoamiCommand returns the `whoami` subcommand.
func NewWhoamiCommand(clientFn func() *Client, formatFn func() Format) *cobra.Command {
	return &cobra.Command{
		Use:          "whoami",
		Short:        "Show the current authenticated identity",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			c := clientFn()
			if c.Token == "" {
				fmt.Fprintln(os.Stderr, "Not logged in. Run `base login` first.")
				return nil
			}

			// Try superuser health endpoint (returns extra data when authenticated)
			data, _, err := c.Get("/health")
			if err != nil {
				return err
			}
			return Print(os.Stdout, formatFn(), data)
		},
	}
}
