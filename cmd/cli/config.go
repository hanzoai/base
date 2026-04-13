package cli

import (
	"fmt"
	"os"
	"path/filepath"
)

// configDir returns the base config directory, respecting XDG_CONFIG_HOME.
func configDir() string {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "base")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".", ".config", "base")
	}
	return filepath.Join(home, ".config", "base")
}

// tokenPath returns the full path to the stored token file.
func tokenPath() string {
	return filepath.Join(configDir(), "token")
}

// SaveToken writes the token to disk with mode 0600.
func SaveToken(token string) error {
	dir := configDir()
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	return os.WriteFile(tokenPath(), []byte(token), 0600)
}

// LoadToken reads the stored token from disk.
// Returns empty string (no error) if the file does not exist.
func LoadToken() string {
	data, err := os.ReadFile(tokenPath())
	if err != nil {
		return ""
	}
	return string(data)
}

// ResolveToken returns the token from (in priority order):
// 1. --token flag
// 2. $BASE_TOKEN env var
// 3. ~/.config/base/token file
func ResolveToken(flagToken string) string {
	if flagToken != "" {
		return flagToken
	}
	if env := os.Getenv("BASE_TOKEN"); env != "" {
		return env
	}
	return LoadToken()
}

// ResolveURL returns the server URL from (in priority order):
// 1. --url flag
// 2. $BASE_URL env var
// 3. http://127.0.0.1:8090
func ResolveURL(flagURL string) string {
	if flagURL != "" {
		return flagURL
	}
	if env := os.Getenv("BASE_URL"); env != "" {
		return env
	}
	return "http://127.0.0.1:8090"
}
