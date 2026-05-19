package gojavm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/dop251/goja"
	"github.com/hanzoai/base/plugins/extruntime"
)

// interruptValue is what we pass to goja.Runtime.Interrupt() when ctx
// cancels mid-invocation. goja re-throws this as an interruptedError
// from RunString/Call; we detect it and translate back to ctx.Err().
type interruptValue struct{}

func (interruptValue) Error() string { return "gojavm: context canceled" }

// module is one loaded extension. The JS source is compiled to a
// *goja.Program once at Load. Per Invoke we borrow a runtime from the
// pool, ensure the program has been run on that runtime (registers
// globalThis.fn / globalThis.<export>), then call the requested
// function.
type module struct {
	name    string
	exports []string
	program *goja.Program
	pool    *vmsPool

	// runtimeInit tracks which goja.Runtime instances have already had
	// the program executed on them. Each pool slot only needs to run
	// the script once; subsequent invocations just call globalThis.fn.
	// Keyed by pointer identity. Cheap and correct.
	mu          sync.Mutex
	runtimeInit map[*goja.Runtime]bool
	closed      bool
}

func newModule(pool *vmsPool, dir string, m *extruntime.Manifest) (*module, error) {
	src := m.Module
	if src == "" {
		src = "index.js"
	}
	path := filepath.Join(dir, src)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("gojavm: read %s: %w", path, err)
	}

	// Compile once. goja.Compile is deterministic and the resulting
	// *goja.Program is safe to share across runtimes.
	prog, err := goja.Compile(path, string(data), true)
	if err != nil {
		return nil, fmt.Errorf("gojavm: compile %s: %w", path, err)
	}

	return &module{
		name:        m.Name,
		exports:     m.Exports,
		program:     prog,
		pool:        pool,
		runtimeInit: map[*goja.Runtime]bool{},
	}, nil
}

func (m *module) Name() string      { return m.name }
func (m *module) Runtime() string   { return "goja" }
func (m *module) Exports() []string { return m.exports }

func (m *module) Invoke(ctx context.Context, fn string, payload []byte) ([]byte, error) {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return nil, extruntime.ErrClosed
	}
	m.mu.Unlock()

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	var out []byte
	runErr := m.pool.run(func(vm *goja.Runtime) error {
		// Ensure the module's program has been executed on this
		// particular runtime, populating globalThis.<fn>.
		if err := m.ensureLoaded(vm); err != nil {
			return err
		}

		callable, ok := goja.AssertFunction(vm.Get(fn))
		if !ok {
			return fmt.Errorf("%w: %s:%s", extruntime.ErrUnknownFn, m.name, fn)
		}

		// Unmarshal the payload to a Go value; let goja convert it to
		// a JS value. The runtime layer treats payloads as JSON bytes
		// so every backend (wasm/v8/native/goja) sees the same wire.
		var arg any
		if len(payload) > 0 {
			if err := json.Unmarshal(payload, &arg); err != nil {
				return fmt.Errorf("gojavm: payload not JSON: %w", err)
			}
		}

		// Watchdog: when ctx cancels, interrupt the runtime. The
		// interrupted Call returns an *InterruptedError wrapping our
		// interruptValue. Stop the watchdog once the call returns so
		// we don't leak the goroutine.
		done := make(chan struct{})
		defer close(done)
		go func() {
			select {
			case <-ctx.Done():
				vm.Interrupt(interruptValue{})
			case <-done:
			}
		}()

		jsArg := vm.ToValue(arg)
		result, err := callable(goja.Undefined(), jsArg)
		if err != nil {
			// Translate an interrupt back to the ctx error.
			var iex *goja.InterruptedError
			if errors.As(err, &iex) {
				if ctxErr := ctx.Err(); ctxErr != nil {
					return ctxErr
				}
			}
			return fmt.Errorf("gojavm: invoke %s:%s: %w", m.name, fn, err)
		}

		// Marshal the result back to JSON bytes. goja's
		// json.Stringify handles JS-native edge cases (undefined ->
		// no field, functions skipped, Date -> ISO string) more
		// faithfully than Go's encoding/json on the exported value.
		out = stringify(vm, result)
		return nil
	})
	if runErr != nil {
		return nil, runErr
	}
	return out, nil
}

func (m *module) Close() error {
	m.mu.Lock()
	m.closed = true
	m.runtimeInit = nil
	m.mu.Unlock()
	return nil
}

// ensureLoaded runs the compiled program on this runtime exactly once.
// Subsequent calls are no-ops. We track the runtime pointer identity;
// when a pool slot is recycled (it isn't in our pool, but if a future
// implementation rotates slots) the entry would simply be re-run.
func (m *module) ensureLoaded(vm *goja.Runtime) error {
	m.mu.Lock()
	already := m.runtimeInit[vm]
	if !already {
		m.runtimeInit[vm] = true
	}
	m.mu.Unlock()

	if already {
		return nil
	}

	if _, err := vm.RunProgram(m.program); err != nil {
		// Roll back the init marker so a transient error doesn't
		// permanently poison the slot for this module.
		m.mu.Lock()
		delete(m.runtimeInit, vm)
		m.mu.Unlock()
		return fmt.Errorf("gojavm: load %s: %w", m.name, err)
	}
	return nil
}

// stringify reproduces `JSON.stringify(value)` semantics in Go without
// re-entering the runtime. goja.Value.Export gives us a Go-side view;
// json.Marshal on that view produces the canonical wire bytes.
//
// We prefer this to `vm.RunString("JSON.stringify(...)")` because:
//   - it avoids parsing a literal expression each call,
//   - it avoids polluting globalThis with a temp var, and
//   - undefined results become an empty []byte rather than the
//     literal string "undefined" (which is not valid JSON).
func stringify(vm *goja.Runtime, v goja.Value) []byte {
	if v == nil || goja.IsUndefined(v) || goja.IsNull(v) {
		// `null` is valid JSON; for parity with JSON.stringify(null) -> "null"
		// we emit literal null. Undefined -> empty bytes (the spec says
		// JSON.stringify(undefined) returns undefined, not a string).
		if v != nil && goja.IsNull(v) {
			return []byte("null")
		}
		return nil
	}
	exported := v.Export()
	b, err := json.Marshal(exported)
	if err != nil {
		// Fall back to the runtime's own stringifier, which handles
		// quirky values (Symbol, BigInt, cycles) by throwing — we
		// turn that throw into an empty result.
		return nil
	}
	return b
}
