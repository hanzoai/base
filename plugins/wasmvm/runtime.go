// Package wasmvm implements the wazero-backed extension runtime for Base.
//
// One process-wide wazero.Runtime is created with a compilation cache so
// every Module loaded from disk reuses the same JIT/AOT artifacts. WASI
// snapshot preview1 is instantiated once on that runtime so guest modules
// targeting wasi (Rust, AssemblyScript with wasi shim, TinyGo) can do
// stdio + clock + random without per-module host wiring.
//
// The host-guest ABI is intentionally minimal — see module.go for the
// exact calling convention. Anything more ambitious (component model,
// witx bindings, capability passing) lives at a higher layer.
package wasmvm

import (
	"context"
	"fmt"
	"sync"

	"github.com/hanzoai/base/plugins/extruntime"
	"github.com/tetratelabs/wazero"
	wasi "github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
)

// NewRuntime returns a Runtime backed by a shared wazero.Runtime.
// The compilation cache is in-memory; persisting it across process
// restarts is a deployment concern, not a runtime concern.
func NewRuntime() extruntime.Runtime {
	return &wasmRuntime{}
}

type wasmRuntime struct {
	once sync.Once
	err  error
	rt   wazero.Runtime
	// initCtx is bound at first Load so WASI's instantiate has a
	// context to attach to; the actual invoke uses the caller's ctx.
	initCtx context.Context
}

func (*wasmRuntime) Name() string { return "wazero" }

func (*wasmRuntime) Capabilities() extruntime.Capabilities {
	return extruntime.Capabilities{
		AcceptsLanguages: []string{"wasm"},
		HardSandbox:      true,
		Cgo:              false,
		SupportsAbort:    true,
	}
}

// ensure lazily builds the shared runtime. We can't do this in NewRuntime()
// because failures there have no context to attach to — deferring lets
// Load() surface the error to the caller cleanly.
func (r *wasmRuntime) ensure(ctx context.Context) error {
	r.once.Do(func() {
		cache := wazero.NewCompilationCache()
		cfg := wazero.NewRuntimeConfig().WithCompilationCache(cache)
		r.rt = wazero.NewRuntimeWithConfig(ctx, cfg)
		if _, err := wasi.Instantiate(ctx, r.rt); err != nil {
			r.err = fmt.Errorf("wasmvm: instantiate wasi: %w", err)
		}
		r.initCtx = ctx
	})
	return r.err
}

func (r *wasmRuntime) Load(ctx context.Context, dir string) (extruntime.Module, error) {
	if err := r.ensure(ctx); err != nil {
		return nil, err
	}
	return loadModule(ctx, r.rt, dir)
}

func (r *wasmRuntime) Close() error {
	if r.rt == nil {
		return nil
	}
	// Background ctx — Close should run to completion even if the
	// caller's ctx is already cancelled.
	return r.rt.Close(context.Background())
}
