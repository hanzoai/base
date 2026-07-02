// Package gojavm adapts zip's embedded JavaScript runtime
// (github.com/zap-proto/zip/runtime) to base's extruntime.Runtime SPI, so
// a manifest with `"runtime": "goja"` loads here. There is now exactly
// ONE goja engine in the Hanzo stack — zip's *runtime.JSRuntime — and
// base, cloud and every other zip consumer share it. gojavm is the thin
// extruntime projection of that engine: it owns manifest loading,
// TS/JSX/ESM bundling (esbuild, see transpile.go) and the JSON-bytes
// Invoke wire; the VM pool, host-fn registration and per-request VM
// isolation all live in zip/runtime.
//
// This is the lightweight JS option — pure Go, no cgo, shares the host
// heap. Use it when you don't need a hard sandbox.
//
// It sits alongside plugins/jsvm (the hook-style goja host for .base.js
// hook files). Extension directories with extension.json + runtime=goja
// go through here; hook files still go through jsvm. Collapsing jsvm onto
// zip/runtime as well is tracked separately — it needs base's host-API
// binds surface lifted into zip first.
package gojavm

import (
	"context"
	"fmt"
	"os"
	"strconv"

	zipruntime "github.com/zap-proto/zip/runtime"

	"github.com/hanzoai/base/plugins/extruntime"
)

// defaultPoolSize is the number of pre-warmed goja VMs zip keeps hot.
// Override via BASE_GOJAVM_POOL_SIZE. Zero or negative selects zip's own
// default — every Invoke still borrows a VM, there is no per-call VM
// construction in the steady state.
const defaultPoolSize = 8

// NewRuntime constructs a goja-backed extruntime.Runtime. The goja engine
// is zip's *runtime.JSRuntime; one process-wide pool is shared by every
// module this runtime loads. require()/console/process are provided by
// zip's VM provisioning, so migrated TypeScript backends that
// `require(...)`, `console.log(...)` or read `process.env` run unchanged.
func NewRuntime() extruntime.Runtime {
	size := defaultPoolSize
	if v := os.Getenv("BASE_GOJAVM_POOL_SIZE"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			size = n
		}
	}

	rt, err := zipruntime.NewJSRuntime(zipruntime.JSOptions{PoolSize: size})
	if err != nil {
		// NewJSRuntime only fails if a host fn / module fails to bind; we
		// register none here, so this is unreachable in practice. Fall
		// back to a default runtime rather than panic at package use.
		rt, _ = zipruntime.NewJSRuntime(zipruntime.JSOptions{})
	}

	return &gojaRuntime{js: rt}
}

type gojaRuntime struct {
	js *zipruntime.JSRuntime
}

func (*gojaRuntime) Name() string { return "goja" }

func (*gojaRuntime) Capabilities() extruntime.Capabilities {
	return extruntime.Capabilities{
		AcceptsLanguages: []string{"js"},
		HardSandbox:      false,
		Cgo:              false,
		// goja's Interrupt() is cooperative — guest code without
		// function-call opcodes (a tight for-loop on numerics) can
		// still resist it. Practically every real script yields
		// often enough that abort works, so we report true.
		SupportsAbort: true,
	}
}

func (r *gojaRuntime) Load(ctx context.Context, dir string) (extruntime.Module, error) {
	m, err := extruntime.LoadManifest(dir)
	if err != nil {
		return nil, err
	}
	if m.Runtime != "goja" {
		return nil, fmt.Errorf("%w: goja runtime cannot load %q runtime", extruntime.ErrUnsupported, m.Runtime)
	}
	return newModule(r.js, dir, m)
}

func (r *gojaRuntime) Close() error {
	// zip's JSRuntime has no Close — its pooled VMs are plain values the
	// GC reclaims once the runtime is unreferenced. Drop the reference.
	r.js = nil
	return nil
}
