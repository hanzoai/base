package base

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
)

func TestNew(t *testing.T) {
	// copy os.Args
	originalArgs := make([]string, len(os.Args))
	copy(originalArgs, os.Args)
	defer func() {
		// restore os.Args
		os.Args = originalArgs
	}()

	// change os.Args
	os.Args = os.Args[:1]
	os.Args = append(
		os.Args,
		"--dir=test_dir",
		"--encryptionEnv=test_encryption_env",
		"--debug=true",
	)

	app := New()

	if app == nil {
		t.Fatal("Expected initialized Base instance, got nil")
	}

	if app.RootCmd == nil {
		t.Fatal("Expected RootCmd to be initialized, got nil")
	}

	if app.App == nil {
		t.Fatal("Expected App to be initialized, got nil")
	}

	if app.DataDir() != "test_dir" {
		t.Fatalf("Expected app.DataDir() %q, got %q", "test_dir", app.DataDir())
	}

	if app.EncryptionEnv() != "test_encryption_env" {
		t.Fatalf("Expected app.EncryptionEnv() test_encryption_env, got %q", app.EncryptionEnv())
	}
}

func TestNewWithConfig(t *testing.T) {
	app := NewWithConfig(Config{
		DefaultDataDir:       "test_dir",
		DefaultEncryptionEnv: "test_encryption_env",
		HideStartBanner:      true,
	})

	if app == nil {
		t.Fatal("Expected initialized Base instance, got nil")
	}

	if app.RootCmd == nil {
		t.Fatal("Expected RootCmd to be initialized, got nil")
	}

	if app.App == nil {
		t.Fatal("Expected App to be initialized, got nil")
	}

	if app.hideStartBanner != true {
		t.Fatal("Expected app.hideStartBanner to be true, got false")
	}

	if app.DataDir() != "test_dir" {
		t.Fatalf("Expected app.DataDir() %q, got %q", "test_dir", app.DataDir())
	}

	if app.EncryptionEnv() != "test_encryption_env" {
		t.Fatalf("Expected app.EncryptionEnv() %q, got %q", "test_encryption_env", app.EncryptionEnv())
	}
}

func TestNewWithConfigAndFlags(t *testing.T) {
	// copy os.Args
	originalArgs := make([]string, len(os.Args))
	copy(originalArgs, os.Args)
	defer func() {
		// restore os.Args
		os.Args = originalArgs
	}()

	// change os.Args
	os.Args = os.Args[:1]
	os.Args = append(
		os.Args,
		"--dir=test_dir_flag",
		"--encryptionEnv=test_encryption_env_flag",
		"--debug=false",
	)

	app := NewWithConfig(Config{
		DefaultDataDir:       "test_dir",
		DefaultEncryptionEnv: "test_encryption_env",
		HideStartBanner:      true,
	})

	if app == nil {
		t.Fatal("Expected initialized Base instance, got nil")
	}

	if app.RootCmd == nil {
		t.Fatal("Expected RootCmd to be initialized, got nil")
	}

	if app.App == nil {
		t.Fatal("Expected App to be initialized, got nil")
	}

	if app.hideStartBanner != true {
		t.Fatal("Expected app.hideStartBanner to be true, got false")
	}

	if app.DataDir() != "test_dir_flag" {
		t.Fatalf("Expected app.DataDir() %q, got %q", "test_dir_flag", app.DataDir())
	}

	if app.EncryptionEnv() != "test_encryption_env_flag" {
		t.Fatalf("Expected app.EncryptionEnv() %q, got %q", "test_encryption_env_flag", app.EncryptionEnv())
	}
}

