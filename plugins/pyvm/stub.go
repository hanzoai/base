//go:build !pyvm
// +build !pyvm

// Package pyvm is the CPython (cgo) extension runtime for Base.
//
// The real implementation lives behind the `pyvm` build tag because it
// links libpython at the C level and requires Python 3.12+ development
// headers (pkg-config python-3.13-embed or python-3.12-embed). Default
// base builds compile without it — this stub is what gets linked in
// normally so `NewRuntime()` is always callable, only the engine is
// opt-in via `go build -tags pyvm`.
package pyvm

import (
	"context"
	"fmt"

	"github.com/hanzoai/base/plugins/extruntime"
)

// NewRuntime returns a CPython-shaped runtime that errors at Load time
// when the binary was built without the `pyvm` build tag. Name() and
// Capabilities() still report the pyvm identity so callers can detect
// the runtime is present-but-unavailable rather than missing entirely.
func NewRuntime() extruntime.Runtime { return &stubRuntime{} }

type stubRuntime struct{}

func (*stubRuntime) Name() string { return "pyvm" }

func (*stubRuntime) Capabilities() extruntime.Capabilities {
	return extruntime.Capabilities{
		AcceptsLanguages: []string{"py"},
		// HardSandbox=false: cgo CPython shares the host process. A C
		// extension segfault kills the whole Go binary. Use wazero +
		// RustPython if you need a sandboxed Python.
		HardSandbox:   false,
		Cgo:           true,
		// HONEST: pyvm cannot hard-abort a running invocation. See README.
		SupportsAbort: false,
	}
}

func (*stubRuntime) Load(_ context.Context, _ string) (extruntime.Module, error) {
	return nil, fmt.Errorf("%w: built without -tags pyvm (cgo CPython disabled)", extruntime.ErrUnsupported)
}

func (*stubRuntime) Close() error { return nil }
