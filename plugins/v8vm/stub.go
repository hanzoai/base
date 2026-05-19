//go:build !v8vm
// +build !v8vm

// Package v8vm is the V8 (via cgo) extension runtime for Base.
//
// The real implementation lives behind the `v8vm` build tag because v8go
// pulls in roughly 50 MB of cgo dependencies. The default build of base
// must compile without it, so this stub file is what gets linked in
// normally — `NewRuntime()` is always callable and always exists; only
// the actual V8 engine is opt-in via `go build -tags v8vm`.
package v8vm

import (
	"context"
	"fmt"

	"github.com/hanzoai/base/plugins/extruntime"
)

// NewRuntime returns a v8go-shaped runtime that errors at Load time when
// the binary was built without the `v8vm` build tag. Name() and
// Capabilities() still report the v8go identity so callers can detect
// the runtime is present-but-unavailable rather than missing entirely.
func NewRuntime() extruntime.Runtime { return &stubRuntime{} }

type stubRuntime struct{}

func (*stubRuntime) Name() string { return "v8go" }

func (*stubRuntime) Capabilities() extruntime.Capabilities {
	return extruntime.Capabilities{
		AcceptsLanguages: []string{"js"},
		HardSandbox:      true,
		Cgo:              true,
		SupportsAbort:    true,
	}
}

func (*stubRuntime) Load(_ context.Context, _ string) (extruntime.Module, error) {
	return nil, fmt.Errorf("%w: built without -tags v8vm", extruntime.ErrUnsupported)
}

func (*stubRuntime) Close() error { return nil }