// The ZAP transport's address and mDNS switch come from --zap and --no-mdns
// laid over ZAP_ADDR, and this is where the flag wins or loses.
func TestZapFlags(t *testing.T) {
	// copy os.Args
	originalArgs := make([]string, len(os.Args))
	copy(originalArgs, os.Args)
	defer func() {
		// restore os.Args
		os.Args = originalArgs
	}()

	scenarios := []struct {
		name       string
		env        string
		args       []string
		wantAddr   string
		wantNoMDNS bool
	}{
		{"nothing set", "", []string{"serve"}, "", false},
		{"flags after --http", "", []string{"serve", "--http", "127.0.0.1:18090", "--zap", "127.0.0.1:19652", "--no-mdns"}, "127.0.0.1:19652", true},
		{"flag with =", "", []string{"serve", "--zap=[::1]:19652"}, "[::1]:19652", false},
		{"ZAP_ADDR alone", "127.0.0.1:29652", []string{"serve"}, "127.0.0.1:29652", false},
		{"--zap beats ZAP_ADDR", "0.0.0.0:29652", []string{"serve", "--zap", "127.0.0.1:19652"}, "127.0.0.1:19652", false},
	}

	for _, s := range scenarios {
		t.Run(s.name, func(t *testing.T) {
			t.Setenv("ZAP_ADDR", s.env)
			os.Args = append(originalArgs[:1:1], s.args...)

			config := New().zapConfig()

			if config.Address != s.wantAddr {
				t.Fatalf("Address = %q, want %q", config.Address, s.wantAddr)
			}
			if config.NoMDNS != s.wantNoMDNS {
				t.Fatalf("NoMDNS = %v, want %v", config.NoMDNS, s.wantNoMDNS)
			}
		})
	}
}

func TestSkipBootstrap(t *testing.T) {
	// copy os.Args
	originalArgs := make([]string, len(os.Args))
	copy(originalArgs, os.Args)
	defer func() {
		// restore os.Args
		os.Args = originalArgs
	}()

	tempDir := filepath.Join(os.TempDir(), "temp_data")
	defer os.RemoveAll(tempDir)

	// already bootstrapped
	app0 := NewWithConfig(Config{DefaultDataDir: tempDir})
	app0.Bootstrap()
	if v := app0.skipBootstrap(); !v {
		t.Fatal("[bootstrapped] Expected true, got false")
	}

	// unknown command
	os.Args = os.Args[:1]
	os.Args = append(os.Args, "demo")
	app1 := NewWithConfig(Config{DefaultDataDir: tempDir})
	app1.RootCmd.AddCommand(&cobra.Command{Use: "test"})
	if v := app1.skipBootstrap(); !v {
		t.Fatal("[unknown] Expected true, got false")
	}

	// default flags
	flagScenarios := []struct {
		name  string
		short string
	}{
		{"help", "h"},
		{"version", "v"},
	}

	for _, s := range flagScenarios {
		// base flag
		os.Args = os.Args[:1]
		os.Args = append(os.Args, "--"+s.name)
		app1 := NewWithConfig(Config{DefaultDataDir: tempDir})
		if v := app1.skipBootstrap(); !v {
			t.Fatalf("[--%s] Expected true, got false", s.name)
		}

		// short flag
		os.Args = os.Args[:1]
		os.Args = append(os.Args, "-"+s.short)
		app2 := NewWithConfig(Config{DefaultDataDir: tempDir})
		if v := app2.skipBootstrap(); !v {
			t.Fatalf("[-%s] Expected true, got false", s.short)
		}

		customCmd := &cobra.Command{Use: "custom"}
		customCmd.PersistentFlags().BoolP(s.name, s.short, false, "")

		// base flag in custom command
		os.Args = os.Args[:1]
		os.Args = append(os.Args, "custom")
		os.Args = append(os.Args, "--"+s.name)
		app3 := NewWithConfig(Config{DefaultDataDir: tempDir})
		app3.RootCmd.AddCommand(customCmd)
		if v := app3.skipBootstrap(); v {
			t.Fatalf("[--%s custom] Expected false, got true", s.name)
		}

		// short flag in custom command
		os.Args = os.Args[:1]
		os.Args = append(os.Args, "custom")
		os.Args = append(os.Args, "-"+s.short)
		app4 := NewWithConfig(Config{DefaultDataDir: tempDir})
		app4.RootCmd.AddCommand(customCmd)
		if v := app4.skipBootstrap(); v {
			t.Fatalf("[-%s custom] Expected false, got true", s.short)
		}
	}
}
