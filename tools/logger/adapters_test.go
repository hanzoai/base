package logger_test

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"

	"github.com/hanzoai/base/tools/logger"
	luxlog "github.com/luxfi/log"
)

func TestSlogAdapterWritesPositionalAttrs(t *testing.T) {
	var buf bytes.Buffer
	base := slog.New(slog.NewJSONHandler(&buf, nil))

	adapter := logger.NewSlog(base)
	adapter.Info("hello", "k", "v", "n", 7)

	out := buf.String()
	if !strings.Contains(out, `"k":"v"`) {
		t.Fatalf("expected k=v attr, got %q", out)
	}
	if !strings.Contains(out, `"n":7`) {
		t.Fatalf("expected n=7 attr, got %q", out)
	}
}

func TestSlogAdapterWithSeedsAttrs(t *testing.T) {
	var buf bytes.Buffer
	base := slog.New(slog.NewJSONHandler(&buf, nil))

	root := logger.NewSlog(base)
	child := root.With("component", "x")
	child.Warn("boom")

	out := buf.String()
	if !strings.Contains(out, `"component":"x"`) {
		t.Fatalf("expected seeded component=x, got %q", out)
	}
	if !strings.Contains(out, `"level":"WARN"`) {
		t.Fatalf("expected WARN level, got %q", out)
	}
}

func TestLuxfiAdapterSmoke(t *testing.T) {
	// The luxfi adapter is a pass-through; just exercise the Logger
	// surface to be sure the methods are wired and don't panic.
	l := logger.NewLuxfi(luxlog.New())
	l.Debug("d", "k", "v")
	l.Info("i", "k", "v")
	l.Warn("w", "k", "v")
	l.Error("e", "k", "v")
	if child := l.With("component", "test"); child == nil {
		t.Fatalf("With returned nil")
	}
}
