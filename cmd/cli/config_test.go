package cli

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSaveAndLoadToken(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmpDir)

	token := "eyJhbGciOiJIUzI1NiJ9.test.sig"
	if err := SaveToken(token); err != nil {
		t.Fatal(err)
	}

	// verify file permissions
	info, err := os.Stat(filepath.Join(tmpDir, "base", "token"))
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0600 {
		t.Fatalf("expected 0600, got %o", perm)
	}

	got := LoadToken()
	if got != token {
		t.Fatalf("expected %q, got %q", token, got)
	}
}

func TestLoadTokenMissing(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	got := LoadToken()
	if got != "" {
		t.Fatalf("expected empty string for missing token, got %q", got)
	}
}

func TestResolveToken(t *testing.T) {
	// flag takes priority
	if got := ResolveToken("flag-tok"); got != "flag-tok" {
		t.Fatalf("expected flag-tok, got %s", got)
	}

	// env fallback
	t.Setenv("BASE_TOKEN", "env-tok")
	if got := ResolveToken(""); got != "env-tok" {
		t.Fatalf("expected env-tok, got %s", got)
	}
}

func TestResolveURL(t *testing.T) {
	// flag takes priority
	if got := ResolveURL("http://custom:9090"); got != "http://custom:9090" {
		t.Fatalf("expected http://custom:9090, got %s", got)
	}

	// env fallback
	t.Setenv("BASE_URL", "http://env:8080")
	if got := ResolveURL(""); got != "http://env:8080" {
		t.Fatalf("expected http://env:8080, got %s", got)
	}

	// default
	t.Setenv("BASE_URL", "")
	if got := ResolveURL(""); got != "http://127.0.0.1:8090" {
		t.Fatalf("expected http://127.0.0.1:8090, got %s", got)
	}
}
