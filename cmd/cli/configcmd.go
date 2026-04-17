package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

// BaseConfig is the ~/.config/base/config.json schema.
type BaseConfig struct {
	DefaultEnv string               `json:"default_env"`
	Envs       map[string]EnvConfig `json:"envs"`
	DefaultOrg string               `json:"default_org"`
	TokenPath  string               `json:"token_path"`
}

// EnvConfig holds per-environment service URLs.
type EnvConfig struct {
	ATSURL string `json:"ats_url,omitempty"`
	BDURL  string `json:"bd_url,omitempty"`
	TAURL  string `json:"ta_url,omitempty"`
	IAMURL string `json:"iam_url,omitempty"`
}

// configFilePath returns ~/.config/base/config.json, respecting XDG_CONFIG_HOME.
func configFilePath() string {
	return filepath.Join(configDir(), "config.json")
}

// LoadBaseConfig reads config.json. Returns defaults if file is missing.
func LoadBaseConfig() (*BaseConfig, error) {
	data, err := os.ReadFile(configFilePath())
	if err != nil {
		if os.IsNotExist(err) {
			return defaultBaseConfig(), nil
		}
		return nil, fmt.Errorf("read config: %w", err)
	}

	var cfg BaseConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	return &cfg, nil
}

// SaveBaseConfig writes config.json.
func SaveBaseConfig(cfg *BaseConfig) error {
	dir := configDir()
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	return os.WriteFile(configFilePath(), data, 0600)
}

func defaultBaseConfig() *BaseConfig {
	return &BaseConfig{
		DefaultEnv: "dev",
		Envs: map[string]EnvConfig{
			"local": {
				ATSURL: "http://localhost:8090",
				BDURL:  "http://localhost:8091",
				TAURL:  "http://localhost:8092",
				IAMURL: "http://localhost:8093",
			},
			"dev": {
				ATSURL: "https://ats.localhost:8090",
				BDURL:  "https://bd.localhost:8091",
				TAURL:  "https://ta.localhost:8092",
				IAMURL: "https://iam.dev.example.com",
			},
			"test": {
				ATSURL: "https://ats.test.example.com",
				BDURL:  "https://bd.test.example.com",
				TAURL:  "https://ta.test.example.com",
				IAMURL: "https://iam.test.example.com",
			},
			"main": {
				ATSURL: "https://ats.example.com",
				BDURL:  "https://bd.example.com",
				TAURL:  "https://ta.example.com",
				IAMURL: "https://iam.example.com",
			},
		},
		DefaultOrg: "mlc",
		TokenPath:  "~/.config/base/token",
	}
}

// NewConfigCommand returns the `config` subcommand tree.
// Matches `lux config` shape.
func NewConfigCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Manage CLI configuration",
		Long:  "View and update the Base CLI configuration (~/.config/base/config.json).",
	}

	cmd.AddCommand(configShowCmd())
	cmd.AddCommand(configSetEnvCmd())
	cmd.AddCommand(configSetOrgCmd())
	cmd.AddCommand(configInitCmd())

	return cmd
}

func configShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "show",
		Short:        "Print current configuration",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := LoadBaseConfig()
			if err != nil {
				return err
			}
			data, err := json.MarshalIndent(cfg, "", "  ")
			if err != nil {
				return err
			}
			fmt.Fprintln(os.Stdout, string(data))
			return nil
		},
	}
}

func configSetEnvCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "set-env <env>",
		Short:        "Set the default environment (dev, test, main, local)",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			env, err := parseEnv(args[0])
			if err != nil {
				return err
			}

			cfg, err := LoadBaseConfig()
			if err != nil {
				return err
			}

			cfg.DefaultEnv = string(env)
			if err := SaveBaseConfig(cfg); err != nil {
				return err
			}

			fmt.Fprintf(os.Stdout, "Default environment set to: %s\n", env)
			return nil
		},
	}
}

func configSetOrgCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "set-org <org>",
		Short:        "Set the default organization",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := LoadBaseConfig()
			if err != nil {
				return err
			}

			cfg.DefaultOrg = args[0]
			if err := SaveBaseConfig(cfg); err != nil {
				return err
			}

			fmt.Fprintf(os.Stdout, "Default org set to: %s\n", args[0])
			return nil
		},
	}
}

func configInitCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "init",
		Short:        "Write default config file",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			path := configFilePath()
			if _, err := os.Stat(path); err == nil {
				return fmt.Errorf("config already exists at %s (delete it first to reinitialize)", path)
			}

			cfg := defaultBaseConfig()
			if err := SaveBaseConfig(cfg); err != nil {
				return err
			}

			fmt.Fprintf(os.Stdout, "Wrote default config to %s\n", path)
			return nil
		},
	}
}
