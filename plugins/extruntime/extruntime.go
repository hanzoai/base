// Package extruntime defines the pluggable extension runtime interface used
// by Base's extension subsystem. Each runtime (goja, wazero, v8go, native Go)
// satisfies this interface so an extension declares its preferred runtime in
// its manifest and Base loads it without caring how the code executes.
//
// This sits ALONGSIDE the existing plugins/jsvm goja runtime — it doesn't
// replace it. Goja remains the default for .base.js / .base.ts hook files.
// The other runtimes are opt-in via an `extension.json` manifest:
//
//	# hooks/validate-email/extension.json
//	{
//	  "name":    "validate-email",
//	  "version": "0.1.0",
//	  "runtime": "wazero",
//	  "module":  "validate.wasm",
//	  "exports": ["onCreate", "onUpdate"]
//	}
package extruntime

import (
	"context"
	"errors"
)

// Runtime is implemented by every backing engine.
type Runtime interface {
	// Name returns the runtime identifier ("native" | "goja" | "wazero" | "v8go").
	Name() string

	// Capabilities returns metadata about what this runtime can do.
	Capabilities() Capabilities

	// Load reads an extension package from disk and returns a Module
	// instance. The path is the directory containing the extension.json
	// manifest. Implementations should pre-compile / pre-instantiate
	// here so the per-invocation Invoke cost is minimized.
	Load(ctx context.Context, dir string) (Module, error)

	// Close releases any runtime-level resources (compilation cache, etc.).
	// Modules created by this runtime should already be Close()d by callers.
	Close() error
}

// Module is one loaded extension. It exposes named functions.
type Module interface {
	// Name returns the extension name from its manifest.
	Name() string

	// Runtime returns the runtime that loaded this module.
	Runtime() string

	// Exports returns the list of function names this module declares
	// in its manifest. Invoke may still be called with any name — the
	// runtime decides whether to error or no-op.
	Exports() []string

	// Invoke executes the named function with a JSON-encoded payload
	// and returns a JSON-encoded result. Payloads are bytes so the
	// runtime layer doesn't impose a wire format on the host code —
	// callers serialize once and let every runtime see the same bytes.
	//
	// Context cancellation must abort the running invocation; long-
	// running extension code that doesn't respect ctx is the
	// extension author's bug, not the runtime's. wazero and v8go can
	// hard-abort; goja and native rely on cooperative ctx checking.
	Invoke(ctx context.Context, fn string, payload []byte) ([]byte, error)

	// Close releases the module's resources (compiled wasm, isolate,
	// goja runtime, etc.). After Close, Invoke must error.
	Close() error
}

// Capabilities describes what a runtime supports — used so callers can
// decide whether to load a given extension or skip it (e.g. v8go-only
// builds shouldn't try to load AssemblyScript wasm).
type Capabilities struct {
	// AcceptsLanguages lists the source languages this runtime can run.
	//   native: ["go"]
	//   goja:   ["js"]
	//   wazero: ["wasm"]   (compile any-lang-to-wasm ahead of time)
	//   v8go:   ["js"]
	AcceptsLanguages []string

	// HardSandbox is true if the runtime enforces a memory-isolated
	// sandbox (wazero linear memory, v8go isolate). goja is false
	// (shares Go heap with host); native is N/A (it IS the host).
	HardSandbox bool

	// Cgo indicates the runtime requires cgo to build. Affects binary
	// distribution and cross-compilation.
	Cgo bool

	// SupportsAbort is true if a context cancellation can interrupt a
	// running invocation without cooperative checks in the guest code.
	SupportsAbort bool
}

// Errors returned at the boundary. Wrappers should keep using errors.Is.
var (
	ErrNoManifest   = errors.New("extruntime: extension.toml not found in directory")
	ErrBadManifest  = errors.New("extruntime: extension.toml is invalid")
	ErrUnknownFn    = errors.New("extruntime: function not found in module")
	ErrClosed       = errors.New("extruntime: module is closed")
	ErrUnsupported  = errors.New("extruntime: runtime does not support this operation")
)

// Manifest is the parsed extension.json contents.
type Manifest struct {
	Name    string   `json:"name"`
	Version string   `json:"version"`
	Runtime string   `json:"runtime"`
	Module  string   `json:"module"`
	Exports []string `json:"exports"`
}
