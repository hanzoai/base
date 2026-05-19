// Package starkvm wraps Google's go.starlark.net Starlark interpreter
// as an extruntime.Runtime. Starlark is Python-syntax-with-strict-
// semantics — pure functions, no while loops, no recursion (configurable),
// designed by Google for embedding configuration/policy code into Go.
// Used by Bazel, Buck2, Tilt, Pulumi, Drone CI.
//
// Use this when:
//   - You want users to write Python-feel extension hooks but you don't
//     need the full Python ecosystem (numpy, requests, etc).
//   - You want hermetic-by-construction semantics (no IO, no time, no
//     network unless you wire it in via the host).
//   - You want pure-Go (no cgo, no libpython) parallelism without a GIL.
//
// Per the architecture brief: AcceptsLanguages=["starlark"], HardSandbox
// is false (Starlark values live on the Go heap; a malicious script
// could exhaust memory with deep nesting / large list comprehensions),
// Cgo=false, SupportsAbort=true (starlark.Thread.Cancel is honored on
// the next opcode, comparable to goja.Interrupt — cooperative but
// reliably prompt for any real-world script).
package starkvm

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"sync"

	"github.com/hanzoai/base/plugins/extruntime"
	"go.starlark.net/syntax"
)

// defaultPoolSize is the number of pre-warmed *starlark.Thread instances
// each module owns. Thread is the per-invocation execution context;
// reuse mostly saves Go heap allocations, not anything heavy. Override
// via BASE_STARKVM_POOL_SIZE.
const defaultPoolSize = 8

const poolEnv = "BASE_STARKVM_POOL_SIZE"

// fileOptions enables the relaxed-but-still-pure Starlark dialect:
// while loops, top-level rebinding, set literals, recursion. We need
// these because the validate fixture's domain check is naturally
// expressed with `while` and an early-return helper. Strict Bazel
// Starlark would force list comprehensions and recursion off; we
// pick the permissive variant deliberately and document it on the
// fixture.
var fileOptions = &syntax.FileOptions{
	Set:             true,
	While:           true,
	TopLevelControl: true,
	GlobalReassign:  true,
	Recursion:       true,
}

// NewRuntime constructs a starlark-backed extruntime.Runtime. The pool
// size is shared across modules — each module owns its own thread pool
// because threads carry a global frame reference.
func NewRuntime() extruntime.Runtime {
	size := defaultPoolSize
	if v := os.Getenv(poolEnv); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			size = n
		}
	}
	return &starkRuntime{poolSize: size}
}

type starkRuntime struct {
	poolSize int
	mu       sync.Mutex
	closed   bool
}

func (*starkRuntime) Name() string { return "starlark" }

func (*starkRuntime) Capabilities() extruntime.Capabilities {
	return extruntime.Capabilities{
		AcceptsLanguages: []string{"starlark"},
		// Starlark values live on the Go heap; a malicious script can
		// still exhaust memory, but cannot escape the interpreter to
		// the host process beyond what we expose as builtins. Not a
		// hard sandbox in the wazero sense, but stronger than goja —
		// no eval, no fetch, no fs.
		HardSandbox: false,
		Cgo:         false,
		// starlark.Thread.Cancel sets a flag the evaluator checks
		// between opcodes — same cooperative model as goja.Interrupt.
		// Practically every real-world script hits an opcode boundary
		// within microseconds; we report true.
		SupportsAbort: true,
	}
}

func (r *starkRuntime) Load(ctx context.Context, dir string) (extruntime.Module, error) {
	r.mu.Lock()
	closed := r.closed
	r.mu.Unlock()
	if closed {
		return nil, extruntime.ErrClosed
	}

	m, err := extruntime.LoadManifest(dir)
	if err != nil {
		return nil, err
	}
	if m.Runtime != "starlark" {
		return nil, fmt.Errorf("%w: starlark runtime cannot load %q runtime", extruntime.ErrUnsupported, m.Runtime)
	}
	return newModule(dir, m, r.poolSize)
}

func (r *starkRuntime) Close() error {
	r.mu.Lock()
	r.closed = true
	r.mu.Unlock()
	return nil
}
